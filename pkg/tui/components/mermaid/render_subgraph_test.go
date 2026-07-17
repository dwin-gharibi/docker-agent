package mermaid

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderFlowchartSubgraph(t *testing.T) {
	t.Parallel()

	diagram, ok := Render(`flowchart LR
client[Client] --> api
subgraph backend[Backend]
  api[API] --> db[Database]
  api --> cache[Cache]
end`, 100)
	require.True(t, ok)
	assert.Contains(t, diagram, " Backend ")
	assert.Contains(t, diagram, "Client")
	assert.Contains(t, diagram, "API")
	assert.Contains(t, diagram, "Database")
	assert.Contains(t, diagram, "Cache")
	assert.Contains(t, diagram, "╭─ Backend ")
}

func TestRenderSubgraphMatchesWholeNodeLabel(t *testing.T) {
	t.Parallel()

	diagram, ok := Render(`flowchart LR
long[DeployProd] --> short[Deploy]
subgraph production[Production]
  short
end`, 80)
	require.True(t, ok)
	lines := strings.Split(diagram, "\n")
	var top string
	for _, line := range lines {
		if strings.Contains(line, "Production") {
			top = line
			break
		}
	}
	require.NotEmpty(t, top)
	deployProdLine := ""
	for _, line := range lines {
		if strings.Contains(line, "DeployProd") {
			deployProdLine = line
			break
		}
	}
	require.NotEmpty(t, deployProdLine)
	assert.Greater(t, strings.Index(top, "Production"), strings.Index(deployProdLine, "DeployProd"))
}

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

func TestRenderFlowchartSubgraphPadsRightmostNode(t *testing.T) {
	t.Parallel()

	diagram, ok := Render(`flowchart LR
subgraph infrastructure[Infrastructure]
  runtime[Tool Runtime] --> docker[Docker]
end`, 80)
	require.True(t, ok)
	lines := strings.Split(diagram, "\n")
	var nodeLine string
	for _, line := range lines {
		if strings.Contains(line, "Docker") {
			nodeLine = line
			break
		}
	}
	require.NotEmpty(t, nodeLine)
	assert.Regexp(t, `Docker[^│]*│ {2,}│`, nodeLine)
}

func TestRenderSiblingFlowchartSubgraphsHaveHorizontalSpacing(t *testing.T) {
	t.Parallel()

	diagram, ok := Render(`flowchart LR
subgraph agents[Agents]
  router[Router] --> developer[Developer]
end
subgraph infra[Infrastructure]
  runtime[Runtime] --> docker[Docker]
end
developer --> runtime`, 100)
	require.True(t, ok)
	lines := strings.Split(diagram, "\n")
	require.NotEmpty(t, lines)
	assert.Regexp(t, `╮ {2,}╭`, lines[0])
}

func TestRenderNestedFlowchartSubgraphs(t *testing.T) {
	t.Parallel()

	diagram, ok := Render(`flowchart TD
subgraph platform[Platform]
  gateway[Gateway] --> api
  subgraph services[Services]
    api[API] --> db[Database]
  end
end`, 80)
	require.True(t, ok)
	assert.Equal(t, 1, strings.Count(diagram, " Platform "))
	assert.Equal(t, 1, strings.Count(diagram, " Services "))
	assert.Contains(t, diagram, "Gateway")
	assert.Contains(t, diagram, "Database")
}
