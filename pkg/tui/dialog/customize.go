package dialog

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/docker-agent/pkg/tui/components/toolcommon"
	"github.com/docker/docker-agent/pkg/tui/core"
	"github.com/docker/docker-agent/pkg/tui/core/layout"
	"github.com/docker/docker-agent/pkg/tui/messages"
	"github.com/docker/docker-agent/pkg/tui/styles"
)

const (
	customizeWidthPercent = 50
	customizeMinWidth     = 52
	customizeMaxWidth     = 60

	// previewMaxWidth is the widest the layout preview schematic can get.
	previewMaxWidth = 44
	// previewMinWidth keeps the schematic legible on tiny terminals.
	previewMinWidth = 24
)

// customizeRows enumerates the selectable rows of the customize dialog.
const (
	rowPosition = iota
	rowSpacing
	rowUsage
	rowAgents
	rowTools
	rowTodos
	rowCount
)

// sidebarPositions is the ←/→ cycle order of the position selector.
var sidebarPositions = []messages.SidebarPosition{
	messages.SidebarRight,
	messages.SidebarLeft,
	messages.SidebarTop,
	messages.SidebarBottom,
}

// positionLabels maps positions to their display labels.
var positionLabels = map[messages.SidebarPosition]string{
	messages.SidebarRight:  "Right",
	messages.SidebarLeft:   "Left",
	messages.SidebarTop:    "Top",
	messages.SidebarBottom: "Bottom",
}

// sectionSpacings is the ←/→ cycle order of the section-spacing selector.
var sectionSpacings = []messages.SectionSpacing{
	messages.SpacingCompact,
	messages.SpacingNormal,
	messages.SpacingRelaxed,
}

// spacingLabels maps spacings to their display labels.
var spacingLabels = map[messages.SectionSpacing]string{
	messages.SpacingCompact: "Compact",
	messages.SpacingNormal:  "Normal",
	messages.SpacingRelaxed: "Relaxed",
}

// customizeDialog lets the user customize the TUI layout: sidebar position,
// spacing between sidebar sections, and which sections are visible. Changes
// are previewed live (both in the schematic and in the UI behind the dialog);
// Enter persists them, Esc restores the original layout.
type customizeDialog struct {
	BaseDialog

	original messages.LayoutSettings
	current  messages.LayoutSettings
	selected int
}

// NewCustomizeDialog creates the layout customization dialog seeded with the
// currently active settings.
func NewCustomizeDialog(current messages.LayoutSettings) Dialog {
	current.SidebarPosition = messages.ParseSidebarPosition(string(current.SidebarPosition))
	current.SectionSpacing = messages.ParseSectionSpacing(string(current.SectionSpacing))
	return &customizeDialog{
		original: current,
		current:  current,
	}
}

func (d *customizeDialog) Init() tea.Cmd { return nil }

func (d *customizeDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := d.SetSize(msg.Width, msg.Height)
		return d, cmd

	case tea.KeyPressMsg:
		if cmd := HandleQuit(msg); cmd != nil {
			return d, cmd
		}
		cmd := d.handleKey(msg)
		return d, cmd
	}
	return d, nil
}

func (d *customizeDialog) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		return d.cancel()
	case "up", "k", "ctrl+k":
		if d.selected > 0 {
			d.selected--
		}
	case "down", "j", "ctrl+j":
		if d.selected < rowCount-1 {
			d.selected++
		}
	case "shift+tab":
		d.selected = (d.selected - 1 + rowCount) % rowCount
	case "tab":
		d.selected = (d.selected + 1) % rowCount
	case "home", "g":
		d.selected = 0
	case "end", "G":
		d.selected = rowCount - 1
	case "left", "h":
		return d.changeValue(-1)
	case "right", "l", "space":
		return d.changeValue(+1)
	case "enter":
		return d.apply()
	}
	return nil
}

// changeValue cycles the selected row's value by delta and emits a live preview.
func (d *customizeDialog) changeValue(delta int) tea.Cmd {
	switch d.selected {
	case rowPosition:
		d.current.SidebarPosition = cycleValue(sidebarPositions, d.current.SidebarPosition, delta)
	case rowSpacing:
		d.current.SectionSpacing = cycleValue(sectionSpacings, d.current.SectionSpacing, delta)
	case rowUsage:
		d.current.HideUsage = !d.current.HideUsage
	case rowAgents:
		d.current.HideAgents = !d.current.HideAgents
	case rowTools:
		d.current.HideTools = !d.current.HideTools
	case rowTodos:
		d.current.HideTodos = !d.current.HideTodos
	default:
		return nil
	}
	return core.CmdHandler(messages.PreviewLayoutMsg{Layout: d.current})
}

// cycleValue returns the value delta steps away from current in the cycle order.
func cycleValue[T comparable](values []T, current T, delta int) T {
	idx := 0
	for i, v := range values {
		if v == current {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(values)) % len(values)
	return values[idx]
}

// apply closes the dialog and commits the current settings.
func (d *customizeDialog) apply() tea.Cmd {
	if d.current == d.original {
		return closeDialogCmd()
	}
	return tea.Sequence(
		closeDialogCmd(),
		core.CmdHandler(messages.ApplyLayoutMsg{Layout: d.current}),
	)
}

// cancel closes the dialog and restores the original settings.
func (d *customizeDialog) cancel() tea.Cmd {
	if d.current == d.original {
		return closeDialogCmd()
	}
	return tea.Sequence(
		closeDialogCmd(),
		core.CmdHandler(messages.CancelLayoutPreviewMsg{Original: d.original}),
	)
}

func (d *customizeDialog) Position() (row, col int) {
	return d.CenterDialog(d.View())
}

func (d *customizeDialog) View() string {
	width := d.ComputeDialogWidth(customizeWidthPercent, customizeMinWidth, customizeMaxWidth)
	inner := d.ContentWidth(width, 2)

	preview := lipgloss.NewStyle().
		Width(inner).
		Align(lipgloss.Center).
		Render(renderLayoutPreview(d.current, inner))

	content := NewContent(inner).
		AddTitle("Customize Layout").
		AddSeparator().
		AddSpace().
		AddContent(preview).
		AddSpace().
		AddContent(d.renderSelectorRow(rowPosition, "Sidebar position", positionLabels[d.current.SidebarPosition], inner)).
		AddContent(d.renderSelectorRow(rowSpacing, "Section spacing", spacingLabels[d.current.SectionSpacing], inner)).
		AddSpace().
		AddContent(styles.MutedStyle.Render("Sidebar sections")).
		AddContent(d.renderToggleRow(rowUsage, "Token usage", d.current.HideUsage)).
		AddContent(d.renderToggleRow(rowAgents, "Agents", d.current.HideAgents)).
		AddContent(d.renderToggleRow(rowTools, "Tools", d.current.HideTools)).
		AddContent(d.renderToggleRow(rowTodos, "Todos", d.current.HideTodos)).
		AddSpace().
		AddHelpKeys("↑/↓", "navigate", "←/→", "change", "enter", "apply", "esc", "cancel").
		Build()

	return styles.DialogStyle.Width(width).Render(content)
}

// renderSelectorRow renders a row with a ‹ value › selector aligned to the right.
func (d *customizeDialog) renderSelectorRow(row int, label, valueLabel string, width int) string {
	value := "‹ " + valueLabel + " ›"

	labelStyle := styles.PaletteUnselectedActionStyle
	valueStyle := styles.SecondaryStyle
	prefix := "  "
	if d.selected == row {
		labelStyle = styles.PaletteSelectedActionStyle
		valueStyle = styles.HighlightWhiteStyle
		prefix = styles.HighlightWhiteStyle.Render("› ")
	}

	left := prefix + labelStyle.Render(label)
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(value))
	return left + strings.Repeat(" ", gap) + valueStyle.Render(value)
}

// renderToggleRow renders a checkbox row for one sidebar section.
func (d *customizeDialog) renderToggleRow(row int, label string, hidden bool) string {
	check := "[x]"
	if hidden {
		check = "[ ]"
	}

	labelStyle := styles.PaletteUnselectedActionStyle
	checkStyle := styles.SecondaryStyle
	prefix := "  "
	if d.selected == row {
		labelStyle = styles.PaletteSelectedActionStyle
		checkStyle = styles.HighlightWhiteStyle
		prefix = styles.HighlightWhiteStyle.Render("› ")
	}
	if !hidden {
		checkStyle = checkStyle.Foreground(styles.Success)
	}

	return prefix + checkStyle.Render(check) + " " + labelStyle.Render(label)
}

// visibleSectionLabels returns the sidebar section labels that are visible
// under the given settings. The session block is always shown.
func visibleSectionLabels(s messages.LayoutSettings) []string {
	labels := []string{"session"}
	if !s.HideUsage {
		labels = append(labels, "usage")
	}
	if !s.HideAgents {
		labels = append(labels, "agents")
	}
	if !s.HideTools {
		labels = append(labels, "tools")
	}
	if !s.HideTodos {
		labels = append(labels, "todos")
	}
	return labels
}

// renderLayoutPreview draws a small schematic of the resulting layout: the
// chat box, the input box, and the sidebar (side column or horizontal band)
// listing only the visible sections. maxWidth caps the schematic width.
func renderLayoutPreview(s messages.LayoutSettings, maxWidth int) string {
	width := max(previewMinWidth, min(previewMaxWidth, maxWidth))
	switch messages.ParseSidebarPosition(string(s.SidebarPosition)) {
	case messages.SidebarLeft:
		return renderSidePreview(s, width, true)
	case messages.SidebarTop:
		return renderBandPreview(s, width, false)
	case messages.SidebarBottom:
		return renderBandPreview(s, width, true)
	default:
		return renderSidePreview(s, width, false)
	}
}

// borderStyle renders preview borders.
var previewBorder = func(text string) string { return styles.FadingStyle.Render(text) }

// previewLabelCell renders a label padded to width inside a preview box cell.
func previewLabelCell(label string, width int, style lipgloss.Style) string {
	label = toolcommon.TruncateText(label, max(0, width-1))
	cell := " " + style.Render(label)
	return cell + strings.Repeat(" ", max(0, width-lipgloss.Width(cell)))
}

// previewCenteredCell renders a label centered within width.
func previewCenteredCell(label string, width int, style lipgloss.Style) string {
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, style.Render(label))
}

// renderSidePreview draws the vertical layout with the sidebar on the left or right.
func renderSidePreview(s messages.LayoutSettings, width int, onLeft bool) string {
	inner := width - 2
	sideW := max(9, inner/3)
	chatW := inner - sideW - 1
	const contentRows = 5

	labels := visibleSectionLabels(s)
	sectionStyle := styles.TabAccentStyle

	var lines []string
	hbarChat := strings.Repeat("─", chatW)
	hbarSide := strings.Repeat("─", sideW)
	hbarFull := strings.Repeat("─", inner)

	if onLeft {
		lines = append(lines, previewBorder("╭"+hbarSide+"┬"+hbarChat+"╮"))
	} else {
		lines = append(lines, previewBorder("╭"+hbarChat+"┬"+hbarSide+"╮"))
	}

	for i := range contentRows {
		var side, chat string
		if i < len(labels) {
			side = previewLabelCell(labels[i], sideW, sectionStyle)
		} else {
			side = strings.Repeat(" ", sideW)
		}
		if i == contentRows/2 {
			chat = previewCenteredCell("chat", chatW, styles.SecondaryStyle)
		} else {
			chat = strings.Repeat(" ", chatW)
		}

		v := previewBorder("│")
		if onLeft {
			lines = append(lines, v+side+v+chat+v)
		} else {
			lines = append(lines, v+chat+v+side+v)
		}
	}

	if onLeft {
		lines = append(lines, previewBorder("├"+hbarSide+"┴"+hbarChat+"┤"))
	} else {
		lines = append(lines, previewBorder("├"+hbarChat+"┴"+hbarSide+"┤"))
	}
	lines = append(lines,
		previewBorder("│")+previewLabelCell("input", inner, styles.SecondaryStyle)+previewBorder("│"),
		previewBorder("╰"+hbarFull+"╯"),
	)

	return strings.Join(lines, "\n")
}

// renderBandPreview draws the layout with the sidebar as a horizontal band
// above (bottom=false) or below (bottom=true) the chat.
func renderBandPreview(s messages.LayoutSettings, width int, bottom bool) string {
	inner := width - 2
	const chatRows = 3

	band := previewLabelCell(strings.Join(visibleSectionLabels(s), " · "), inner, styles.TabAccentStyle)
	hbarFull := strings.Repeat("─", inner)
	v := previewBorder("│")

	chatLines := make([]string, 0, chatRows)
	for i := range chatRows {
		if i == chatRows/2 {
			chatLines = append(chatLines, v+previewCenteredCell("chat", inner, styles.SecondaryStyle)+v)
		} else {
			chatLines = append(chatLines, v+strings.Repeat(" ", inner)+v)
		}
	}

	lines := []string{previewBorder("╭" + hbarFull + "╮")}
	if bottom {
		lines = append(lines, chatLines...)
		lines = append(lines, previewBorder("├"+hbarFull+"┤"), v+band+v)
	} else {
		lines = append(lines, v+band+v, previewBorder("├"+hbarFull+"┤"))
		lines = append(lines, chatLines...)
	}
	lines = append(lines,
		previewBorder("├"+hbarFull+"┤"),
		v+previewLabelCell("input", inner, styles.SecondaryStyle)+v,
		previewBorder("╰"+hbarFull+"╯"),
	)

	return strings.Join(lines, "\n")
}
