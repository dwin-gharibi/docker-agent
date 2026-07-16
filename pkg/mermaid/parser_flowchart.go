package mermaid

import "strings"

func parseFlowchart(doc *Document, statements []string) {
	for _, statement := range statements {
		if mermaidFlowchartDirective(statement) {
			continue
		}
		cursor := mermaidCursor{input: statement}
		from, ok := cursor.readNode()
		if !ok {
			continue
		}
		addMermaidNode(doc, from.id, from.Label, from.shape)

		parsedEdge := false
		for {
			label, ok := cursor.readFlowchartConnector()
			if !ok {
				break
			}
			to, ok := cursor.readNode()
			if !ok {
				break
			}
			addMermaidNode(doc, to.id, to.Label, to.shape)
			doc.Edges = append(doc.Edges, Edge{From: from.id, To: to.id, Label: cleanMermaidLabel(label)})
			from = to
			parsedEdge = true
		}
		if !parsedEdge {
			addMermaidOrder(doc, from.id)
		}
	}
}

func (c *mermaidCursor) readFlowchartConnector() (string, bool) {
	position := c.pos
	if _, ok := c.readEdgeOperator(); ok {
		label, _ := c.readPipeLabel()
		return label, true
	}

	c.pos = position
	c.skipSpace()
	if !c.hasPrefix("--") {
		return "", false
	}
	c.pos += 2
	labelStart := c.pos
	if end := strings.Index(c.input[c.pos:], "-->"); end >= 0 {
		c.pos += end
		label := c.input[labelStart:c.pos]
		c.pos += len("-->")
		return label, true
	}
	c.pos = position
	return "", false
}

func mermaidFlowchartDirective(statement string) bool {
	fields := strings.Fields(statement)
	if len(fields) == 0 {
		return true
	}
	switch strings.ToLower(fields[0]) {
	case "direction", "subgraph", "end", "classdef", "class", "style", "click", "linkstyle":
		return true
	default:
		return false
	}
}
