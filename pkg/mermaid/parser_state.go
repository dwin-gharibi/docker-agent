package mermaid

import "strings"

func parseState(doc *Document, statements []string) {
	pseudoState := 0
	for _, statement := range statements {
		cursor := mermaidCursor{input: statement}
		if cursor.consumeKeyword("state") {
			label, ok := cursor.readQuoted()
			if !ok || !cursor.consumeKeyword("as") {
				continue
			}
			id, ok := cursor.readIdentifier()
			if ok {
				addMermaidNode(doc, id, label)
			}
			continue
		}

		from, ok := cursor.readStateReference()
		if !ok {
			continue
		}
		if operator, ok := cursor.readEdgeOperator(); !ok || operator != "-->" {
			continue
		}
		to, ok := cursor.readStateReference()
		if !ok {
			continue
		}
		label := ""
		if cursor.consume(':') {
			label = strings.TrimSpace(cursor.remaining())
		}
		if from == "[*]" {
			pseudoState++
			from = mermaidPseudoStateID("start", pseudoState)
			addMermaidNode(doc, from, "Start")
		} else {
			addMermaidNode(doc, from, from)
		}
		if to == "[*]" {
			pseudoState++
			to = mermaidPseudoStateID("end", pseudoState)
			addMermaidNode(doc, to, "End")
		} else {
			addMermaidNode(doc, to, to)
		}
		doc.Edges = append(doc.Edges, Edge{From: from, To: to, Label: label})
	}
}

func mermaidPseudoStateID(kind string, index int) string {
	return "__" + kind + "_" + strings.Repeat("_", index)
}
