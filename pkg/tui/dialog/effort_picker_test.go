package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/effort"
)

func newTestEffortPicker(t *testing.T, levels []effort.Level, current effort.Level) *effortPickerDialog {
	t.Helper()
	dialog := NewEffortPickerDialog(levels, current)
	d, ok := dialog.(*effortPickerDialog)
	require.True(t, ok)
	d.Init()
	d.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	return d
}

func TestEffortPickerPreselectsCurrent(t *testing.T) {
	t.Parallel()

	levels := []effort.Level{effort.None, effort.Low, effort.Medium, effort.High}
	d := newTestEffortPicker(t, levels, effort.Medium)

	require.Equal(t, 2, d.selected, "current level should be preselected")
}

func TestEffortPickerUnknownCurrentSelectsFirst(t *testing.T) {
	t.Parallel()

	levels := []effort.Level{effort.None, effort.Low, effort.Medium, effort.High}
	d := newTestEffortPicker(t, levels, "")

	require.Equal(t, 0, d.selected, "unknown current should leave the first level selected")
}

func TestEffortPickerNavigation(t *testing.T) {
	t.Parallel()

	levels := []effort.Level{effort.None, effort.Low, effort.Medium}
	d := newTestEffortPicker(t, levels, effort.None)

	downKey := tea.KeyPressMsg{Code: tea.KeyDown}
	upKey := tea.KeyPressMsg{Code: tea.KeyUp}

	d.Update(downKey)
	require.Equal(t, 1, d.selected)

	d.Update(downKey)
	require.Equal(t, 2, d.selected)

	// At the end, down must not move past the last level.
	d.Update(downKey)
	require.Equal(t, 2, d.selected)

	d.Update(upKey)
	require.Equal(t, 1, d.selected)

	d.Update(upKey)
	d.Update(upKey)
	require.Equal(t, 0, d.selected, "up must not move before the first level")
}

func TestEffortPickerSelectionReturnsCommand(t *testing.T) {
	t.Parallel()

	levels := []effort.Level{effort.None, effort.Low, effort.Medium}
	d := newTestEffortPicker(t, levels, effort.Low)

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd, "enter should return a command")
}

func TestEffortPickerEscape(t *testing.T) {
	t.Parallel()

	levels := []effort.Level{effort.None, effort.Low}
	d := newTestEffortPicker(t, levels, effort.Low)

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.NotNil(t, cmd, "escape should return a command")
}

func TestEffortPickerViewShowsLevelsAndCurrent(t *testing.T) {
	t.Parallel()

	levels := []effort.Level{effort.None, effort.Low, effort.Medium, effort.High, effort.Max}
	d := newTestEffortPicker(t, levels, effort.High)

	view := d.View()
	assert.Contains(t, view, "Select Reasoning Effort")
	for _, level := range levels {
		assert.Contains(t, view, level.String())
	}
	assert.Contains(t, view, "(current)")
}

func TestEffortPickerViewChangesOnNavigation(t *testing.T) {
	t.Parallel()

	levels := []effort.Level{effort.None, effort.Low, effort.Medium}
	d := newTestEffortPicker(t, levels, effort.None)

	view1 := d.View()
	d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	view2 := d.View()

	require.NotEqual(t, view1, view2, "view should change after navigation")
}
