package completion

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompletionManagerDisabledItems verifies that Disabled items (e.g. a
// non-restartable toolset for /toolset-restart) are shown for context but
// cannot be submitted: Enter/Tab are a no-op and the popup stays open, the
// ghost-suggestion preview stays empty while one is selected, and cursor
// movement still lands on them.
func TestCompletionManagerDisabledItems(t *testing.T) {
	t.Parallel()

	t.Run("enter on a disabled item is a no-op and keeps the popup open", func(t *testing.T) {
		t.Parallel()

		m := New().(*manager)
		m.Update(OpenMsg{Items: []Item{
			{Label: "filesystem", Value: "/toolset-restart filesystem", Disabled: true},
		}})
		require.True(t, m.visible)

		_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

		assert.True(t, m.visible, "popup must stay open on a disabled entry")
		assert.Nil(t, cmd, "no SelectedMsg/ClosedMsg should be dispatched")
	})

	t.Run("tab on a disabled item is a no-op and keeps the popup open", func(t *testing.T) {
		t.Parallel()

		m := New().(*manager)
		m.Update(OpenMsg{Items: []Item{
			{Label: "filesystem", Value: "/toolset-restart filesystem", Disabled: true},
		}})

		_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})

		assert.True(t, m.visible)
		assert.Nil(t, cmd)
	})

	t.Run("enter on an enabled item still submits as usual", func(t *testing.T) {
		t.Parallel()

		m := New().(*manager)
		m.Update(OpenMsg{Items: []Item{
			{Label: "github", Value: "/toolset-restart github", Disabled: false},
		}})

		_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

		assert.False(t, m.visible, "popup must close on a normal entry")
		assert.NotNil(t, cmd)
	})

	t.Run("selecting a disabled item sends an empty preview value", func(t *testing.T) {
		t.Parallel()

		m := New().(*manager)
		m.Update(OpenMsg{Items: []Item{
			{Label: "github", Value: "/toolset-restart github", Disabled: false},
			{Label: "filesystem", Value: "/toolset-restart filesystem", Disabled: true},
		}})

		// Move selection onto the disabled item.
		_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		assert.Equal(t, 1, m.selected, "cursor movement onto a disabled item must still work")

		require.NotNil(t, cmd)
		msg := cmd()
		selChanged, ok := msg.(SelectionChangedMsg)
		require.True(t, ok)
		assert.Empty(t, selChanged.Value, "disabled items must not drive the ghost-suggestion preview")
	})

	t.Run("view dims a disabled item and does not dim a normal one", func(t *testing.T) {
		t.Parallel()

		m := New().(*manager)
		m.width = 80
		m.height = 24
		m.Update(OpenMsg{Items: []Item{
			{Label: "github", Description: "MCP · ready", Value: "/toolset-restart github", Disabled: false},
			{Label: "filesystem", Description: "Built-in · (not restartable)", Value: "/toolset-restart filesystem", Disabled: true},
		}})

		view := m.View()
		assert.Contains(t, view, "github")
		assert.Contains(t, view, "filesystem")
		assert.Contains(t, view, "not restartable")
	})

	t.Run("view never leaks raw ANSI escape text, selected or not, disabled or not", func(t *testing.T) {
		t.Parallel()

		// Regression test for a bug where selecting a disabled row rendered
		// the literal escape text (e.g. "[3;38;2;90;106;168m...[m") instead of
		// a styled line: completion.go's View() rendered the description with
		// descStyle.Render(...) and then re-wrapped the whole line (label +
		// already-ANSI description) in itemStyle.Render(...). That outer
		// re-render mangled the inner ANSI reset whenever itemStyle also set an
		// attribute (e.g. CompletionDisabledSelectedStyle's Underline).
		newManager := func(selected int) *manager {
			m := New().(*manager)
			m.width = 80
			m.height = 24
			m.Update(OpenMsg{Items: []Item{
				{Label: "github", Description: "MCP · ready", Value: "/toolset-restart github", Disabled: false},
				{Label: "filesystem", Description: "Built-in · ready (not restartable)", Value: "/toolset-restart filesystem", Disabled: true},
			}})
			m.selected = selected
			return m
		}

		for _, tc := range []struct {
			name     string
			selected int
			wantText string
		}{
			{"disabled selected", 1, "Built-in · ready (not restartable)"},
			{"disabled unselected", 0, "Built-in · ready (not restartable)"},
			{"enabled selected", 0, "MCP · ready"},
			{"enabled unselected", 1, "MCP · ready"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				visible := ansi.Strip(newManager(tc.selected).View())

				assert.NotContains(t, visible, "[3;", "raw ANSI SGR params must not leak as literal text")
				assert.NotContains(t, visible, "[m", "raw ANSI reset must not leak as literal text")
				assert.Contains(t, visible, tc.wantText, "the annotation must still render")
			})
		}
	})
}
