package ui

import (
	"fmt"
	"strconv"
	"strings"

	pathx "github.com/docker/docker-agent/pkg/path"
	"github.com/docker/docker-agent/pkg/tui/components/toolcommon"
)

// StatusModel is the snapshot of run state shown in the footer.
type StatusModel struct {
	WorkingDir string
	Branch     string

	Agent    string
	Model    string
	Thinking string

	ContextLength int64
	ContextLimit  int64
	Tokens        int64 // input + output tokens used so far
	Cost          float64
	CostKnown     bool
}

// RenderStatus builds the two-line footer:
//
//	<working dir>  ⎇ <branch>                          <agent>
//	<context bar> <pct> · <tokens> · <cost>  <model> · <effort>
func RenderStatus(d StatusModel, width int) []string {
	dir := StSecondary().Render(Truncate(pathx.ShortenHome(d.WorkingDir), max(10, width/2)))
	left1 := dir
	if d.Branch != "" {
		left1 += StMuted().Render("  ⎇ " + d.Branch)
	}

	right1 := ""
	if d.Agent != "" {
		right1 = StAccent().Render(d.Agent)
	}

	left2 := RenderContext(d)

	var rightParts []string
	if d.Model != "" {
		rightParts = append(rightParts, d.Model)
	}
	if d.Thinking != "" {
		rightParts = append(rightParts, d.Thinking)
	}
	right2 := StMuted().Render(strings.Join(rightParts, " · "))

	return []string{
		ComposeLine(left1, right1, width),
		ComposeLine(left2, right2, width),
	}
}

// RenderContext renders the context and cost portion of the status.
func RenderContext(d StatusModel) string {
	Cost := renderCostSuffix(d)
	if d.ContextLimit <= 0 {
		if d.Tokens > 0 {
			return StMuted().Render(FormatTokens(d.Tokens)+" tokens") + Cost
		}
		return RenderBar(0) + StMuted().Render(" 0% · 0/0") + Cost
	}

	pct := float64(d.ContextLength) / float64(d.ContextLimit)
	if pct > 1 {
		pct = 1
	}
	bar := RenderBar(pct)
	label := fmt.Sprintf(" %d%% · %s/%s",
		int(pct*100+0.5),
		FormatTokens(d.ContextLength),
		FormatTokens(d.ContextLimit),
	)
	return bar + StMuted().Render(label) + Cost
}

func renderCostSuffix(d StatusModel) string {
	if !d.CostKnown {
		return ""
	}
	return StMuted().Render(" · ") + StAccent().Render(toolcommon.FormatCostUSD(d.Cost))
}

// ContextBarWidth is the cell width of the context-usage gauge.
const ContextBarWidth = 10

// RenderBar renders the context usage gauge.
func RenderBar(pct float64) string {
	filled := min(int(pct*float64(ContextBarWidth)+0.5), ContextBarWidth)
	style := StSuccess()
	switch {
	case pct >= 0.85:
		style = StError()
	case pct >= 0.6:
		style = StWarning()
	}
	return style.Render(strings.Repeat("█", filled)) + StMuted().Render(strings.Repeat("░", ContextBarWidth-filled))
}

// ComposeLine right-aligns right within width, truncating left if necessary.
func ComposeLine(left, right string, width int) string {
	lw := DisplayWidth(left)
	rw := DisplayWidth(right)
	if rw > width {
		return Truncate(right, width)
	}
	if lw+rw+1 > width {
		left = Truncate(left, max(0, width-rw-1))
		lw = DisplayWidth(left)
	}
	gap := max(1, width-lw-rw)
	return left + strings.Repeat(" ", gap) + right
}

// FormatTokens formats a token count for compact status display.
func FormatTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return strconv.FormatInt(n, 10)
	}
}
