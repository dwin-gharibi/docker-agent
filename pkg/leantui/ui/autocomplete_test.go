package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCommands() []Command {
	return []Command{
		{Name: "new", Desc: "Start a new session", Kind: CmdBuiltin},
		{Name: "help", Desc: "Show help", Kind: CmdBuiltin},
		{Name: "compact", Desc: "Compact", Kind: CmdBuiltin},
		{Name: "plan", Desc: "Switch to planner", Kind: CmdAgent},
	}
}

func TestAutocompleteActivation(t *testing.T) {
	t.Parallel()
	a := NewAutocomplete()
	a.SetCommands(testCommands())

	assert.True(t, a.Sync("/ne"))
	cur, ok := a.Current()
	require.True(t, ok)
	assert.Equal(t, "new", cur.Name)

	assert.False(t, a.Sync("hello"))  // no leading slash
	assert.False(t, a.Sync("/new x")) // contains a space
	assert.False(t, a.Sync("/zzzzz")) // no matches
}

func TestAutocompleteNavigation(t *testing.T) {
	t.Parallel()
	a := NewAutocomplete()
	a.SetCommands(testCommands())
	require.True(t, a.Sync("/")) // all commands match

	first, _ := a.Current()
	a.MoveDown()
	second, _ := a.Current()
	assert.NotEqual(t, first.Name, second.Name)

	a.MoveUp()
	back, _ := a.Current()
	assert.Equal(t, first.Name, back.Name)
}

func TestAutocompleteRenderWidth(t *testing.T) {
	t.Parallel()
	a := NewAutocomplete()
	a.SetCommands(testCommands())
	require.True(t, a.Sync("/"))
	rows := a.Render(60)
	assert.NotEmpty(t, rows)
	for _, r := range rows {
		assert.LessOrEqual(t, DisplayWidth(r), 60)
	}
}

func TestAutocompleteBuiltinsBeforeAgent(t *testing.T) {
	t.Parallel()
	matches := FilterCommands(testCommands(), "")
	// The agent command "plan" must sort after every built-in.
	last := matches[len(matches)-1]
	assert.Equal(t, "plan", last.Name)
	assert.Equal(t, CmdAgent, last.Kind)
}
