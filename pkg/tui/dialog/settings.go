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
	settingsWidthPercent = 50
	settingsMinWidth     = 52
	settingsMaxWidth     = 60

	// previewMaxWidth is the widest the layout preview schematic can get.
	previewMaxWidth = 44
	// previewMinWidth keeps the schematic legible on tiny terminals.
	previewMinWidth = 24
)

// settingsTabs enumerates the tabs of the settings dialog.
const (
	tabVisuals = iota
	tabBehavior
	tabCount
)

// settingsTabLabels maps tabs to their display labels.
var settingsTabLabels = [tabCount]string{"Visuals", "Behavior"}

// visualsRows enumerates the selectable rows of the Visuals tab.
const (
	rowPosition = iota
	rowSpacing
	rowSessionPath
	rowUsage
	rowAgents
	rowTools
	rowTodos
	visualsRowCount
)

// behaviorRows enumerates the selectable rows of the Behavior tab.
const (
	rowSendMode = iota
	behaviorRowCount
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

// sendModes is the ←/→ cycle order of the send-mode switch.
var sendModes = []messages.SendMode{
	messages.SendModeSteer,
	messages.SendModeQueue,
}

// sendModeOption describes one radio line of the send-mode switch.
type sendModeOption struct {
	mode  messages.SendMode
	label string
	desc  string
}

// sendModeOptions lists the send-mode choices with the short description
// rendered next to each label.
var sendModeOptions = []sendModeOption{
	{messages.SendModeSteer, "Steer", "send to the working agent mid-turn"},
	{messages.SendModeQueue, "Queue", "hold until the current turn ends"},
}

// settingsDialog lets the user tune the TUI from two tabs: Visuals (sidebar
// position, section spacing, visible sections — previewed live both in the
// schematic and in the UI behind the dialog) and Behavior (what happens to
// messages sent while the agent is working). Enter persists everything, Esc
// restores the original layout.
type settingsDialog struct {
	BaseDialog

	original messages.LayoutSettings
	current  messages.LayoutSettings

	originalMode messages.SendMode
	currentMode  messages.SendMode

	// showVisuals gates the Visuals tab; it is false when the sidebar is
	// disabled (lean mode or --sidebar=false) and there is no layout to tune.
	showVisuals bool

	tab int
	// selected tracks the highlighted row per tab so switching tabs
	// preserves each tab's position.
	selected [tabCount]int
}

// NewSettingsDialog creates the settings dialog seeded with the currently
// active values. showVisuals hides the Visuals tab when there is no sidebar
// to customize.
func NewSettingsDialog(current messages.LayoutSettings, mode messages.SendMode, showVisuals bool) Dialog {
	current.SidebarPosition = messages.ParseSidebarPosition(string(current.SidebarPosition))
	current.SectionSpacing = messages.ParseSectionSpacing(string(current.SectionSpacing))
	mode = messages.ParseSendMode(string(mode))
	d := &settingsDialog{
		original:     current,
		current:      current,
		originalMode: mode,
		currentMode:  mode,
		showVisuals:  showVisuals,
	}
	if !showVisuals {
		d.tab = tabBehavior
	}
	return d
}

func (d *settingsDialog) Init() tea.Cmd { return nil }

func (d *settingsDialog) Update(msg tea.Msg) (layout.Model, tea.Cmd) {
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

// rowCount returns the number of selectable rows on the active tab.
func (d *settingsDialog) rowCount() int {
	if d.tab == tabBehavior {
		return behaviorRowCount
	}
	return visualsRowCount
}

func (d *settingsDialog) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		return d.cancel()
	case "tab":
		d.switchTab(+1)
	case "shift+tab":
		d.switchTab(-1)
	case "up", "k", "ctrl+k":
		if d.selected[d.tab] > 0 {
			d.selected[d.tab]--
		}
	case "down", "j", "ctrl+j":
		if d.selected[d.tab] < d.rowCount()-1 {
			d.selected[d.tab]++
		}
	case "home", "g":
		d.selected[d.tab] = 0
	case "end", "G":
		d.selected[d.tab] = d.rowCount() - 1
	case "left", "h":
		return d.changeValue(-1)
	case "right", "l", "space":
		return d.changeValue(+1)
	case "enter":
		return d.apply()
	}
	return nil
}

// switchTab cycles the active tab by delta; a no-op without the Visuals tab.
func (d *settingsDialog) switchTab(delta int) {
	if !d.showVisuals {
		return
	}
	d.tab = (d.tab + delta + tabCount) % tabCount
}

// changeValue cycles the selected row's value by delta. Visuals changes emit
// a live preview; the send mode only takes effect when applied.
func (d *settingsDialog) changeValue(delta int) tea.Cmd {
	if d.tab == tabBehavior {
		if d.selected[tabBehavior] == rowSendMode {
			d.currentMode = cycleValue(sendModes, d.currentMode, delta)
		}
		return nil
	}

	switch d.selected[tabVisuals] {
	case rowPosition:
		d.current.SidebarPosition = cycleValue(sidebarPositions, d.current.SidebarPosition, delta)
	case rowSpacing:
		d.current.SectionSpacing = cycleValue(sectionSpacings, d.current.SectionSpacing, delta)
	case rowSessionPath:
		d.current.HideSessionPath = !d.current.HideSessionPath
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
func (d *settingsDialog) apply() tea.Cmd {
	if d.current == d.original && d.currentMode == d.originalMode {
		return closeDialogCmd()
	}
	return tea.Sequence(
		closeDialogCmd(),
		core.CmdHandler(messages.ApplySettingsMsg{Layout: d.current, SendMode: d.currentMode}),
	)
}

// cancel closes the dialog and restores the original layout. The send mode
// never previews, so only layout changes need to be rolled back.
func (d *settingsDialog) cancel() tea.Cmd {
	if d.current == d.original {
		return closeDialogCmd()
	}
	return tea.Sequence(
		closeDialogCmd(),
		core.CmdHandler(messages.CancelLayoutPreviewMsg{Original: d.original}),
	)
}

func (d *settingsDialog) Position() (row, col int) {
	return d.CenterDialog(d.View())
}

func (d *settingsDialog) View() string {
	width := d.ComputeDialogWidth(settingsWidthPercent, settingsMinWidth, settingsMaxWidth)
	inner := d.ContentWidth(width, 2)

	content := NewContent(inner).
		AddTitle("Settings").
		AddSeparator()

	helpKeys := []string{"↑/↓", "navigate", "←/→", "change"}
	if d.showVisuals {
		content.AddSpace().AddContent(d.renderTabBar(inner))
		helpKeys = append(helpKeys, "tab", "switch tab")
	}

	if d.tab == tabBehavior {
		d.renderBehaviorTab(content, inner)
	} else {
		d.renderVisualsTab(content, inner)
	}

	content.
		AddSpace().
		AddHelpKeys(append(helpKeys, "enter", "apply", "esc", "cancel")...)

	return styles.DialogStyle.Width(width).Render(content.Build())
}

// renderTabBar renders the Visuals/Behavior tab labels, highlighting the
// active one.
func (d *settingsDialog) renderTabBar(width int) string {
	tabs := make([]string, 0, tabCount)
	for i, label := range settingsTabLabels {
		style := styles.MutedStyle
		if i == d.tab {
			style = styles.HighlightWhiteStyle.Underline(true)
		}
		tabs = append(tabs, style.Render(label))
	}
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, strings.Join(tabs, "    "))
}

// renderVisualsTab renders the layout preview, the position/spacing
// selectors, and the section visibility toggles.
func (d *settingsDialog) renderVisualsTab(content *Content, inner int) {
	preview := lipgloss.NewStyle().
		Width(inner).
		Align(lipgloss.Center).
		Render(renderLayoutPreview(d.current, inner))

	content.
		AddSpace().
		AddContent(preview).
		AddSpace().
		AddContent(d.renderSelectorRow(rowPosition, "Sidebar position", positionLabels[d.current.SidebarPosition], inner)).
		AddContent(d.renderSelectorRow(rowSpacing, "Section spacing", spacingLabels[d.current.SectionSpacing], inner)).
		AddSpace().
		AddContent(styles.MutedStyle.Render("Sidebar sections")).
		AddContent(d.renderToggleRow(rowSessionPath, "Session path", d.current.HideSessionPath)).
		AddContent(d.renderToggleRow(rowUsage, "Token usage", d.current.HideUsage)).
		AddContent(d.renderToggleRow(rowAgents, "Agents", d.current.HideAgents)).
		AddContent(d.renderToggleRow(rowTools, "Tools", d.current.HideTools)).
		AddContent(d.renderToggleRow(rowTodos, "Todos", d.current.HideTodos))
}

// renderBehaviorTab renders the send-mode switch as a radio group: both
// choices stay visible, ←/→ or space moves the mark between them.
func (d *settingsDialog) renderBehaviorTab(content *Content, _ int) {
	content.
		AddSpace().
		AddContent(styles.MutedStyle.Render("While agent is working")).
		AddSpace()
	for _, opt := range sendModeOptions {
		content.AddContent(d.renderSendModeOption(opt))
	}
}

// renderSendModeOption renders one radio line of the send-mode switch.
func (d *settingsDialog) renderSendModeOption(opt sendModeOption) string {
	glyph := "○"
	labelStyle := styles.PaletteUnselectedActionStyle
	glyphStyle := styles.SecondaryStyle
	prefix := "  "
	if d.currentMode == opt.mode {
		glyph = "●"
		labelStyle = styles.PaletteSelectedActionStyle
		glyphStyle = styles.SecondaryStyle.Foreground(styles.Success)
		prefix = styles.HighlightWhiteStyle.Render("› ")
	}
	return prefix + glyphStyle.Render(glyph) + " " + labelStyle.Render(opt.label) + "   " + styles.MutedStyle.Render(opt.desc)
}

// renderSelectorRow renders a row with a ‹ value › selector aligned to the right.
func (d *settingsDialog) renderSelectorRow(row int, label, valueLabel string, width int) string {
	value := "‹ " + valueLabel + " ›"

	labelStyle := styles.PaletteUnselectedActionStyle
	valueStyle := styles.SecondaryStyle
	prefix := "  "
	if d.selected[d.tab] == row {
		labelStyle = styles.PaletteSelectedActionStyle
		valueStyle = styles.HighlightWhiteStyle
		prefix = styles.HighlightWhiteStyle.Render("› ")
	}

	left := prefix + labelStyle.Render(label)
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(value))
	return left + strings.Repeat(" ", gap) + valueStyle.Render(value)
}

// renderToggleRow renders a checkbox row for one sidebar section.
func (d *settingsDialog) renderToggleRow(row int, label string, hidden bool) string {
	check := "[x]"
	if hidden {
		check = "[ ]"
	}

	labelStyle := styles.PaletteUnselectedActionStyle
	checkStyle := styles.SecondaryStyle
	prefix := "  "
	if d.selected[d.tab] == row {
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
// under the given settings. The session block is always shown; its label
// reads "session/path" while the session path is visible and "session" once
// it is hidden.
func visibleSectionLabels(s messages.LayoutSettings) []string {
	sessionLabel := "session/path"
	if s.HideSessionPath {
		sessionLabel = "session"
	}
	labels := []string{sessionLabel}
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
