package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/board"
)

func TestProjectsDialogDeleteAsksConfirmation(t *testing.T) {
	d := newProjectsDialog([]board.Project{{Name: "one", Path: "/one"}, {Name: "two", Path: "/two"}})

	// x opens the confirmation instead of deleting right away.
	_, cmd := d.Update(keyPress("x"))
	require.Nil(t, cmd)
	assert.Equal(t, projectsConfirming, d.mode)
	assert.Contains(t, d.View(80, 40), "Remove project?")

	// esc cancels: back to the list, nothing deleted.
	_, cmd = d.Update(keyPress("esc"))
	require.Nil(t, cmd)
	assert.Equal(t, projectsList, d.mode)

	// x then y confirms and emits the delete for the selected project.
	_, _ = d.Update(keyPress("x"))
	_, cmd = d.Update(keyPress("y"))
	require.NotNil(t, cmd)
	assert.Equal(t, deleteProjectMsg{name: "one"}, cmd())
}

func TestProjectsDialogEditPrefillsForm(t *testing.T) {
	d := newProjectsDialog([]board.Project{{Name: "one", Path: "/one", Agent: "coder"}})

	_, _ = d.Update(keyPress("e"))
	require.Equal(t, projectsEditing, d.mode)
	assert.Equal(t, "one", d.editing)
	assert.Equal(t, "one", d.inputs[0].Value())
	assert.Equal(t, "/one", d.inputs[1].Value())
	assert.Equal(t, "coder", d.inputs[2].Value())
	assert.Contains(t, d.View(80, 40), "Edit project")

	// Submitting carries the original name so the model updates in place.
	_, cmd := d.Update(keyPress("enter"))
	require.NotNil(t, cmd)
	assert.Equal(t, submitProjectMsg{
		project: board.Project{Name: "one", Path: "/one", Agent: "coder"},
		oldName: "one",
	}, cmd())
}

func TestProjectsDialogMoveEmitsReorder(t *testing.T) {
	d := newProjectsDialog([]board.Project{{Name: "one"}, {Name: "two"}})

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
	require.NotNil(t, cmd)
	assert.Equal(t, moveProjectMsg{name: "one", delta: 1}, cmd())

	_, cmd = d.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift})
	require.NotNil(t, cmd)
	assert.Equal(t, moveProjectMsg{name: "one", delta: -1}, cmd())
}

// A form in progress must survive a ctrl+o browse round-trip: only the
// path field may change (regression test for the add flow losing the typed
// name and agent).
func TestProjectsDialogBrowseKeepsFormInProgress(t *testing.T) {
	root := pickerDirs(t)
	d := newProjectsDialog(nil)
	_ = d.startForm("", "myname", root, "coder")

	// ctrl+o browses from the form; esc returns to it with fields intact.
	_, _ = d.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	require.Equal(t, projectsPicking, d.mode)
	_, _ = d.Update(keyPress("esc"))
	require.Equal(t, projectsEditing, d.mode)
	assert.Equal(t, "myname", d.inputs[0].Value())
	assert.Equal(t, "coder", d.inputs[2].Value())

	// Picking a directory ("use this directory") replaces only the path.
	_, _ = d.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	require.Equal(t, projectsPicking, d.mode)
	_, _ = d.Update(keyPress("enter"))
	require.Equal(t, projectsEditing, d.mode)
	assert.Equal(t, "myname", d.inputs[0].Value())
	assert.Equal(t, root, d.inputs[1].Value())
	assert.Equal(t, "coder", d.inputs[2].Value())
}
