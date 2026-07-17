package markdown

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tui/components/mermaid"
)

func TestFastRendererRendersMermaidFlowchartInline(t *testing.T) {
	t.Parallel()

	result, blocks, err := NewFastRenderer(60).RenderWithCodeBlocks("```mermaid\nflowchart LR\n  A[Build image] -->|success| B{Publish}\n```")
	require.NoError(t, err)
	plain := stripANSI(result)
	assert.Contains(t, plain, "Build image")
	assert.Contains(t, plain, "Publish")
	assert.Contains(t, plain, "success")
	assert.Contains(t, plain, "▶")
	assert.Contains(t, plain, "Build image")
	assert.Contains(t, plain, "◇ Publish ◇")
	assert.NotContains(t, plain, "flowchart LR")
	assert.Empty(t, blocks, "rendered diagrams are not source-code copy targets")
}

func TestFastRendererKeepsDeclaredNodeLabelsAndUsesCompactBoxes(t *testing.T) {
	t.Parallel()

	result, err := NewFastRenderer(100).Render("```mermaid\nflowchart LR\nA[Start] --> B{Validate}\nB -->|yes| C[Done]\n```")
	require.NoError(t, err)
	plain := stripANSI(result)
	assert.Equal(t, 1, strings.Count(plain, "Validate"))
	assert.NotContains(t, plain, "│ B │")
	for line := range strings.SplitSeq(plain, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "╭") {
			assert.Less(t, len([]rune(trimmed)), 60, "nodes should use their content width, not half the viewport")
		}
	}
}

func TestFastRendererRendersFlowchartTopology(t *testing.T) {
	t.Parallel()

	source := "```mermaid\ngraph TD\nA[\"Client: CLOSED\"] -->|\"Send SYN\"| B[\"Client: SYN-SENT\"]\nB --> C{\"Server reachable?\"}\nC -->|\"Yes\"| D[\"Server: SYN-RECEIVED\"]\nC -->|\"No / timeout\"| E[\"Retransmit SYN\"]\nE --> B\n```"
	result, err := NewFastRenderer(80).Render(source)
	require.NoError(t, err)
	plain := stripANSI(result)
	assert.Contains(t, plain, "│ Client: CLOSED │")
	assert.Contains(t, plain, "Send SYN")
	assert.Contains(t, plain, "▼")
	assert.Contains(t, plain, "│ Client: SYN-SENT │")
	assert.Contains(t, plain, "Yes")
	assert.Contains(t, plain, "No / timeout")
	assert.Contains(t, plain, "│ Server: SYN-RECEIVED │")
	assert.Contains(t, plain, "│ Retransmit SYN │")
	assert.Contains(t, plain, "↩ Client: SYN-SENT")
	assert.NotContains(t, plain, "│ B │")
}

func TestMermaidFlowchartCentersParentAndSplitsBranches(t *testing.T) {
	t.Parallel()

	diagram, ok := mermaid.Render("flowchart TD\nA[Decision] -->|yes| B[Left branch]\nA -->|no| C[Right branch]", 80)
	require.True(t, ok)
	lines := strings.Split(diagram, "\n")
	require.GreaterOrEqual(t, len(lines), 9)
	root := strings.Index(lines[1], "Decision")
	left := strings.Index(lines[len(lines)-2], "Left branch")
	right := strings.Index(lines[len(lines)-2], "Right branch")
	assert.Less(t, left, root)
	assert.Greater(t, right, root)
	assert.Contains(t, diagram, "╭")
	assert.Contains(t, diagram, "┴")
	assert.Contains(t, diagram, "yes")
	assert.Contains(t, diagram, "no")
}

func TestFastRendererRendersMermaidSequenceDiagramInline(t *testing.T) {
	t.Parallel()

	result, err := NewFastRenderer(50).Render("```MERMAID\nsequenceDiagram\nparticipant U as User\nparticipant A as Agent\nU->>A: Ask a question\nA-->>U: Answer\n```")
	require.NoError(t, err)
	plain := stripANSI(result)
	assert.Contains(t, plain, "User")
	assert.Contains(t, plain, "Agent")
	assert.Contains(t, plain, "Ask a question")
	assert.Contains(t, plain, "Answer")
	assert.NotContains(t, plain, "sequenceDiagram")
}

func TestFastRendererMermaidUsesVerticalLayoutWhenNarrow(t *testing.T) {
	t.Parallel()

	result, err := NewFastRenderer(16).Render("```mermaid\ngraph TD\nA[Start] --> B[Finish]\n```")
	require.NoError(t, err)
	plain := stripANSI(result)
	assert.Contains(t, plain, "Start")
	assert.Contains(t, plain, "Finish")
	assert.Contains(t, plain, "▼")
	for line := range strings.SplitSeq(plain, "\n") {
		assert.LessOrEqual(t, len([]rune(strings.TrimRight(line, " "))), 16)
	}
}

func TestFastRendererAlignsSequenceParticipantsOnLifelines(t *testing.T) {
	t.Parallel()

	source := "```mermaid\nsequenceDiagram\nactor User\nparticipant WebApp as Web App\nparticipant API\nparticipant DB as Database\nUser->>WebApp: Submit form\nWebApp->>API: POST /items\nAPI->>DB: Save item\nDB-->>API: Item saved\nAPI-->>WebApp: 201 Created\nWebApp-->>User: Show success message\n```"
	result, err := NewFastRenderer(100).Render(source)
	require.NoError(t, err)
	plain := stripANSI(result)
	assert.Contains(t, plain, "Web App")
	assert.Contains(t, plain, "Database")
	assert.Contains(t, plain, "◀")
	assert.NotContains(t, plain, "DB-")
	lines := strings.Split(plain, "\n")
	require.GreaterOrEqual(t, len(lines), 6)
	headerConnector := strings.Index(lines[2], "┬")
	lifeline := strings.Index(lines[3], "│")
	assert.Equal(t, runewidth.StringWidth(lines[2][:headerConnector]), runewidth.StringWidth(lines[3][:lifeline]))
}

func TestFastRendererRendersMermaidStateDiagramInline(t *testing.T) {
	t.Parallel()

	result, err := NewFastRenderer(50).Render("```mermaid\nstateDiagram-v2\n[*] --> Idle\nstate \"Processing request\" as Working\nIdle --> Working: begin\nWorking --> [*]: complete\n```")
	require.NoError(t, err)
	plain := stripANSI(result)
	assert.Contains(t, plain, "Start")
	assert.Contains(t, plain, "Idle")
	assert.Contains(t, plain, "Processing request")
	assert.Contains(t, plain, "End")
	assert.NotContains(t, plain, "stateDiagram-v2")
}

func TestFastRendererUnsupportedMermaidFallsBackToCode(t *testing.T) {
	t.Parallel()

	result, blocks, err := NewFastRenderer(60).RenderWithCodeBlocks("```mermaid\npie\n  title Pets\n  \"Dogs\" : 4\n```")
	require.NoError(t, err)
	assert.Contains(t, stripANSI(result), "pie")
	require.Len(t, blocks, 1)
}
