package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSwitchFocusArgumentCompletion covers the Tab/switchFocus wiring: an
// open argument-completion popup (e.g. "/toolset-restart ") must win over
// the normal editor<->content focus toggle, but plain text must still
// switch focus as before.
func TestSwitchFocusArgumentCompletion(t *testing.T) {
	t.Parallel()

	t.Run("argument completion available: focus stays on editor", func(t *testing.T) {
		t.Parallel()

		m, ed := newTestModel(t)
		m.focusedPanel = PanelEditor
		type argCompletionOpenedMsg struct{}
		ed.argumentCompletionCmd = func() tea.Msg { return argCompletionOpenedMsg{} }

		_, cmd := m.switchFocus()

		assert.Equal(t, PanelEditor, m.focusedPanel, "focus should not switch when the popup opens")
		require.NotNil(t, cmd)
		msgs := collectMsgs(cmd)
		require.Len(t, msgs, 1)
		_, ok := msgs[0].(argCompletionOpenedMsg)
		assert.True(t, ok, "should return the command from TryStartArgumentCompletion")
	})

	t.Run("no argument completion: focus switches to content as before", func(t *testing.T) {
		t.Parallel()

		m, ed := newTestModel(t)
		m.focusedPanel = PanelEditor
		ed.argumentCompletionCmd = nil

		_, _ = m.switchFocus()

		assert.Equal(t, PanelContent, m.focusedPanel, "focus should switch when there's nothing to complete")
	})

	t.Run("argument completion available takes priority over a pending suggestion", func(t *testing.T) {
		t.Parallel()

		// Regression test: a stale ghost suggestion (e.g. a history match like
		// "/toolset-restart oldtool" left over from a previous run) must not
		// win the race for Tab and get silently accepted in place of opening a
		// fresh argument-completion popup - that was bug 2, and the fix is to
		// check TryStartArgumentCompletion before AcceptSuggestion in
		// switchFocus.
		m, ed := newTestModel(t)
		m.focusedPanel = PanelEditor
		type argCompletionOpenedMsg struct{}
		type suggestionAcceptedMsg struct{}
		ed.argumentCompletionCmd = func() tea.Msg { return argCompletionOpenedMsg{} }
		ed.acceptSuggestionCmd = func() tea.Msg { return suggestionAcceptedMsg{} }

		_, cmd := m.switchFocus()

		assert.Equal(t, PanelEditor, m.focusedPanel)
		require.NotNil(t, cmd)
		msgs := collectMsgs(cmd)
		require.Len(t, msgs, 1)
		_, ok := msgs[0].(argCompletionOpenedMsg)
		assert.True(t, ok, "argument completion must win over accepting a pending suggestion")
	})

	t.Run("no argument completion falls back to accepting a pending suggestion", func(t *testing.T) {
		t.Parallel()

		m, ed := newTestModel(t)
		m.focusedPanel = PanelEditor
		ed.argumentCompletionCmd = nil
		type suggestionAcceptedMsg struct{}
		ed.acceptSuggestionCmd = func() tea.Msg { return suggestionAcceptedMsg{} }

		_, cmd := m.switchFocus()

		assert.Equal(t, PanelEditor, m.focusedPanel, "focus should not switch when a suggestion was accepted")
		require.NotNil(t, cmd)
		msgs := collectMsgs(cmd)
		require.Len(t, msgs, 1)
		_, ok := msgs[0].(suggestionAcceptedMsg)
		assert.True(t, ok)
	})
}
