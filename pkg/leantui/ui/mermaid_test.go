package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

func TestRenderAssistantLinesRendersMermaidInline(t *testing.T) {
	t.Parallel()

	lines := RenderAssistantLines("Before\n\n```mermaid\nflowchart LR\nA[Plan] --> B[Execute]\n```\n\nAfter", 48)
	plain := ansi.Strip(strings.Join(lines, "\n"))
	assert.Contains(t, plain, "Plan")
	assert.Contains(t, plain, "Execute")
	assert.Contains(t, plain, "▶")
	assert.NotContains(t, plain, "flowchart LR")
	assert.Contains(t, plain, "Before")
	assert.Contains(t, plain, "After")
}
