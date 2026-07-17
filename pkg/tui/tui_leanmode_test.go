package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/app"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tui/commands"
	"github.com/docker/docker-agent/pkg/tui/components/editor/completions"
)

// leanModeTestCategories mirrors the real "Session"/"Settings" categories
// closely enough to exercise lean-mode filtering: /settings is a no-op in
// lean mode (OpenSettingsDialogMsg is dropped), /exit is not.
func leanModeTestCategories(context.Context, tea.Model) []commands.Category {
	noop := func(string) tea.Cmd { return func() tea.Msg { return nil } }
	return []commands.Category{
		{
			Name: "Session",
			Commands: []commands.Item{
				{ID: "session.exit", Label: "Exit", SlashCommand: "/exit", Immediate: true, Execute: noop},
			},
		},
		{
			Name: "Settings",
			Commands: []commands.Item{
				{ID: "settings.open", Label: "Settings", SlashCommand: "/settings", Immediate: true, Execute: noop},
			},
		},
	}
}

func TestCommandCategories_LeanModeExcludesSettings(t *testing.T) {
	t.Parallel()

	t.Run("lean mode drops /settings from categories, completion, and the parser", func(t *testing.T) {
		t.Parallel()

		m := &appModel{ctx: t.Context, buildCommandCategories: leanModeTestCategories}
		WithLeanMode()(m)

		categories := m.commandCategories()
		require.Len(t, categories, 1, "the now-empty Settings category should be dropped")
		assert.Equal(t, "Session", categories[0].Name)

		items := completions.NewCommandCompletion(categories).Items()
		for _, item := range items {
			assert.NotEqual(t, "/settings", item.Value, "inline completion should not offer /settings in lean mode")
		}

		parser := commands.NewParser(categories...)
		assert.Nil(t, parser.Parse("/settings"), "the palette parser should not run /settings in lean mode")
	})

	t.Run("classic mode keeps /settings in categories, completion, and the parser", func(t *testing.T) {
		t.Parallel()

		m := &appModel{ctx: t.Context, buildCommandCategories: leanModeTestCategories}

		categories := m.commandCategories()
		require.Len(t, categories, 2)

		items := completions.NewCommandCompletion(categories).Items()
		var sawSettings bool
		for _, item := range items {
			if item.Value == "/settings" {
				sawSettings = true
			}
		}
		assert.True(t, sawSettings, "inline completion should offer /settings in classic mode")

		parser := commands.NewParser(categories...)
		cmd := parser.Parse("/settings")
		require.NotNil(t, cmd, "the palette parser should run /settings in classic mode")
	})
}

// TestLeanModeDisabledSlashCommandsMatchDroppedMessages guards
// leanModeDisabledSlashCommands against drifting out of sync with
// leanModeDroppedMessageTypes (the lean-mode message-drop switch in
// appModel.update): every built-in slash command whose Execute produces a
// dropped message type must be listed, and every listed command must
// actually produce one of those types.
func TestLeanModeDisabledSlashCommandsMatchDroppedMessages(t *testing.T) {
	t.Parallel()

	application := app.New(t.Context(), stubRuntime{}, session.New())
	categories := commands.BuildCommandCategories(t.Context(), application)

	disabled := make(map[string]bool, len(leanModeDisabledSlashCommands))
	for _, c := range leanModeDisabledSlashCommands {
		disabled[c] = true
	}

	matched := make(map[string]bool, len(leanModeDisabledSlashCommands))
	for _, category := range categories {
		for _, item := range category.Commands {
			if item.SlashCommand == "" || !item.Immediate || item.Execute == nil {
				continue
			}
			cmd := item.Execute("")
			if cmd == nil {
				continue
			}
			msg := cmd()

			if isLeanModeNoOp(msg) {
				matched[item.SlashCommand] = true
				assert.True(t, disabled[item.SlashCommand],
					"%s produces %T, which lean mode drops, but is missing from leanModeDisabledSlashCommands", item.SlashCommand, msg)
			} else {
				assert.False(t, disabled[item.SlashCommand],
					"%s is listed in leanModeDisabledSlashCommands but produces %T, which lean mode does not drop", item.SlashCommand, msg)
			}
		}
	}

	for _, c := range leanModeDisabledSlashCommands {
		assert.True(t, matched[c], "%s is listed in leanModeDisabledSlashCommands but no built-in command produces a dropped message type for it", c)
	}
}
