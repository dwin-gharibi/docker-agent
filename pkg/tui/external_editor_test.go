package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tui/components/notification"
	"github.com/docker/docker-agent/pkg/tui/messages"
)

// valueRecordingEditor records SetValue calls so tests can assert what the
// external-editor callback wrote back into the input editor.
type valueRecordingEditor struct {
	mockEditor

	value string
}

func (e *valueRecordingEditor) SetValue(v string) { e.value = v }
func (e *valueRecordingEditor) Value() string     { return e.value }

func writeTempPromptFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prompt.md")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// TestExternalEditorCallback_RefocusesEditor pins the fix for the "edited a
// question in vi with ctrl+g but Enter did nothing" bug: ctrl+g works from
// any panel, so after the external editor exits the callback must request
// focus back on the input editor. Otherwise, when the content panel was
// focused (e.g. while watching a running turn), Enter is routed to the
// transcript and the edited question is never sent or queued.
func TestExternalEditorCallback_RefocusesEditor(t *testing.T) {
	ed := &valueRecordingEditor{}
	path := writeTempPromptFile(t, "What is 2+2?\n")

	msg := externalEditorCallback(ed, path)(nil)

	assert.Equal(t, "What is 2+2?", ed.value, "edited content must land in the editor, without the trailing newline")
	assert.Equal(t, messages.RequestFocusMsg{Target: messages.PanelEditor}, msg,
		"the callback must refocus the editor so Enter sends the edited content")

	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "the temp file must be removed")
}

func TestExternalEditorCallback_EmptyContentClearsEditor(t *testing.T) {
	ed := &valueRecordingEditor{value: "old content"}
	path := writeTempPromptFile(t, "  \n\n")

	msg := externalEditorCallback(ed, path)(nil)

	assert.Empty(t, ed.value, "blank-only content must clear the editor")
	assert.Equal(t, messages.RequestFocusMsg{Target: messages.PanelEditor}, msg)
}

func TestExternalEditorCallback_EditorError(t *testing.T) {
	ed := &valueRecordingEditor{value: "untouched"}
	path := writeTempPromptFile(t, "ignored")

	msg := externalEditorCallback(ed, path)(errors.New("exit status 1"))

	show, ok := msg.(notification.ShowMsg)
	require.True(t, ok, "an editor error must surface as an error notification")
	assert.Equal(t, notification.TypeError, show.Type)
	assert.Equal(t, "untouched", ed.value, "the editor content must not change on error")

	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "the temp file must be removed on error")
}

func TestExternalEditorCallback_ReadError(t *testing.T) {
	ed := &valueRecordingEditor{value: "untouched"}
	path := filepath.Join(t.TempDir(), "missing.md")

	msg := externalEditorCallback(ed, path)(nil)

	show, ok := msg.(notification.ShowMsg)
	require.True(t, ok, "a read failure must surface as an error notification")
	assert.Equal(t, notification.TypeError, show.Type)
	assert.Equal(t, "untouched", ed.value, "the editor content must not change when the temp file cannot be read")
}
