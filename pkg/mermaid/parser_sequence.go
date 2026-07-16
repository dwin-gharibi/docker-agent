package mermaid

import "strings"

func parseSequence(doc *Document, statements []string) {
	for _, statement := range statements {
		cursor := mermaidCursor{input: statement}
		keyword, ok := cursor.readIdentifier()
		if !ok {
			continue
		}
		if strings.EqualFold(keyword, "note") {
			parseMermaidSequenceNote(doc, &cursor)
			continue
		}
		if strings.EqualFold(keyword, "participant") || strings.EqualFold(keyword, "actor") {
			id, ok := cursor.readIdentifier()
			if !ok {
				continue
			}
			label := id
			if cursor.consumeKeyword("as") {
				label = cleanMermaidLabel(cursor.remaining())
			}
			addMermaidNode(doc, id, label)
			doc.Participants = appendUniqueMermaid(doc.Participants, id)
			continue
		}

		cursor.pos = 0
		from, ok := cursor.readIdentifier()
		if !ok {
			continue
		}
		if _, ok = cursor.readEdgeOperator(); !ok {
			continue
		}
		to, ok := cursor.readIdentifier()
		if !ok || !cursor.consume(':') {
			continue
		}
		label := strings.TrimSpace(cursor.remaining())
		addMermaidNode(doc, from, from)
		addMermaidNode(doc, to, to)
		doc.Participants = appendUniqueMermaid(doc.Participants, from)
		doc.Participants = appendUniqueMermaid(doc.Participants, to)
		edge := Edge{From: from, To: to, Label: label}
		doc.Edges = append(doc.Edges, edge)
		doc.SequenceEvents = append(doc.SequenceEvents, SequenceEvent{
			Kind: SequenceMessage, From: from, To: to, Label: label,
		})
	}
}

func parseMermaidSequenceNote(doc *Document, cursor *mermaidCursor) {
	placement, ok := cursor.readIdentifier()
	if !ok {
		return
	}
	event := SequenceEvent{Kind: SequenceNote}
	switch strings.ToLower(placement) {
	case "over":
		event.Placement = NoteOver
	case "left":
		if !cursor.consumeKeyword("of") {
			return
		}
		event.Placement = NoteLeftOf
	case "right":
		if !cursor.consumeKeyword("of") {
			return
		}
		event.Placement = NoteRightOf
	default:
		return
	}

	from, ok := cursor.readIdentifier()
	if !ok {
		return
	}
	to := from
	if event.Placement == NoteOver && cursor.consume(',') {
		if to, ok = cursor.readIdentifier(); !ok {
			return
		}
	}
	if !cursor.consume(':') {
		return
	}
	event.From, event.To = from, to
	event.Label = strings.TrimSpace(cursor.remaining())
	addMermaidNode(doc, from, from)
	addMermaidNode(doc, to, to)
	doc.Participants = appendUniqueMermaid(doc.Participants, from)
	doc.Participants = appendUniqueMermaid(doc.Participants, to)
	doc.SequenceEvents = append(doc.SequenceEvents, event)
}
