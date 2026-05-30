package root

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseAgentPickerRefs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"empty defaults", "", []string{"default", "coder"}},
		{"whitespace defaults", "   ", []string{"default", "coder"}},
		{"single ref", "coder", []string{"coder"}},
		{"multiple refs", "default,coder", []string{"default", "coder"}},
		{"trims whitespace", " default , coder ", []string{"default", "coder"}},
		{"drops empty entries", "default,,coder,", []string{"default", "coder"}},
		{"only commas defaults", ",,,", []string{"default", "coder"}},
		{"external refs", "default,agentcatalog/pirate", []string{"default", "agentcatalog/pirate"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseAgentPickerRefs(tt.raw))
		})
	}
}

func TestPrependAgentRef(t *testing.T) {
	assert.Equal(t, []string{"coder"}, prependAgentRef("coder", nil))
	assert.Equal(t, []string{"coder", "hello"}, prependAgentRef("coder", []string{"hello"}))
	assert.Equal(t, []string{"coder", "a", "b"}, prependAgentRef("coder", []string{"a", "b"}))
}

func TestTruncateDetail(t *testing.T) {
	// Collapses newlines and runs of whitespace into single spaces.
	assert.Equal(t, "a b c", truncateDetail("a\nb\t  c", 80))
	// Truncates to width with an ellipsis.
	assert.Equal(t, "hel…", truncateDetail("hello world", 4))
	// Empty / whitespace-only input collapses to empty.
	assert.Empty(t, truncateDetail("   \n\t ", 80))
}

func TestAgentPickerRenderNoPanic(t *testing.T) {
	choices := []agentChoice{
		{ref: "default", description: "A helpful AI assistant"},
		{ref: "agentcatalog/some-really-long-agent-reference-name", description: strings.Repeat("very long description ", 20)},
		{ref: "broken", err: errors.New("multi\nline\nerror that is also quite long and should be truncated cleanly")},
	}
	m := newAgentPickerModel(choices)

	// Render across a range of widths, including degenerate ones, to make
	// sure width math never produces a panic or a negative truncation width.
	for _, w := range []int{0, 1, 10, 30, 80, 200} {
		m.width = w
		m.height = 24
		assert.NotPanics(t, func() { _ = m.render() })
	}
}

func TestAgentPickerModelNavigation(t *testing.T) {
	m := newAgentPickerModel([]agentChoice{
		{ref: "default"},
		{ref: "coder"},
	})

	// Up at the top is a no-op.
	m.moveUp()
	assert.Equal(t, 0, m.cursor)

	m.moveDown()
	assert.Equal(t, 1, m.cursor)

	// Down at the bottom is a no-op.
	m.moveDown()
	assert.Equal(t, 1, m.cursor)

	m.moveUp()
	assert.Equal(t, 0, m.cursor)
}
