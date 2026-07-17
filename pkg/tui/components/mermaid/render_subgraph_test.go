package mermaid

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindMermaidTextBoundsMatchesWholeNodeLabel(t *testing.T) {
	t.Parallel()

	lines := []string{
		"╭────────────╮     ╭────────╮",
		"│ DeployProd │────▶│ Deploy │",
		"╰────────────╯     ╰────────╯",
	}
	bounds := findMermaidTextBounds(lines, "Deploy")
	require.True(t, bounds.ok)
	assert.Equal(t, mermaidStringWidth("│ DeployProd │────▶│ "), bounds.left+2)
}
