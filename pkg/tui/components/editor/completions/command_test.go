package completions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tui/commands"
	"github.com/docker/docker-agent/pkg/tui/components/completion"
)

// categoriesWithSettings mirrors the real "Settings" category, whose
// "Preferences" label doesn't share a prefix with its "/settings" slash
// command — the case that exposed the inline-completion bug.
func categoriesWithSettings() []commands.Category {
	return []commands.Category{
		{
			Name: "Session",
			Commands: []commands.Item{
				{ID: "session.exit", Label: "Exit", SlashCommand: "/exit"},
			},
		},
		{
			Name: "Settings",
			Commands: []commands.Item{
				{ID: "settings.open", Label: "Preferences", SlashCommand: "/settings"},
			},
		},
	}
}

func TestCommandCompletionItems(t *testing.T) {
	t.Parallel()

	items := NewCommandCompletion(categoriesWithSettings()).Items()

	require.Len(t, items, 2)
	labels := map[string]string{}
	for _, item := range items {
		labels[item.Label] = item.Value
	}
	assert.Equal(t, "/settings", labels["Preferences"])
	assert.Equal(t, "/exit", labels["Exit"])
}

// TestCommandCompletionMatchesSettingsQuery exercises the full pipeline the
// editor uses: items from commandCompletion.Items(), filtered by the
// completion manager in the command completion's own MatchMode. It guards
// against a regression where typing "/settings" (trigger stripped to
// "settings") failed to surface the settings command because filtering only
// looked at Label ("Preferences"), never at Value ("/settings").
func TestCommandCompletionMatchesSettingsQuery(t *testing.T) {
	t.Parallel()

	cmdCompletion := NewCommandCompletion(categoriesWithSettings())

	for _, query := range []string{"settings", "set", "s"} {
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
			assert.Contains(t, view, "Preferences", "settings command should appear for query %q", query)
		})
	}
}

func TestCommandCompletionMatchModeIsPrefix(t *testing.T) {
	t.Parallel()

	c := NewCommandCompletion(nil)
	assert.Equal(t, completion.MatchPrefix, c.MatchMode())
}
