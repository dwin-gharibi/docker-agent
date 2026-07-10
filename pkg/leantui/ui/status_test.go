package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatTokens(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "500", FormatTokens(500))
	assert.Equal(t, "999", FormatTokens(999))
	assert.Equal(t, "1.0k", FormatTokens(1000))
	assert.Equal(t, "1.2k", FormatTokens(1234))
	assert.Equal(t, "1.0M", FormatTokens(1_000_000))
	assert.Equal(t, "2.5M", FormatTokens(2_500_000))
}

func TestComposeLineRightAligns(t *testing.T) {
	t.Parallel()
	out := ComposeLine("left", "right", 20)
	assert.Equal(t, 20, DisplayWidth(out))
	assert.GreaterOrEqual(t, len(out), len("left")+len("right"))
	assert.Contains(t, out, "left")
	assert.Contains(t, out, "right")
}

func TestComposeLineTruncatesLeft(t *testing.T) {
	t.Parallel()
	out := ComposeLine("a very long left side that does not fit", "right", 15)
	assert.LessOrEqual(t, DisplayWidth(out), 15)
	assert.Contains(t, out, "right")
}

func TestRenderBarWidth(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ContextBarWidth, DisplayWidth(RenderBar(0.5, 0)))
	assert.Equal(t, ContextBarWidth, DisplayWidth(RenderBar(0, 0)))
	assert.Equal(t, ContextBarWidth, DisplayWidth(RenderBar(1, 0)))
	assert.Equal(t, ContextBarWidth, DisplayWidth(RenderBar(1.5, 0))) // clamped
	assert.Equal(t, ContextBarWidth, DisplayWidth(RenderBar(0.8, 0.5)))
}

func TestRenderContextShowsZerosBeforeUsage(t *testing.T) {
	t.Parallel()
	out := RenderContext(StatusModel{})
	assert.NotContains(t, out, "context")
	assert.Contains(t, out, "0% · 0/0")
}

func TestRenderContextCompacting(t *testing.T) {
	t.Parallel()
	d := StatusModel{ContextLength: 9_000, ContextLimit: 10_000, Compacting: true}

	out := RenderContext(d)
	assert.Contains(t, out, "compacting…")
	assert.NotContains(t, out, "90%", "the percentage yields to the compacting indicator")
	assert.Contains(t, out, "9.0k/10.0k", "token counts stay visible while compacting")

	d.Compacting = false
	out = RenderContext(d)
	assert.Contains(t, out, "90%")
	assert.NotContains(t, out, "compacting…")
}

func TestRenderContextCompactingWithoutLimit(t *testing.T) {
	t.Parallel()
	out := RenderContext(StatusModel{Compacting: true})
	assert.Contains(t, out, "compacting…")
}

func TestRenderStatusFitsWidth(t *testing.T) {
	t.Parallel()
	d := StatusModel{
		WorkingDir:    "/home/user/project",
		Branch:        "main",
		Agent:         "coder",
		Model:         "openai/gpt-5",
		Thinking:      "high",
		ContextLength: 24_000,
		ContextLimit:  200_000,
		Tokens:        24_000,
		Cost:          0.05,
		CostKnown:     true,
	}
	lines := RenderStatus(d, 80)
	assert.Len(t, lines, 2)
	assert.Contains(t, strings.Join(lines, "\n"), "$0.05")
	for _, l := range lines {
		assert.LessOrEqual(t, DisplayWidth(l), 80)
	}
}
