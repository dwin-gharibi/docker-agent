package mermaid

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mermaidparser "github.com/docker/docker-agent/pkg/mermaid"
)

func TestRenderFlowchartDirections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		direction string
		arrow     string
		before    string
		after     string
	}{
		{name: "top down", direction: "TD", arrow: "▼", before: "Start", after: "Finish"},
		{name: "top bottom alias", direction: "TB", arrow: "▼", before: "Start", after: "Finish"},
		{name: "bottom top", direction: "BT", arrow: "▲", before: "Finish", after: "Start"},
		{name: "left right", direction: "LR", arrow: "▶", before: "Start", after: "Finish"},
		{name: "right left", direction: "RL", arrow: "◀", before: "Finish", after: "Start"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			diagram, ok := Render("flowchart "+tt.direction+"\nA[Start] -->|go| B[Finish]", 60)
			require.True(t, ok)
			assert.Contains(t, diagram, tt.arrow)
			assert.Contains(t, diagram, "go")
			assert.Less(t, strings.Index(diagram, tt.before), strings.Index(diagram, tt.after))
			for line := range strings.SplitSeq(diagram, "\n") {
				assert.LessOrEqual(t, runewidth.StringWidth(line), 60)
			}
		})
	}
}

func TestRenderRightToLeftFlowchartPreservesStructuralCharactersInLabels(t *testing.T) {
	t.Parallel()

	diagram, ok := Render("flowchart RL\nA[╭ A ▶ B ┤] -->|╰ edge ▶ ╮| B[Target]", 100)
	require.True(t, ok)
	assert.Contains(t, diagram, "╭ A ▶ B ┤")
	assert.Contains(t, diagram, "╰ edge ▶ ╮")
	assert.NotContains(t, diagram, "├ B ◀ A ╮")
}

func TestRenderRightToLeftFlowchartPointsArrowsAtTargets(t *testing.T) {
	t.Parallel()

	diagram, ok := Render("flowchart RL\nClient --> API\nAPI --> Database", 100)
	require.True(t, ok)
	lines := strings.Split(diagram, "\n")
	require.Len(t, lines, 3)
	middle := lines[1]
	assert.Regexp(t, `Database[^◀]*│◀─+┤[^A]*API[^◀]*│◀─+┤[^C]*Client`, middle)
	assert.NotContains(t, diagram, "├◀")
	assert.NotContains(t, diagram, "├ ◀")
	assert.NotContains(t, diagram, "▶")
}

func TestRenderRightToLeftFlowchartMirrorsBranchingTopology(t *testing.T) {
	t.Parallel()

	source := "A[Start] --> B{Decision}\nB -->|Yes| C[Option A]\nB -->|No| D[Option B]\nC --> E[Finish]\nD --> E"
	leftToRight, ok := Render("flowchart LR\n"+source, 100)
	require.True(t, ok)
	rightToLeft, ok := Render("flowchart RL\n"+source, 100)
	require.True(t, ok)

	assert.Equal(t, flipMermaidGraphHorizontal(leftToRight, 100, mermaidGraphLabels([]mermaidparser.Edge{
		{Label: "Yes"}, {Label: "No"},
	}, map[string]string{
		"A": "Start", "B": "◇ Decision ◇", "C": "Option A", "D": "Option B", "E": "Finish",
	})), rightToLeft)
	assert.Equal(t, 1, strings.Count(rightToLeft, "◇ Decision ◇"))
	assert.Contains(t, rightToLeft, "Option A")
	assert.Contains(t, rightToLeft, "Option B")
	assert.Contains(t, rightToLeft, "Yes")
	assert.Contains(t, rightToLeft, "No")
}

func TestRenderHorizontalFlowchartStopsParentEdgeAtVerticalBranch(t *testing.T) {
	t.Parallel()

	diagram, ok := Render("flowchart LR\nA[Root] --> B[Top]\nA --> C[Bottom]", 100)
	require.True(t, ok)
	assert.Contains(t, diagram, "├─┤")
	assert.NotContains(t, diagram, "├─┼")
}

func TestRenderHorizontalFlowchartUsesRoundedEdgeCorners(t *testing.T) {
	t.Parallel()

	diagram, ok := Render("flowchart LR\nA[Root] --> B[Top]\nA --> C[Middle]\nA --> D[Bottom]", 100)
	require.True(t, ok)
	assert.Contains(t, diagram, "╭──▶")
	assert.Contains(t, diagram, "╰──▶")
	assert.NotContains(t, diagram, "┌")
	assert.NotContains(t, diagram, "┐")
	assert.NotContains(t, diagram, "└")
	assert.NotContains(t, diagram, "┘")
}

func TestRenderHorizontalFlowchartFallsBackWithinNarrowWidth(t *testing.T) {
	t.Parallel()

	diagram, ok := Render("flowchart LR\nA[Long starting node] --> B[Long finishing node]", 16)
	require.True(t, ok)
	assert.Contains(t, diagram, "Long starting")
	assert.Contains(t, diagram, "Long finish")
	for line := range strings.SplitSeq(diagram, "\n") {
		assert.LessOrEqual(t, runewidth.StringWidth(line), 16)
	}
}
