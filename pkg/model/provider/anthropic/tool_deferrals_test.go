package anthropic

import (
	"encoding/json"
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
