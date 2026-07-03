package dialog

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/runtime"
)

func testBreakdown() *runtime.ContextBreakdown {
	return &runtime.ContextBreakdown{
		SystemPrompt:      runtime.ContextCategory{Tokens: 1200, Items: 3},
		ToolDefinitions:   runtime.ContextCategory{Tokens: 8400, Items: 23},
		PromptFiles:       runtime.ContextCategory{Tokens: 600, Items: 1},
		Messages:          runtime.ContextCategory{Tokens: 6100, Items: 12},
		ToolResults:       runtime.ContextCategory{Tokens: 8200, Items: 18},
		CompactionSummary: runtime.ContextCategory{Tokens: 900, Items: 1},
		ContextLimit:      128_000,
		Model:             "openai/gpt-4o",
	}
}

func TestNewContextDialog(t *testing.T) {
	t.Parallel()

	dialog := NewContextDialog(testBreakdown())
	require.NotNil(t, dialog)
}

func TestContextDialogView(t *testing.T) {
	t.Parallel()

	dialog := NewContextDialog(testBreakdown())
	dialog.SetSize(100, 50)
	view := dialog.View()

	assert.Contains(t, view, "Context Window")
	assert.Contains(t, view, "openai/gpt-4o")
	assert.Contains(t, view, "limit: 128.0K tokens")

	// Every category is listed, even implicitly small ones.
	assert.Contains(t, view, "System prompt")
	assert.Contains(t, view, "Tool definitions")
	assert.Contains(t, view, "Prompt files")
	assert.Contains(t, view, "Messages")
	assert.Contains(t, view, "Tool results")
	assert.Contains(t, view, "Compaction summary")
	assert.Contains(t, view, "Free space")

	// Item counts and the estimate disclaimer.
	assert.Contains(t, view, "(23 tools)")
	assert.Contains(t, view, "(12 messages)")
	assert.Contains(t, view, "(1 file)")
	assert.Contains(t, view, "estimates")

	// Usage summary: 25.4K used of 128K.
	assert.Contains(t, view, "~25.4K of 128.0K tokens")
}

func TestContextDialogViewUnknownLimit(t *testing.T) {
	t.Parallel()

	b := testBreakdown()
	b.ContextLimit = 0
	b.Model = "external-harness"

	dialog := NewContextDialog(b)
	dialog.SetSize(100, 50)
	view := dialog.View()

	assert.Contains(t, view, "context limit unknown")
	assert.Contains(t, view, "tokens estimated")
	assert.NotContains(t, view, "Free space")
}

func TestContextDialogEmptyBreakdown(t *testing.T) {
	t.Parallel()

	dialog := NewContextDialog(&runtime.ContextBreakdown{Model: "openai/gpt-4o"})
	dialog.SetSize(100, 50)
	view := dialog.View()

	assert.Contains(t, view, "Context Window")
	assert.Contains(t, view, "System prompt")
}

func TestContextRowsFreeSpace(t *testing.T) {
	t.Parallel()

	rows := contextRows(testBreakdown())
	require.Len(t, rows, 7)
	last := rows[len(rows)-1]
	assert.True(t, last.free)
	// 128000 - 25400 estimated tokens used.
	assert.Equal(t, int64(102_600), last.tokens)

	// An over-budget estimate must not produce a negative free-space row.
	over := testBreakdown()
	over.Messages.Tokens = 500_000
	rows = contextRows(over)
	require.Len(t, rows, 6)
	for _, row := range rows {
		assert.False(t, row.free)
	}
}

func TestContextPercentLabel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "-", percentLabel(0, 1000))
	assert.Equal(t, "-", percentLabel(10, 0))
	assert.Equal(t, "<1%", percentLabel(1, 1000))
	assert.Equal(t, "5%", percentLabel(50, 1000))
	assert.Equal(t, "100%", percentLabel(1000, 1000))
}

func TestContextScaleTokens(t *testing.T) {
	t.Parallel()

	b := testBreakdown()
	assert.Equal(t, int64(128_000), scaleTokens(b))

	// Estimate exceeding the limit scales against the estimate so the bar
	// and percentages stay consistent (never above 100%).
	b.Messages.Tokens = 500_000
	assert.Equal(t, b.TotalTokens(), scaleTokens(b))

	// Unknown limit scales against the estimated total.
	b = testBreakdown()
	b.ContextLimit = 0
	assert.Equal(t, b.TotalTokens(), scaleTokens(b))
}

func TestRenderContextBarWidth(t *testing.T) {
	t.Parallel()

	b := testBreakdown()
	rows := contextRows(b)

	for _, width := range []int{1, 10, 40, 80, 200} {
		bar := renderContextBar(rows, scaleTokens(b), width)
		assert.Equal(t, width, lipgloss.Width(bar), "bar must span exactly %d cells", width)
	}

	assert.Empty(t, renderContextBar(rows, 0, 40), "zero scale renders no bar")
	assert.Empty(t, renderContextBar(rows, scaleTokens(b), 0), "zero width renders no bar")
}

func TestContextDialogPlainText(t *testing.T) {
	t.Parallel()

	d := &contextDialog{breakdown: testBreakdown()}
	text := d.renderPlainText()

	assert.Contains(t, text, "Context Window")
	assert.Contains(t, text, "openai/gpt-4o")
	assert.Contains(t, text, "Tool definitions")
	assert.Contains(t, text, "8.4K")
	assert.Contains(t, text, "(23 tools)")
	assert.Contains(t, text, "Free space")
	assert.Contains(t, text, "estimates")
	assert.NotContains(t, text, "\x1b[", "plain text must carry no ANSI escapes")
}
