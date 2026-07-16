package mermaid

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
