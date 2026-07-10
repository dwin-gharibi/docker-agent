package session

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/tools"
)

func addToolResult(s *Session, toolCallID, content string) {
	s.AddMessage(NewAgentMessage("test-agent", &chat.Message{
		Role:       chat.MessageRoleTool,
		ToolCallID: toolCallID,
		Content:    content,
	}))
}

// storedToolResult returns the tool-result message as retained by the
// session, bypassing GetMessages so the tests observe ingestion-time state.
func storedToolResult(t *testing.T, s *Session, toolCallID string) chat.Message {
	t.Helper()

	for _, item := range s.Messages {
		if item.IsMessage() && item.Message.Message.ToolCallID == toolCallID {
			return item.Message.Message
		}
	}

	require.Failf(t, "tool result not found", "tool_call_id=%s", toolCallID)
	return chat.Message{}
}

func TestAddMessageMaxToolResultTokensDefaultPreservesOversizedResult(t *testing.T) {
	t.Parallel()
	oversized := strings.Repeat("x", 100_000)

	s := New()
	addToolResult(s, "tc", oversized)

	assert.Equal(t, oversized, storedToolResult(t, s, "tc").Content)
}

func TestAddMessageMaxToolResultTokensNegativeDisablesCap(t *testing.T) {
	t.Parallel()
	oversized := strings.Repeat("x", 100_000)

	s := New(WithMaxToolResultTokens(-1))
	addToolResult(s, "tc", oversized)

	assert.Equal(t, oversized, storedToolResult(t, s, "tc").Content)
}

func TestAddMessageMaxToolResultTokensCapsAtIngestion(t *testing.T) {
	t.Parallel()
	oversized := strings.Repeat("x", 100_000)

	s := New(WithMaxToolResultTokens(100))
	addToolResult(s, "tc", oversized)

	got := storedToolResult(t, s, "tc").Content
	assert.Contains(t, got, toolResultTruncationMarker)
	assert.LessOrEqual(t, approximateTokens(got), 100)
}

func TestAddMessageMaxToolResultTokensKeepsHeadAndTail(t *testing.T) {
	t.Parallel()
	content := "HEAD-MARKER " + strings.Repeat("h", 500) +
		strings.Repeat("m", 4000) +
		strings.Repeat("t", 500) + " TAIL-MARKER"

	s := New(WithMaxToolResultTokens(256)) // 1024-byte budget
	addToolResult(s, "tc", content)

	got := storedToolResult(t, s, "tc").Content
	assert.True(t, strings.HasPrefix(got, "HEAD-MARKER "), "head must survive")
	assert.True(t, strings.HasSuffix(got, " TAIL-MARKER"), "tail must survive")
	assert.Contains(t, got, toolResultTruncationMarker)
	assert.NotContains(t, got, "mmm", "middle must be removed")
	assert.LessOrEqual(t, approximateTokens(got), 256, "marker must count within the cap")
}

func TestAddMessageMaxToolResultTokensBoundaryUnchanged(t *testing.T) {
	t.Parallel()
	exact := strings.Repeat("e", 100) // exactly 25 tokens at len/4
	below := strings.Repeat("b", 99)

	s := New(WithMaxToolResultTokens(25))
	addToolResult(s, "exact", exact)
	addToolResult(s, "below", below)

	assert.Equal(t, exact, storedToolResult(t, s, "exact").Content)
	assert.Equal(t, below, storedToolResult(t, s, "below").Content)
}

func TestAddMessageMaxToolResultTokensPreservesUTF8(t *testing.T) {
	t.Parallel()
	// 3-byte runes make the raw cut points land mid-rune.
	content := strings.Repeat("€", 4000)

	s := New(WithMaxToolResultTokens(64))
	addToolResult(s, "tc", content)

	got := storedToolResult(t, s, "tc").Content
	assert.True(t, utf8.ValidString(got), "truncated content must stay valid UTF-8")
	assert.Contains(t, got, toolResultTruncationMarker)
	assert.True(t, strings.HasPrefix(got, "€"), "head must keep whole runes")
	assert.True(t, strings.HasSuffix(got, "€"), "tail must keep whole runes")
}

func TestAddMessageMaxToolResultTokensLeavesNonToolMessagesAlone(t *testing.T) {
	t.Parallel()
	oversized := strings.Repeat("x", 100_000)

	s := New(WithMaxToolResultTokens(10))
	s.AddMessage(UserMessage(oversized))
	s.AddMessage(NewAgentMessage("test-agent", &chat.Message{
		Role:    chat.MessageRoleAssistant,
		Content: oversized,
	}))

	for _, item := range s.Messages {
		require.True(t, item.IsMessage())
		assert.Equal(t, oversized, item.Message.Message.Content)
	}
}

func TestAddMessageMaxToolResultTokensSyncsMultiContentText(t *testing.T) {
	t.Parallel()
	oversized := strings.Repeat("x", 100_000)
	images := []tools.MediaContent{{Data: "aGk=", MimeType: "image/png"}}

	s := New(WithMaxToolResultTokens(100))
	s.AddMessage(NewAgentMessage("test-agent", &chat.Message{
		Role:         chat.MessageRoleTool,
		ToolCallID:   "tc",
		Content:      oversized,
		MultiContent: chat.BuildToolResultMultiContent(oversized, images, nil),
	}))

	got := storedToolResult(t, s, "tc")
	require.Len(t, got.MultiContent, 2)
	assert.Equal(t, chat.MessagePartTypeText, got.MultiContent[0].Type)
	assert.Equal(t, got.Content, got.MultiContent[0].Text, "text part must stay in sync with Content")
	assert.Contains(t, got.MultiContent[0].Text, toolResultTruncationMarker)
	assert.Equal(t, chat.MessagePartTypeImageURL, got.MultiContent[1].Type, "non-text parts must be untouched")
}

func TestAddMessageMaxToolResultTokensSyncsOneDuplicateAndBoundsIdenticalExtraText(t *testing.T) {
	t.Parallel()
	oversized := strings.Repeat("x", 100_000)

	s := New(WithMaxToolResultTokens(100))
	s.AddMessage(NewAgentMessage("test-agent", &chat.Message{
		Role:       chat.MessageRoleTool,
		ToolCallID: "tc",
		Content:    oversized,
		MultiContent: []chat.MessagePart{
			{Type: chat.MessagePartTypeText, Text: oversized}, // Content duplicate
			{Type: chat.MessagePartTypeText, Text: oversized}, // Distinct identical payload
		},
	}))

	got := storedToolResult(t, s, "tc")
	require.Len(t, got.MultiContent, 2)
	assert.Equal(t, got.Content, got.MultiContent[0].Text, "first matching text part must stay in sync with Content")
	assert.Empty(t, got.MultiContent[1].Text, "distinct identical text must still consume the exhausted budget")
	assert.LessOrEqual(t, toolResultTextTokens(got), 100)
}

// toolResultTextTokens approximates the aggregate textual tokens a provider
// can receive from a stored tool result: Content counted once (a text part
// equal to Content is its duplicate) plus distinct text parts and
// inline-text documents.
func toolResultTextTokens(msg chat.Message) int {
	total := len(msg.Content)
	contentDuplicateSeen := false
	for _, part := range msg.MultiContent {
		switch {
		case part.Type == chat.MessagePartTypeText && !contentDuplicateSeen && part.Text == msg.Content:
			contentDuplicateSeen = true
		case part.Type == chat.MessagePartTypeText:
			total += len(part.Text)
		case part.Type == chat.MessagePartTypeDocument && part.Document != nil:
			total += len(part.Document.Source.InlineText)
		}
	}
	return total / 4
}

func TestAddMessageMaxToolResultTokensCapsInlineTextDocument(t *testing.T) {
	t.Parallel()
	content := "short tool output"
	docText := strings.Repeat("€", 50_000)
	documents := []tools.DocumentContent{
		{Name: "big.md", MimeType: "text/markdown", Text: docText},
		{Name: "blob.pdf", MimeType: "application/pdf", Data: "UERGREFUQQ=="}, // "PDFDATA"
	}

	s := New(WithMaxToolResultTokens(100)) // 400-byte budget
	s.AddMessage(NewAgentMessage("test-agent", &chat.Message{
		Role:         chat.MessageRoleTool,
		ToolCallID:   "tc",
		Content:      content,
		MultiContent: chat.BuildToolResultMultiContent(content, nil, documents),
	}))

	got := storedToolResult(t, s, "tc")
	assert.Equal(t, content, got.Content, "content within budget must stay intact")
	require.Len(t, got.MultiContent, 3)

	doc := got.MultiContent[1].Document
	require.NotNil(t, doc)
	assert.Equal(t, "big.md", doc.Name, "document metadata must be preserved")
	assert.Equal(t, "text/markdown", doc.MimeType, "document metadata must be preserved")
	assert.Contains(t, doc.Source.InlineText, toolResultTruncationMarker)
	assert.True(t, utf8.ValidString(doc.Source.InlineText), "truncated document must stay valid UTF-8")
	assert.Equal(t, int64(len(doc.Source.InlineText)), doc.Size, "size must track the kept text")

	binary := got.MultiContent[2].Document
	require.NotNil(t, binary)
	assert.Equal(t, []byte("PDFDATA"), binary.Source.InlineData, "binary documents must be untouched")

	assert.LessOrEqual(t, toolResultTextTokens(got), 100)
}

// TestAddMessageMaxToolResultTokensDoesNotMutateCallerDocument pins the
// copy-on-write contract: truncation must replace the stored part's Document
// with a copy, leaving a *chat.Document the caller still holds untouched.
func TestAddMessageMaxToolResultTokensDoesNotMutateCallerDocument(t *testing.T) {
	t.Parallel()
	docText := strings.Repeat("d", 100_000)
	external := &chat.Document{
		Name:     "big.md",
		MimeType: "text/markdown",
		Size:     int64(len(docText)),
		Source:   chat.DocumentSource{InlineText: docText},
	}

	s := New(WithMaxToolResultTokens(100))
	s.AddMessage(NewAgentMessage("test-agent", &chat.Message{
		Role:       chat.MessageRoleTool,
		ToolCallID: "tc",
		Content:    "short tool output",
		MultiContent: []chat.MessagePart{
			{Type: chat.MessagePartTypeText, Text: "short tool output"},
			{Type: chat.MessagePartTypeDocument, Document: external},
		},
	}))

	got := storedToolResult(t, s, "tc")
	require.Len(t, got.MultiContent, 2)
	stored := got.MultiContent[1].Document
	require.NotNil(t, stored)
	require.NotSame(t, external, stored, "truncation must copy the document, not mutate the caller's")
	assert.Contains(t, stored.Source.InlineText, toolResultTruncationMarker)
	assert.Less(t, len(stored.Source.InlineText), len(docText))
	assert.Equal(t, int64(len(stored.Source.InlineText)), stored.Size, "stored size must track the kept text")

	assert.Equal(t, docText, external.Source.InlineText, "caller's document text must stay untouched")
	assert.Equal(t, int64(len(docText)), external.Size, "caller's document size must stay untouched")
}

// TestAddMessageMaxToolResultTokensBoundsAggregateAcrossDocuments proves the
// cap is a result-wide budget: content plus six oversized documents would
// reach ~7x the cap if each payload were capped independently.
func TestAddMessageMaxToolResultTokensBoundsAggregateAcrossDocuments(t *testing.T) {
	t.Parallel()
	oversized := strings.Repeat("x", 100_000)
	content := strings.Repeat("c", 100)
	documents := make([]tools.DocumentContent, 6)
	for i := range documents {
		documents[i] = tools.DocumentContent{
			Name:     fmt.Sprintf("doc-%d.txt", i),
			MimeType: "text/plain",
			Text:     oversized,
		}
	}
	images := []tools.MediaContent{{Data: "aGk=", MimeType: "image/png"}}

	s := New(WithMaxToolResultTokens(100)) // 400-byte budget
	s.AddMessage(NewAgentMessage("test-agent", &chat.Message{
		Role:         chat.MessageRoleTool,
		ToolCallID:   "tc",
		Content:      content,
		MultiContent: chat.BuildToolResultMultiContent(content, images, documents),
	}))

	got := storedToolResult(t, s, "tc")
	assert.LessOrEqual(t, toolResultTextTokens(got), 100, "aggregate must never exceed the per-result cap")
	assert.Equal(t, content, got.Content, "content within budget must stay intact")

	require.Len(t, got.MultiContent, 8) // text + 6 documents + image
	assert.Equal(t, got.Content, got.MultiContent[0].Text, "duplicate text part must stay in sync")
	for i := 1; i <= 6; i++ {
		doc := got.MultiContent[i].Document
		require.NotNil(t, doc)
		assert.Equal(t, fmt.Sprintf("doc-%d.txt", i-1), doc.Name, "document metadata must be preserved")
		assert.Equal(t, "text/plain", doc.MimeType, "document metadata must be preserved")
		assert.True(t, utf8.ValidString(doc.Source.InlineText))
		assert.Less(t, len(doc.Source.InlineText), len(oversized))
	}
	assert.Contains(t, got.MultiContent[1].Document.Source.InlineText, toolResultTruncationMarker,
		"first document gets the budget left over from content")
	assert.Empty(t, got.MultiContent[6].Document.Source.InlineText,
		"documents past an exhausted budget keep metadata but no text")
	assert.Equal(t, chat.MessagePartTypeImageURL, got.MultiContent[7].Type, "non-text parts must be untouched")
}

func TestTruncateMiddleOutTinyCaps(t *testing.T) {
	t.Parallel()
	contents := []struct {
		name    string
		content string
	}{
		{"ascii", strings.Repeat("abcdefgh", 50)},
		{"three-byte runes", strings.Repeat("€", 150)},
		{"four-byte runes", strings.Repeat("🌍", 100)},
		{"mixed width runes", strings.Repeat("a€🌍", 50)},
	}
	for _, tt := range contents {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for maxTokens := 1; maxTokens <= 16; maxTokens++ {
				got := truncateMiddleOut(tt.content, maxTokens)
				assert.LessOrEqual(t, approximateTokens(got), maxTokens, "cap exceeded for maxTokens=%d", maxTokens)
				assert.Less(t, len(got), len(tt.content), "truncation must shrink for maxTokens=%d", maxTokens)
				assert.True(t, utf8.ValidString(got), "invalid UTF-8 for maxTokens=%d", maxTokens)
			}
		})
	}
}

// Regression: tiny caps used to emit the 47-byte marker around two kept
// bytes, producing results far larger than both the cap and the input.
func TestTruncateMiddleOutNeverGrowsShortInput(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"abcdefghi",            // 9 bytes of ASCII
		"🌍🌍",                   // 8 bytes, 4-byte runes
		strings.Repeat("€", 4), // 12 bytes, 3-byte runes
	}
	for _, content := range inputs {
		for maxTokens := 1; maxTokens <= 12; maxTokens++ {
			got := truncateMiddleOut(content, maxTokens)
			assert.LessOrEqual(t, len(got), len(content), "content=%q maxTokens=%d", content, maxTokens)
			assert.LessOrEqual(t, approximateTokens(got), maxTokens, "content=%q maxTokens=%d", content, maxTokens)
			assert.True(t, utf8.ValidString(got), "content=%q maxTokens=%d", content, maxTokens)
		}
	}
}

func TestTruncateMiddleOutFullMarkerWhenBudgetAllows(t *testing.T) {
	t.Parallel()
	content := "HEAD" + strings.Repeat("m", 1000) + "TAIL"

	// 13 tokens (52 bytes) is the smallest ASCII budget fitting the full
	// marker plus at least one byte of head and tail.
	got := truncateMiddleOut(content, 13)
	assert.Contains(t, got, toolResultTruncationMarker)
	assert.True(t, strings.HasPrefix(got, "HEA"), "head must survive")
	assert.True(t, strings.HasSuffix(got, "IL"), "tail must survive")
	assert.LessOrEqual(t, approximateTokens(got), 13)

	got = truncateMiddleOut(content, 64)
	assert.Contains(t, got, toolResultTruncationMarker)
	assert.True(t, strings.HasPrefix(got, "HEAD"), "head must survive")
	assert.True(t, strings.HasSuffix(got, "TAIL"), "tail must survive")
	assert.LessOrEqual(t, approximateTokens(got), 64)
}

func TestTruncateMiddleOutCompactFallbackBelowMarkerBudget(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("abcdefgh", 50)

	// 12 tokens (48 bytes) cannot fit the 47-byte marker plus a complete rune
	// on each side, so the compact ellipsis keeps real content instead.
	got := truncateMiddleOut(content, 12)
	assert.NotContains(t, got, toolResultTruncationMarker)
	assert.Contains(t, got, "...")
	assert.True(t, strings.HasPrefix(got, "abcdefgh"), "head must survive")
	assert.True(t, strings.HasSuffix(got, "abcdefgh"), "tail must survive")
	assert.LessOrEqual(t, approximateTokens(got), 12)
}
