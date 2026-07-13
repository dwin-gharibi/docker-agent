package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tui/messages"
)

func newTestSettingsDialog(t *testing.T, layout messages.LayoutSettings) *settingsDialog {
	t.Helper()
	d, ok := NewSettingsDialog(layout, messages.SendModeSteer, true).(*settingsDialog)
	require.True(t, ok)
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	return d
}

func TestSettingsDialogNormalizesValues(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	assert.Equal(t, messages.SidebarRight, d.current.SidebarPosition)

	raw, ok := NewSettingsDialog(messages.LayoutSettings{}, messages.SendMode("bogus"), true).(*settingsDialog)
	require.True(t, ok)
	assert.Equal(t, messages.SendModeSteer, raw.currentMode, "unknown send mode normalizes to steer")
}

func TestSettingsDialogNavigation(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	down := tea.KeyPressMsg{Code: tea.KeyDown}
	up := tea.KeyPressMsg{Code: tea.KeyUp}

	require.Equal(t, rowPosition, d.selected[tabVisuals])

	d.Update(down)
	require.Equal(t, rowSpacing, d.selected[tabVisuals])

	d.Update(down)
	require.Equal(t, rowSessionPath, d.selected[tabVisuals])

	d.Update(down)
	require.Equal(t, rowUsage, d.selected[tabVisuals])

	for range 10 {
		d.Update(down)
	}
	require.Equal(t, rowTodos, d.selected[tabVisuals], "down must stop at the last row")

	d.Update(up)
	require.Equal(t, rowTools, d.selected[tabVisuals])
}

func TestSettingsDialogTabSwitching(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	require.Equal(t, tabVisuals, d.tab)

	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	assert.Equal(t, tabBehavior, d.tab)

	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	assert.Equal(t, tabVisuals, d.tab, "tab wraps around")

	d.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	assert.Equal(t, tabBehavior, d.tab, "shift+tab cycles backwards")
}

func TestSettingsDialogWithoutVisualsTab(t *testing.T) {
	t.Parallel()

	d, ok := NewSettingsDialog(messages.LayoutSettings{}, messages.SendModeSteer, false).(*settingsDialog)
	require.True(t, ok)
	d.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	require.Equal(t, tabBehavior, d.tab, "without visuals the dialog opens on the behavior tab")

	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	assert.Equal(t, tabBehavior, d.tab, "tab must not reach the hidden visuals tab")

	view := ansi.Strip(d.View())
	assert.NotContains(t, view, "Visuals")
	assert.NotContains(t, view, "Sidebar position")
	assert.Contains(t, view, "While agent is working")
}

func TestSettingsDialogCyclesPositionAndPreviews(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	require.NotNil(t, cmd)
	msgs := collectMsgs(cmd)
	require.Len(t, msgs, 1)
	preview, ok := msgs[0].(messages.PreviewLayoutMsg)
	require.True(t, ok, "changing a value must emit a live preview")
	assert.Equal(t, messages.SidebarLeft, preview.Layout.SidebarPosition)

	d.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	assert.Equal(t, messages.SidebarTop, d.current.SidebarPosition)

	// Cycling backwards from the start wraps around.
	d.current.SidebarPosition = messages.SidebarRight
	d.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	assert.Equal(t, messages.SidebarBottom, d.current.SidebarPosition)
}

func TestSettingsDialogCyclesSpacingAndPreviews(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	require.Equal(t, messages.SpacingNormal, d.current.SectionSpacing,
		"empty spacing normalizes to normal")
	d.selected[tabVisuals] = rowSpacing

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	require.NotNil(t, cmd)
	msgs := collectMsgs(cmd)
	require.Len(t, msgs, 1)
	preview, ok := msgs[0].(messages.PreviewLayoutMsg)
	require.True(t, ok, "changing the spacing must emit a live preview")
	assert.Equal(t, messages.SpacingRelaxed, preview.Layout.SectionSpacing)

	d.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	assert.Equal(t, messages.SpacingCompact, d.current.SectionSpacing, "cycling wraps around")

	d.current.SectionSpacing = messages.SpacingNormal
	d.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	assert.Equal(t, messages.SpacingCompact, d.current.SectionSpacing)
}

func TestSettingsDialogTogglesSection(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	d.selected[tabVisuals] = rowUsage

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	msgs := collectMsgs(cmd)
	require.Len(t, msgs, 1)
	preview, ok := msgs[0].(messages.PreviewLayoutMsg)
	require.True(t, ok)
	assert.True(t, preview.Layout.HideUsage, "space must hide the usage section")

	d.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	assert.False(t, d.current.HideUsage, "space must toggle back")
}

func TestSettingsDialogTogglesSessionPath(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	d.selected[tabVisuals] = rowSessionPath

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	msgs := collectMsgs(cmd)
	require.Len(t, msgs, 1)
	preview, ok := msgs[0].(messages.PreviewLayoutMsg)
	require.True(t, ok, "toggling the session path must emit a live preview")
	assert.True(t, preview.Layout.HideSessionPath, "space must hide the session path")

	d.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	assert.False(t, d.current.HideSessionPath, "space must toggle back")
}

func TestSettingsDialogTogglesSendModeWithoutPreview(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	require.Equal(t, tabBehavior, d.tab)
	require.Equal(t, messages.SendModeSteer, d.currentMode)

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	assert.Nil(t, cmd, "the send mode has no live preview")
	assert.Equal(t, messages.SendModeQueue, d.currentMode)

	d.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	assert.Equal(t, messages.SendModeSteer, d.currentMode, "cycling wraps around")
}

func TestSettingsDialogApplyEmitsApplySettings(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	d.selected[tabVisuals] = rowTools
	d.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	d.Update(tea.KeyPressMsg{Code: tea.KeyRight})

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msgs := collectMsgs(cmd)
	require.Len(t, msgs, 2, "apply must close the dialog and emit the settings")
	_, ok := msgs[0].(CloseDialogMsg)
	require.True(t, ok)
	applied, ok := msgs[1].(messages.ApplySettingsMsg)
	require.True(t, ok)
	assert.True(t, applied.Layout.HideTools)
	assert.Equal(t, messages.SendModeQueue, applied.SendMode)
}

func TestSettingsDialogApplySendModeChangeOnly(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	d.Update(tea.KeyPressMsg{Code: tea.KeyRight})

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msgs := collectMsgs(cmd)
	require.Len(t, msgs, 2)
	applied, ok := msgs[1].(messages.ApplySettingsMsg)
	require.True(t, ok, "a send-mode-only change must still be applied")
	assert.Equal(t, messages.SendModeQueue, applied.SendMode)
}

func TestSettingsDialogApplyWithoutChangesOnlyCloses(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msgs := collectMsgs(cmd)
	require.Len(t, msgs, 1)
	_, ok := msgs[0].(CloseDialogMsg)
	assert.True(t, ok, "no changes: apply just closes")
}

func TestSettingsDialogEscapeRestoresOriginal(t *testing.T) {
	t.Parallel()

	original := messages.LayoutSettings{SidebarPosition: messages.SidebarLeft, SectionSpacing: messages.SpacingNormal}
	d := newTestSettingsDialog(t, original)
	d.Update(tea.KeyPressMsg{Code: tea.KeyRight})

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	msgs := collectMsgs(cmd)
	require.Len(t, msgs, 2)
	_, ok := msgs[0].(CloseDialogMsg)
	require.True(t, ok)
	cancel, ok := msgs[1].(messages.CancelLayoutPreviewMsg)
	require.True(t, ok, "esc after a change must restore the original layout")
	assert.Equal(t, original, cancel.Original)
}

func TestSettingsDialogEscapeWithSendModeChangeOnlyCloses(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	d.Update(tea.KeyPressMsg{Code: tea.KeyRight})

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	msgs := collectMsgs(cmd)
	require.Len(t, msgs, 1, "the send mode never previews, so there is nothing to roll back")
	_, ok := msgs[0].(CloseDialogMsg)
	assert.True(t, ok)
}

func TestSettingsDialogViewShowsVisualsRows(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	view := ansi.Strip(d.View())

	assert.Contains(t, view, "Settings")
	assert.Contains(t, view, "Visuals")
	assert.Contains(t, view, "Behavior")
	assert.Contains(t, view, "Sidebar position")
	assert.Contains(t, view, "Right")
	assert.Contains(t, view, "Section spacing")
	assert.Contains(t, view, "Normal")
	assert.Contains(t, view, "Session path")
	assert.Contains(t, view, "Token usage")
	assert.Contains(t, view, "Agents")
	assert.Contains(t, view, "Tools")
	assert.Contains(t, view, "Todos")
}

func TestSettingsDialogViewShowsBehaviorRows(t *testing.T) {
	t.Parallel()

	d := newTestSettingsDialog(t, messages.LayoutSettings{})
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	view := ansi.Strip(d.View())

	assert.Contains(t, view, "While agent is working")
	assert.Contains(t, view, "● Steer", "steer starts selected")
	assert.Contains(t, view, "○ Queue")
	assert.Contains(t, view, "mid-turn")
	assert.Contains(t, view, "hold until the current turn ends")
	assert.NotContains(t, view, "Sidebar position", "behavior tab must not render visuals rows")

	d.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	view = ansi.Strip(d.View())
	assert.Contains(t, view, "● Queue", "the mark moves to the chosen mode")
	assert.Contains(t, view, "○ Steer")
}

func TestRenderLayoutPreviewReflectsSections(t *testing.T) {
	t.Parallel()

	full := ansi.Strip(renderLayoutPreview(messages.LayoutSettings{}, previewMaxWidth))
	assert.Contains(t, full, "chat")
	assert.Contains(t, full, "input")
	assert.Contains(t, full, "session/path", "a visible session path shows in the session label")
	assert.Contains(t, full, "usage")
	assert.Contains(t, full, "todos")

	trimmed := ansi.Strip(renderLayoutPreview(messages.LayoutSettings{
		HideSessionPath: true,
		HideUsage:       true,
		HideTodos:       true,
	}, previewMaxWidth))
	assert.NotContains(t, trimmed, "session/path", "a hidden session path shortens the session label")
	assert.Contains(t, trimmed, "session")
	assert.NotContains(t, trimmed, "usage")
	assert.NotContains(t, trimmed, "todos")
	assert.Contains(t, trimmed, "agents")
}

func TestRenderLayoutPreviewPositions(t *testing.T) {
	t.Parallel()

	for _, position := range sidebarPositions {
		preview := ansi.Strip(renderLayoutPreview(messages.LayoutSettings{SidebarPosition: position}, previewMaxWidth))
		assert.Contains(t, preview, "chat", "position %s", position)
		assert.Contains(t, preview, "session", "position %s", position)
	}

	// Band layouts list the sections on a single line; the full label is
	// wider than the band and gets truncated at its tail.
	band := ansi.Strip(renderLayoutPreview(messages.LayoutSettings{SidebarPosition: messages.SidebarTop}, previewMaxWidth))
	assert.Contains(t, band, "session/path · usage · agents · tools")

	hidden := ansi.Strip(renderLayoutPreview(messages.LayoutSettings{
		SidebarPosition: messages.SidebarTop,
		HideSessionPath: true,
	}, previewMaxWidth))
	assert.Contains(t, hidden, "session · usage · agents · tools · todos")

	// Narrow widths truncate the band list instead of overflowing.
	narrow := ansi.Strip(renderLayoutPreview(messages.LayoutSettings{SidebarPosition: messages.SidebarTop}, previewMinWidth))
	assert.Contains(t, narrow, "session")
}
