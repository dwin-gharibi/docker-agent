// Package mermaid parses Mermaid diagrams and renders terminal-native views.
package mermaid

import mermaidparser "github.com/docker/docker-agent/pkg/mermaid"

// Render parses source and renders its diagram within width terminal cells.
// The boolean is false when the diagram type or syntax is unsupported.
func Render(source string, width int) (string, bool) {
	document, err := mermaidparser.Parse(source)
	if err != nil {
		return "", false
	}

	labels := make(map[string]string, len(document.Nodes))
	for id, node := range document.Nodes {
		labels[id] = node.Label
		if node.Shape == mermaidparser.ShapeDecision {
			labels[id] = "◇ " + node.Label + " ◇"
		}
	}

	switch document.Kind {
	case mermaidparser.DiagramFlowchart:
		return drawMermaidGraph(document.Edges, document.NodeOrder, labels, width), true
	case mermaidparser.DiagramSequence:
		return drawMermaidSequence(document.Edges, document.SequenceEvents, document.Participants, labels, width), true
	case mermaidparser.DiagramState:
		return drawMermaidGraph(document.Edges, document.NodeOrder, labels, width), true
	default:
		return "", false
	}
}
