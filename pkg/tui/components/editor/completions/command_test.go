package completions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tui/commands"
	"github.com/docker/docker-agent/pkg/tui/components/completion"
)

// categoriesWithToolsetRestart mirrors the real "Session" category's
// "Restart Toolset" command, whose label doesn't share a prefix with its
// "/toolset-restart" slash command — the case that exposed the
// inline-completion bug.
func categoriesWithToolsetRestart() []commands.Category {
	return []commands.Category{
		{
			Name: "Session",
			Commands: []commands.Item{
				{ID: "session.exit", Label: "Exit", SlashCommand: "/exit"},
				{ID: "session.toolset.restart", Label: "Restart Toolset", SlashCommand: "/toolset-restart"},
			},
		},
	}
}

func TestCommandCompletionItems(t *testing.T) {
	t.Parallel()

	items := NewCommandCompletion(categoriesWithToolsetRestart()).Items()

	require.Len(t, items, 2)
	labels := map[string]string{}
	for _, item := range items {
		labels[item.Label] = item.Value
	}
	assert.Equal(t, "/toolset-restart", labels["Restart Toolset"])
	assert.Equal(t, "/exit", labels["Exit"])
}

// TestCommandCompletionMatchesToolsetRestartQuery exercises the full pipeline
// the editor uses: items from commandCompletion.Items(), filtered by the
// completion manager in the command completion's own MatchMode. It guards
// against a regression where typing "/toolset-restart" (trigger stripped to
// "toolset-restart") failed to surface the command because filtering only
// looked at Label ("Restart Toolset"), never at Value ("/toolset-restart").
func TestCommandCompletionMatchesToolsetRestartQuery(t *testing.T) {
	t.Parallel()

	cmdCompletion := NewCommandCompletion(categoriesWithToolsetRestart())

	for _, query := range []string{"toolset-restart", "tool", "t"} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()

			m := completion.New()
			m.Update(completion.OpenMsg{
				Items:     cmdCompletion.Items(),
				MatchMode: cmdCompletion.MatchMode(),
			})
			_, cmd := m.Update(completion.QueryMsg{Query: query})
			require.NotNil(t, cmd)

			view := m.View()
			assert.Contains(t, view, "Restart Toolset", "toolset-restart command should appear for query %q", query)
		})
	}
}

func TestCommandCompletionMatchModeIsPrefix(t *testing.T) {
	t.Parallel()

	c := NewCommandCompletion(nil)
	assert.Equal(t, completion.MatchPrefix, c.MatchMode())
}
