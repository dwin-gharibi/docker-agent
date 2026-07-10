package session

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/tools"
)

// reloadedToolExchangeItems builds a history that never passed through
// AddMessage — like a session reconstructed from persistence (the generic
// API/SQLite path stores messages directly) — ending in a tool result with
// the given payloads. The assistant tool_use matches the result so
// sanitizeToolCalls retains it.
func reloadedToolExchangeItems(toolCallID, content string, multiContent []chat.MessagePart) []Item {
	return []Item{
		NewMessageItem(UserMessage("run " + toolCallID)),
		NewMessageItem(NewAgentMessage("test-agent", &chat.Message{
			Role: chat.MessageRoleAssistant,
			ToolCalls: []tools.ToolCall{
				{
					ID: toolCallID,
					Function: tools.FunctionCall{
						Name:      "test_tool",
						Arguments: `{"input":"` + toolCallID + `"}`,
					},
				},
			},
		})),
		NewMessageItem(NewAgentMessage("test-agent", &chat.Message{
			Role:         chat.MessageRoleTool,
			ToolCallID:   toolCallID,
			Content:      content,
			MultiContent: multiContent,
		})),
	}
}

func toolResultMessage(t *testing.T, messages []chat.Message, toolCallID string) chat.Message {
	t.Helper()

	for _, msg := range messages {
		if msg.Role == chat.MessageRoleTool && msg.ToolCallID == toolCallID {
			return msg
		}
	}

	require.Failf(t, "tool result not found", "tool_call_id=%s", toolCallID)
	return chat.Message{}
}

// TestGetMessagesMaxToolResultTokensCapsReloadedResult pins the read-time
// backstop: a tool result that entered the history without AddMessage (e.g.
// persisted by the API/SQLite path and reloaded) must still reach the
// provider bounded — Content, its duplicate text part, and inline-text
// documents alike — while the stored history stays untouched.
func TestGetMessagesMaxToolResultTokensCapsReloadedResult(t *testing.T) {
	t.Parallel()
	oversized := strings.Repeat("x", 100_000)
	docText := strings.Repeat("€", 50_000)
	documents := []tools.DocumentContent{
		{Name: "big.md", MimeType: "text/markdown", Text: docText},
	}

	s := New(
		WithMessages(reloadedToolExchangeItems("tc", oversized, chat.BuildToolResultMultiContent(oversized, nil, documents))),
		WithMaxToolResultTokens(100),
	)

	messages := s.GetMessages(agent.New("test-agent", "test instruction"))

	got := toolResultMessage(t, messages, "tc")
	assert.Contains(t, got.Content, toolResultTruncationMarker)
	assert.LessOrEqual(t, toolResultTextTokens(got), 100, "aggregate must never exceed the per-result cap")
	require.Len(t, got.MultiContent, 2)
	assert.Equal(t, got.Content, got.MultiContent[0].Text, "duplicate text part must stay in sync with Content")
	doc := got.MultiContent[1].Document
	require.NotNil(t, doc)
	assert.Equal(t, "big.md", doc.Name, "document metadata must be preserved")
	assert.Less(t, len(doc.Source.InlineText), len(docText))
	assert.True(t, utf8.ValidString(doc.Source.InlineText), "truncated document must stay valid UTF-8")
	assert.Equal(t, int64(len(doc.Source.InlineText)), doc.Size, "size must track the kept text")

	// Reading must not rewrite history: the stored message keeps the
	// unbounded payloads.
	stored := storedToolResult(t, s, "tc")
	assert.Equal(t, oversized, stored.Content)
	require.Len(t, stored.MultiContent, 2)
	assert.Equal(t, oversized, stored.MultiContent[0].Text)
	require.NotNil(t, stored.MultiContent[1].Document)
	assert.Equal(t, docText, stored.MultiContent[1].Document.Source.InlineText)
}

func TestGetMessagesMaxToolResultTokensDefaultPreservesReloadedResult(t *testing.T) {
	t.Parallel()
	oversized := strings.Repeat("x", 100_000)

	s := New(WithMessages(reloadedToolExchangeItems("tc", oversized, nil)))

	messages := s.GetMessages(agent.New("test-agent", "test instruction"))

	assert.Equal(t, oversized, toolResultMessage(t, messages, "tc").Content)
}
