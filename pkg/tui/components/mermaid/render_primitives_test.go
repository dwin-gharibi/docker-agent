package mermaid

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoxPartsAccountsForEmojiGraphemeWidth(t *testing.T) {
	t.Parallel()

	parts := boxParts("⚙️ Runtime", mermaidStringWidth("⚙️ Runtime")+4)
	assert.Equal(t, mermaidStringWidth(parts[0]), mermaidStringWidth(parts[1]))
	assert.Equal(t, mermaidStringWidth(parts[1]), mermaidStringWidth(parts[2]))
	assert.Equal(t, "│ ⚙️ Runtime │", parts[1])
}

func TestWriteMermaidCanvasKeepsEmojiGraphemeClustersTogether(t *testing.T) {
	t.Parallel()

	canvas := mermaidCanvas(8)
	writeMermaidCanvas(canvas, 1, "⚙️X")
	writeMermaidCanvas(canvas, 7, ".")
	line := mermaidCanvasText(canvas)
	assert.Contains(t, line, "⚙️X")
	assert.Equal(t, 8, mermaidStringWidth(line))
}

func TestBoxPartsUsesRoundedCorners(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"╭──────╮",
		"│ Node │",
		"╰──────╯",
	}, boxParts("Node", 8))
}

func TestWriteMermaidCanvasAccountsForWideAndCombiningRunes(t *testing.T) {
	t.Parallel()

	canvas := mermaidCanvas(12)
	writeMermaidCanvas(canvas, 2, "界A")
	writeMermaidCanvas(canvas, 11, ".")
	line := mermaidCanvasText(canvas)
	assert.Equal(t, 12, runewidth.StringWidth(line))
	index := strings.Index(line, "A")
	require.NotEqual(t, -1, index)
	assert.Equal(t, 4, runewidth.StringWidth(line[:index]))

	canvas = mermaidCanvas(8)
	writeMermaidCanvas(canvas, 1, "e\u0301x")
	writeMermaidCanvas(canvas, 7, ".")
	line = mermaidCanvasText(canvas)
	assert.Contains(t, line, "éx")
	assert.Equal(t, 8, runewidth.StringWidth(line))
}
