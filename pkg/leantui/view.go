package leantui

import "github.com/docker/docker-agent/pkg/leantui/ui"

// buildLines produces the entire frame: the conversation, the slash-command
// popup, the input box (or a confirmation prompt) and the status footer. It
// returns the lines plus the hardware cursor position (a line index and column).
func (m *model) buildLines() (lines []string, cursorLine, cursorCol int) {
	width := m.width

	lines = m.transcript.lines(width, m.spinnerFrame, m.busy, m.sessionState, m.pendingUsers)

	lines = append(lines, m.ac.Render(width)...)

	inputStart := len(lines)
	if m.confirm != nil {
		confirmLines := m.confirm.render(width)
		lines = append(lines, confirmLines...)
		cursorLine = inputStart + max(len(confirmLines)-1, 0)
		if len(confirmLines) > 0 {
			cursorCol = min(ui.DisplayWidth(confirmLines[len(confirmLines)-1]), max(width-1, 0))
		}
	} else {
		editorLines, row, col := m.editor.Layout(width)
		lines = append(lines, editorLines...)
		cursorLine = inputStart + row
		cursorCol = col
	}

	lines = append(lines, "")
	lines = append(lines, ui.RenderStatus(m.status, width)...)

	return lines, cursorLine, cursorCol
}

// confirmState holds a pending tool-approval prompt.
type confirmState struct {
	tool     string // raw tool name, used to scope "always allow"
	toolView ui.ToolView
}

func (c *confirmState) render(width int) []string {
	lines := []string{ui.Truncate(ui.StWarning().Render("● Approve tool call"), width)}
	lines = append(lines, ui.RenderTool(c.toolView, width)...)
	lines = append(lines, ui.Truncate(ui.StMuted().Render("[y] yes   [a] always this tool   [s] whole session   [n] no"), width))
	return lines
}
