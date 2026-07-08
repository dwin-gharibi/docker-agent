package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tui/messages"
)

func newTestCustomizeDialog(t *testing.T, current messages.LayoutSettings) *customizeDialog {
	t.Helper()
	d, ok := NewCustomizeDialog(current).(*customizeDialog)
	require.True(t, ok)
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	return d
}

func TestCustomizeDialogNormalizesPosition(t *testing.T) {
	t.Parallel()

	d := newTestCustomizeDialog(t, messages.LayoutSettings{})
	assert.Equal(t, messages.SidebarRight, d.current.SidebarPosition)
}

func TestCustomizeDialogNavigation(t *testing.T) {
	t.Parallel()

	d := newTestCustomizeDialog(t, messages.LayoutSettings{})
	down := tea.KeyPressMsg{Code: tea.KeyDown}
	up := tea.KeyPressMsg{Code: tea.KeyUp}

	require.Equal(t, rowPosition, d.selected)

	d.Update(down)
	require.Equal(t, rowSpacing, d.selected)

	d.Update(down)
	require.Equal(t, rowUsage, d.selected)

	for range 10 {
		d.Update(down)
	}
	require.Equal(t, rowTodos, d.selected, "down must stop at the last row")

	d.Update(up)
	require.Equal(t, rowTools, d.selected)
}

func TestCustomizeDialogCyclesPositionAndPreviews(t *testing.T) {
	t.Parallel()

	d := newTestCustomizeDialog(t, messages.LayoutSettings{})

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

func TestCustomizeDialogCyclesSpacingAndPreviews(t *testing.T) {
	t.Parallel()

	d := newTestCustomizeDialog(t, messages.LayoutSettings{})
	require.Equal(t, messages.SpacingNormal, d.current.SectionSpacing,
		"empty spacing normalizes to normal")
	d.selected = rowSpacing

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

func TestCustomizeDialogTogglesSection(t *testing.T) {
	t.Parallel()

	d := newTestCustomizeDialog(t, messages.LayoutSettings{})
	d.selected = rowUsage

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	msgs := collectMsgs(cmd)
	require.Len(t, msgs, 1)
	preview, ok := msgs[0].(messages.PreviewLayoutMsg)
	require.True(t, ok)
	assert.True(t, preview.Layout.HideUsage, "space must hide the usage section")

	d.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	assert.False(t, d.current.HideUsage, "space must toggle back")
}

func TestCustomizeDialogApplyEmitsApplyLayout(t *testing.T) {
	t.Parallel()

	d := newTestCustomizeDialog(t, messages.LayoutSettings{})
	d.selected = rowTools
	d.Update(tea.KeyPressMsg{Code: tea.KeySpace})

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msgs := collectMsgs(cmd)
	require.Len(t, msgs, 2, "apply must close the dialog and emit the layout")
	_, ok := msgs[0].(CloseDialogMsg)
	require.True(t, ok)
	applied, ok := msgs[1].(messages.ApplyLayoutMsg)
	require.True(t, ok)
	assert.True(t, applied.Layout.HideTools)
}

func TestCustomizeDialogApplyWithoutChangesOnlyCloses(t *testing.T) {
	t.Parallel()

	d := newTestCustomizeDialog(t, messages.LayoutSettings{})

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msgs := collectMsgs(cmd)
	require.Len(t, msgs, 1)
	_, ok := msgs[0].(CloseDialogMsg)
	assert.True(t, ok, "no changes: apply just closes")
}

func TestCustomizeDialogEscapeRestoresOriginal(t *testing.T) {
	t.Parallel()

	original := messages.LayoutSettings{SidebarPosition: messages.SidebarLeft, SectionSpacing: messages.SpacingNormal}
	d := newTestCustomizeDialog(t, original)
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

func TestCustomizeDialogEscapeWithoutChangesOnlyCloses(t *testing.T) {
	t.Parallel()

	d := newTestCustomizeDialog(t, messages.LayoutSettings{})

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	msgs := collectMsgs(cmd)
	require.Len(t, msgs, 1)
	_, ok := msgs[0].(CloseDialogMsg)
	assert.True(t, ok)
}

func TestCustomizeDialogViewShowsRows(t *testing.T) {
	t.Parallel()

	d := newTestCustomizeDialog(t, messages.LayoutSettings{})
	view := ansi.Strip(d.View())

	assert.Contains(t, view, "Customize Layout")
	assert.Contains(t, view, "Sidebar position")
	assert.Contains(t, view, "Right")
	assert.Contains(t, view, "Section spacing")
	assert.Contains(t, view, "Normal")
	assert.Contains(t, view, "Token usage")
	assert.Contains(t, view, "Agents")
	assert.Contains(t, view, "Tools")
	assert.Contains(t, view, "Todos")
}

func TestRenderLayoutPreviewReflectsSections(t *testing.T) {
	t.Parallel()

	full := ansi.Strip(renderLayoutPreview(messages.LayoutSettings{}, previewMaxWidth))
	assert.Contains(t, full, "chat")
	assert.Contains(t, full, "input")
	assert.Contains(t, full, "session")
	assert.Contains(t, full, "usage")
	assert.Contains(t, full, "todos")

	trimmed := ansi.Strip(renderLayoutPreview(messages.LayoutSettings{
		HideUsage: true,
		HideTodos: true,
	}, previewMaxWidth))
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

	// Band layouts list the sections on a single line.
	band := ansi.Strip(renderLayoutPreview(messages.LayoutSettings{SidebarPosition: messages.SidebarTop}, previewMaxWidth))
	assert.Contains(t, band, "session · usage · agents · tools · todos")

	// Narrow widths truncate the band list instead of overflowing.
	narrow := ansi.Strip(renderLayoutPreview(messages.LayoutSettings{SidebarPosition: messages.SidebarTop}, previewMinWidth))
	assert.Contains(t, narrow, "session")
}
