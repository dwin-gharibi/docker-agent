package tui

import (
	"testing"

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
