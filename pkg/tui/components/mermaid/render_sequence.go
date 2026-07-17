package mermaid

import (
	"strings"

	mermaidparser "github.com/docker/docker-agent/pkg/mermaid"
)

func drawMermaidSequence(
	edges []mermaidparser.Edge,
	events []mermaidparser.SequenceEvent,
	participants []string,
	nodes map[string]string,
	width int,
) string {
	var order []string
	seen := make(map[string]bool)
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			order = append(order, id)
		}
	}
	for _, id := range participants {
		add(id)
	}
	for _, edge := range edges {
		add(edge.From)
		add(edge.To)
	}
	if len(order) < 2 || width/len(order) < 10 {
		return drawMermaidSequenceLinear(edges, events, participants, nodes, width)
	}

	width = max(width, 8)
	cellWidth := width / len(order)
	centers := make(map[string]int, len(order))
	for i, id := range order {
		centers[id] = i*cellWidth + cellWidth/2
	}

	var out []string
	for row := range 3 {
		canvas := mermaidCanvas(width)
		for _, id := range order {
			label := nodes[id]
			boxWidth := min(max(mermaidStringWidth(label)+4, 8), cellWidth-2)
			parts := boxParts(label, boxWidth)
			writeMermaidCanvas(canvas, centers[id]-boxWidth/2, parts[row])
		}
		if row == 2 {
			for _, id := range order {
				canvas[centers[id]] = "┬"
			}
		}
		out = append(out, mermaidCanvasText(canvas))
	}
	out = append(out, mermaidLifeline(order, centers, width))

	if len(events) == 0 {
		for _, edge := range edges {
			events = append(events, mermaidparser.SequenceEvent{
				Kind: mermaidparser.SequenceMessage, From: edge.From, To: edge.To, Label: edge.Label,
			})
		}
	}
	for _, event := range events {
		switch event.Kind {
		case mermaidparser.SequenceMessage:
			out = append(out, renderSequenceMessage(event, order, centers, width)...)
		case mermaidparser.SequenceNote:
			out = append(out, renderSequenceNote(event, order, centers, width)...)
		}
	}
	return strings.Join(out, "\n")
}

func drawMermaidSequenceLinear(
	edges []mermaidparser.Edge,
	events []mermaidparser.SequenceEvent,
	participants []string,
	nodes map[string]string,
	width int,
) string {
	if len(events) == 0 {
		return drawMermaidEdges(edges, participants, nodes, width)
	}
	var out []string
	for _, event := range events {
		switch event.Kind {
		case mermaidparser.SequenceMessage:
			out = append(out, strings.Split(drawMermaidEdges([]mermaidparser.Edge{{
				From: event.From, To: event.To, Label: event.Label,
			}}, nil, nodes, width), "\n")...)
		case mermaidparser.SequenceNote:
			out = append(out, drawMermaidBox("Note: "+event.Label, width)...)
		}
	}
	return strings.Join(out, "\n")
}

func renderSequenceMessage(event mermaidparser.SequenceEvent, order []string, centers map[string]int, width int) []string {
	from, fromOK := centers[event.From]
	to, toOK := centers[event.To]
	if !fromOK || !toOK {
		return nil
	}
	canvas := sequenceCanvas(order, centers, width)
	if from == to {
		label := truncateMermaid(event.Label, max(width-from-7, 1))
		writeMermaidCanvas(canvas, from, "├── "+label+" ──↩")
	} else {
		left, right := min(from, to), max(from, to)
		for x := left + 1; x < right; x++ {
			canvas[x] = "─"
		}
		label := truncateMermaid(event.Label, max(right-left-4, 1))
		labelStart := left + (right-left-mermaidStringWidth(label))/2
		writeMermaidCanvas(canvas, labelStart, " "+label+" ")
		if from < to {
			canvas[from], canvas[to] = "├", "▶"
		} else {
			canvas[to], canvas[from] = "◀", "┤"
		}
	}
	return []string{mermaidCanvasText(canvas), mermaidLifeline(order, centers, width)}
}

func renderSequenceNote(event mermaidparser.SequenceEvent, order []string, centers map[string]int, width int) []string {
	from, fromOK := centers[event.From]
	to, toOK := centers[event.To]
	if !fromOK || !toOK {
		return nil
	}
	labelWidth := mermaidStringWidth(event.Label)
	boxWidth := min(max(labelWidth+4, 10), width)
	start := 0
	switch event.Placement {
	case mermaidparser.NoteLeftOf:
		start = from - boxWidth - 2
	case mermaidparser.NoteRightOf:
		start = from + 2
	case mermaidparser.NoteOver:
		start = (min(from, to) + max(from, to) - boxWidth) / 2
	}
	start = min(max(start, 0), max(width-boxWidth, 0))

	parts := boxParts(event.Label, boxWidth)
	out := make([]string, 0, len(parts)+1)
	for _, part := range parts {
		canvas := sequenceCanvas(order, centers, width)
		writeMermaidCanvas(canvas, start, part)
		out = append(out, mermaidCanvasText(canvas))
	}
	return append(out, mermaidLifeline(order, centers, width))
}

func sequenceCanvas(order []string, centers map[string]int, width int) []string {
	canvas := mermaidCanvas(width)
	for _, id := range order {
		canvas[centers[id]] = "│"
	}
	return canvas
}
