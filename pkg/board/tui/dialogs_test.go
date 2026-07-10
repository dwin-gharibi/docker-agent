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

func TestColumnsDialogDeleteAsksConfirmation(t *testing.T) {
	d := newColumnsDialog([]board.Column{{ID: "dev", Name: "Dev"}, {ID: "done", Name: "Done"}})

	// x opens the confirmation instead of deleting right away.
	_, cmd := d.Update(keyPress("x"))
	require.Nil(t, cmd)
	assert.Equal(t, columnsConfirming, d.mode)
	assert.Contains(t, d.View(80, 40), "Remove column?")

	// esc cancels: back to the list, nothing deleted.
	_, cmd = d.Update(keyPress("esc"))
	require.Nil(t, cmd)
	assert.Equal(t, columnsList, d.mode)

	// x then y confirms and emits the delete for the selected column.
	_, _ = d.Update(keyPress("x"))
	_, cmd = d.Update(keyPress("y"))
	require.NotNil(t, cmd)
	assert.Equal(t, deleteColumnMsg{id: "dev"}, cmd())
}

// Editing a column keeps its id and prompt: the form only exposes the
// display fields, and the id is what cards are attached to.
func TestColumnsDialogEditKeepsIDAndPrompt(t *testing.T) {
	d := newColumnsDialog([]board.Column{{ID: "dev", Name: "Dev", Emoji: "🔨", Prompt: "build it"}})

	_, _ = d.Update(keyPress("e"))
	require.Equal(t, columnsEditing, d.mode)
	assert.Equal(t, "Dev", d.inputs[0].Value())
	assert.Equal(t, "🔨", d.inputs[1].Value())
	assert.Contains(t, d.View(80, 40), "Edit column")

	_, cmd := d.Update(keyPress("enter"))
	require.NotNil(t, cmd)
	assert.Equal(t, submitColumnMsg{
		column: board.Column{ID: "dev", Name: "Dev", Emoji: "🔨", Prompt: "build it"},
		oldID:  "dev",
	}, cmd())
}

func TestColumnsDialogAddSubmitsNewColumn(t *testing.T) {
	d := newColumnsDialog([]board.Column{{ID: "dev", Name: "Dev"}})

	_, _ = d.Update(keyPress("a"))
	require.Equal(t, columnsEditing, d.mode)
	for _, r := range "QA" {
		_, _ = d.Update(keyPress(string(r)))
	}
	_, cmd := d.Update(keyPress("enter"))
	require.NotNil(t, cmd)
	// No oldID: the model adds a new column (the engine derives its id).
	assert.Equal(t, submitColumnMsg{column: board.Column{Name: "QA"}}, cmd())
}

func TestColumnsDialogMoveEmitsReorder(t *testing.T) {
	d := newColumnsDialog([]board.Column{{ID: "dev", Name: "Dev"}, {ID: "done", Name: "Done"}})

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
	require.NotNil(t, cmd)
	assert.Equal(t, moveColumnMsg{id: "dev", delta: 1}, cmd())

	_, cmd = d.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift})
	require.NotNil(t, cmd)
	assert.Equal(t, moveColumnMsg{id: "dev", delta: -1}, cmd())
}

// p hands the selected column to the dedicated prompt editor: prompts are
// long-form and do not fit the single-line form.
func TestColumnsDialogPromptOpensEditor(t *testing.T) {
	col := board.Column{ID: "dev", Name: "Dev", Prompt: "build it"}
	d := newColumnsDialog([]board.Column{col})

	_, cmd := d.Update(keyPress("p"))
	require.NotNil(t, cmd)
	assert.Equal(t, editColumnPromptMsg{column: col}, cmd())
}

// The prompt editor's title and placeholder render the column's name and
// emoji, which come from the hand-editable config file: terminal controls
// must be stripped like at every other column render site.
func TestPromptDialogSanitizesColumnName(t *testing.T) {
	d := newPromptDialog(board.Column{ID: "dev", Name: "Dev\x1b]0;pwned\x07", Emoji: "\x1b[31m🔨"})

	view := d.View(80, 40)
	assert.NotContains(t, view, "\x1b]0;pwned")
	assert.NotContains(t, view, "\x1b[31m🔨")
	assert.Contains(t, view, "Dev")
}
