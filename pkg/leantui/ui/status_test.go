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
	assert.Equal(t, ContextBarWidth, DisplayWidth(RenderBar(0.5)))
	assert.Equal(t, ContextBarWidth, DisplayWidth(RenderBar(0)))
	assert.Equal(t, ContextBarWidth, DisplayWidth(RenderBar(1)))
	assert.Equal(t, ContextBarWidth, DisplayWidth(RenderBar(1.5))) // clamped
}

func TestRenderContextShowsZerosBeforeUsage(t *testing.T) {
	t.Parallel()
	out := RenderContext(StatusModel{})
	assert.NotContains(t, out, "context")
	assert.Contains(t, out, "0% · 0/0")
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
