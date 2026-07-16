package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/model/provider/base"
	"github.com/docker/docker-agent/pkg/tools"
)

func TestSupportsDeferredToolsCompatibility(t *testing.T) {
	t.Parallel()

	assert.True(t, (&Client{Config: base.Config{ModelConfig: latest.ModelConfig{Provider: "anthropic", Model: "claude-sonnet-4-5"}}}).supportsDeferredTools())
	assert.False(t, (&Client{Config: base.Config{ModelConfig: latest.ModelConfig{Provider: "anthropic", Model: "claude-haiku-4-5"}}}).supportsDeferredTools())
	assert.True(t, (&Client{Config: base.Config{ModelConfig: latest.ModelConfig{
		Provider: "anthropic-proxy", Model: "claude-opus-4-6", ProviderOpts: map[string]any{"supports_deferred_tools": true},
	}}}).supportsDeferredTools())
}

func TestConvertToolsMarksOnlyDeferredTools(t *testing.T) {
	requestTools := []tools.Tool{
		{Name: "read", Parameters: map[string]any{"type": "object"}},
		{Name: "search", Parameters: map[string]any{"type": "object"}, Deferred: true},
	}

	standard, err := convertTools(requestTools)
	require.NoError(t, err)
	require.Len(t, standard, 2)
	assert.False(t, standard[0].OfTool.DeferLoading.Valid())
	assert.Equal(t, "ephemeral", string(standard[0].OfTool.CacheControl.Type))
	require.True(t, standard[1].OfTool.DeferLoading.Valid())
	assert.True(t, standard[1].OfTool.DeferLoading.Value)

	beta, err := convertBetaTools(requestTools)
	require.NoError(t, err)
	require.Len(t, beta, 2)
	assert.False(t, beta[0].OfTool.DeferLoading.Valid())
	assert.Equal(t, "ephemeral", string(beta[0].OfTool.CacheControl.Type))
	require.True(t, beta[1].OfTool.DeferLoading.Valid())
	assert.True(t, beta[1].OfTool.DeferLoading.Value)
}

func TestDeferredToolsAreReferencedAtTheirLoadPoint(t *testing.T) {
	messages := []chat.Message{
		{Role: chat.MessageRoleAssistant, ToolCalls: []tools.ToolCall{{
			ID: "call-1", Function: tools.FunctionCall{Name: "add_tool", Arguments: `{}`},
		}}},
		{Role: chat.MessageRoleTool, ToolCallID: "call-1", Content: "activated"},
	}
	requestTools := []tools.Tool{{
		Name: "search", Parameters: map[string]any{"type": "object"},
		Deferred: true, DeferredAtToolCallID: "call-1",
	}}

	standard, err := testClient().convertMessagesWithDeferred(t.Context(), messages, requestTools)
	require.NoError(t, err)
	standardJSON, err := json.Marshal(standard)
	require.NoError(t, err)
	assert.Contains(t, string(standardJSON), `"tool_name":"search","type":"tool_reference"`)

	beta, err := testClient().convertBetaMessagesWithDeferred(t.Context(), messages, requestTools)
	require.NoError(t, err)
	betaJSON, err := json.Marshal(beta)
	require.NoError(t, err)
	assert.Contains(t, string(betaJSON), `"tool_name":"search","type":"tool_reference"`)
}

func TestDeferredToolsKeepSingleMessageCacheBreakpoint(t *testing.T) {
	messages := []chat.Message{
		{Role: chat.MessageRoleUser, Content: "first"},
		{Role: chat.MessageRoleAssistant, Content: "second"},
		{Role: chat.MessageRoleUser, Content: "third"},
	}
	deferredTools := []tools.Tool{{Name: "search", Parameters: map[string]any{"type": "object"}, Deferred: true}}

	countBreakpoints := func(v any) int {
		data, err := json.Marshal(v)
		require.NoError(t, err)
		return strings.Count(string(data), `"cache_control"`)
	}

	standard, err := testClient().convertMessagesWithDeferred(t.Context(), messages, deferredTools)
	require.NoError(t, err)
	assert.Equal(t, 1, countBreakpoints(standard))

	beta, err := testClient().convertBetaMessagesWithDeferred(t.Context(), messages, deferredTools)
	require.NoError(t, err)
	assert.Equal(t, 1, countBreakpoints(beta))

	// Without deferred tools the tool list carries no breakpoint, so two
	// message breakpoints remain in use.
	standard, err = testClient().convertMessagesWithDeferred(t.Context(), messages, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, countBreakpoints(standard))

	beta, err = testClient().convertBetaMessagesWithDeferred(t.Context(), messages, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, countBreakpoints(beta))
}

// Anthropic rejects requests with more than 4 cache_control blocks. Build the
// worst-case request (2 system breakpoints from the session, deferred tools,
// long conversation) and count breakpoints across system + tools + messages.
func TestRequestStaysWithinCacheBreakpointLimit(t *testing.T) {
	messages := []chat.Message{
		{Role: chat.MessageRoleSystem, Content: "invariant instructions", CacheControl: true},
		{Role: chat.MessageRoleSystem, Content: "dynamic context", CacheControl: true},
		{Role: chat.MessageRoleUser, Content: "first"},
		{Role: chat.MessageRoleAssistant, Content: "second"},
		{Role: chat.MessageRoleUser, Content: "third"},
	}

	countBreakpoints := func(parts ...any) int {
		total := 0
		for _, part := range parts {
			data, err := json.Marshal(part)
			require.NoError(t, err)
			total += strings.Count(string(data), `"cache_control"`)
		}
		return total
	}

	for _, requestTools := range [][]tools.Tool{
		nil,
		{
			{Name: "read", Parameters: map[string]any{"type": "object"}},
			{Name: "search", Parameters: map[string]any{"type": "object"}, Deferred: true},
		},
	} {
		convertedTools, err := convertTools(requestTools)
		require.NoError(t, err)
		converted, err := testClient().convertMessagesWithDeferred(t.Context(), messages, requestTools)
		require.NoError(t, err)
		assert.LessOrEqual(t, countBreakpoints(extractSystemBlocks(messages), convertedTools, converted), 4)

		betaTools, err := convertBetaTools(requestTools)
		require.NoError(t, err)
		betaConverted, err := testClient().convertBetaMessagesWithDeferred(t.Context(), messages, requestTools)
		require.NoError(t, err)
		assert.LessOrEqual(t, countBreakpoints(extractBetaSystemBlocks(messages), betaTools, betaConverted), 4)
	}
}
