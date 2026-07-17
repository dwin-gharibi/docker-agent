package mermaid

import (
	"slices"
	"strings"

	mermaidparser "github.com/docker/docker-agent/pkg/mermaid"
)

type mermaidBounds struct {
	left, top, right, bottom int
	ok                       bool
}

func drawMermaidSubgraphs(diagram string, subgraphs []mermaidparser.Subgraph, nodes map[string]string, width int) string {
	if len(subgraphs) == 0 || diagram == "" {
		return diagram
	}

	lines := strings.Split(diagram, "\n")
	padding := 2 * mermaidSubgraphDepth(subgraphs)
	lines = append(make([]string, padding), lines...)
	lines = append(lines, make([]string, padding)...)
	canvasWidth := 0
	for _, line := range lines {
		canvasWidth = max(canvasWidth, mermaidStringWidth(line))
	}
	canvasWidth = max(canvasWidth, width)
	canvas := make([][]string, len(lines))
	for i, line := range lines {
		canvas[i] = mermaidCanvas(canvasWidth)
		writeMermaidCanvas(canvas[i], 0, line)
	}

	bounds := make(map[string]mermaidBounds, len(subgraphs))
	for _, subgraph := range slices.Backward(subgraphs) {
		groupBounds := mermaidSubgraphBounds(lines, subgraph, nodes, bounds, subgraphs)
		if !groupBounds.ok {
			continue
		}
		groupBounds.left = max(groupBounds.left-1, 0)
		groupBounds.right = min(groupBounds.right+3, canvasWidth-1)
		groupBounds.top = max(groupBounds.top-2, 0)
		groupBounds.bottom = min(groupBounds.bottom+1, len(canvas)-1)
		bounds[subgraph.ID] = groupBounds
	}

	for _, subgraph := range subgraphs {
		groupBounds, ok := bounds[subgraph.ID]
		if !ok || groupBounds.right-groupBounds.left < 2 || groupBounds.bottom-groupBounds.top < 2 {
			continue
		}
		drawMermaidSubgraphBox(canvas, groupBounds, subgraph.Label)
	}

	out := make([]string, len(canvas))
	for i := range canvas {
		out[i] = mermaidCanvasText(canvas[i])
	}
	return strings.Join(out, "\n")
}

func mermaidSubgraphDepth(subgraphs []mermaidparser.Subgraph) int {
	parents := make(map[string]string, len(subgraphs))
	for _, subgraph := range subgraphs {
		parents[subgraph.ID] = subgraph.ParentID
	}
	depth := 1
	for _, subgraph := range subgraphs {
		currentDepth := 1
		for parent := subgraph.ParentID; parent != ""; parent = parents[parent] {
			currentDepth++
		}
		depth = max(depth, currentDepth)
	}
	return depth
}

func mermaidSubgraphBounds(lines []string, subgraph mermaidparser.Subgraph, nodes map[string]string, known map[string]mermaidBounds, subgraphs []mermaidparser.Subgraph) mermaidBounds {
	var result mermaidBounds
	for _, id := range subgraph.Nodes {
		result = mergeMermaidBounds(result, findMermaidTextBounds(lines, nodes[id]))
	}
	for _, child := range subgraphs {
		if child.ParentID == subgraph.ID {
			result = mergeMermaidBounds(result, known[child.ID])
		}
	}
	return result
}

func findMermaidTextBounds(lines []string, text string) mermaidBounds {
	if text == "" {
		return mermaidBounds{}
	}
	for y, line := range lines {
		for _, rightBorder := range []string{" │", " ├", " ┤"} {
			box := "│ " + text + rightBorder
			before, _, found := strings.Cut(line, box)
			if !found {
				continue
			}
			x := mermaidStringWidth(before) + 2
			return mermaidBounds{left: max(x-2, 0), top: max(y-1, 0), right: x + mermaidStringWidth(text) + 1, bottom: min(y+1, len(lines)-1), ok: true}
		}
	}
	return mermaidBounds{}
}

func mergeMermaidBounds(a, b mermaidBounds) mermaidBounds {
	if !a.ok {
		return b
	}
	if !b.ok {
		return a
	}
	return mermaidBounds{
		left: min(a.left, b.left), top: min(a.top, b.top),
		right: max(a.right, b.right), bottom: max(a.bottom, b.bottom), ok: true,
	}
}

func drawMermaidSubgraphBox(canvas [][]string, bounds mermaidBounds, label string) {
	for x := bounds.left + 1; x < bounds.right; x++ {
		if canvas[bounds.top][x] == " " {
			canvas[bounds.top][x] = "─"
		}
		if canvas[bounds.bottom][x] == " " {
			canvas[bounds.bottom][x] = "─"
		}
	}
	for y := bounds.top + 1; y < bounds.bottom; y++ {
		if canvas[y][bounds.left] == " " {
			canvas[y][bounds.left] = "│"
		}
		if canvas[y][bounds.right] == " " {
			canvas[y][bounds.right] = "│"
		}
	}
	canvas[bounds.top][bounds.left], canvas[bounds.top][bounds.right] = "╭", "╮"
	canvas[bounds.bottom][bounds.left], canvas[bounds.bottom][bounds.right] = "╰", "╯"
	label = " " + truncateMermaid(label, max(bounds.right-bounds.left-3, 1)) + " "
	writeMermaidCanvas(canvas[bounds.top], bounds.left+2, label)
}
