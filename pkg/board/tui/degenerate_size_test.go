package tui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/docker/docker-agent/pkg/board"
	"github.com/docker/docker-agent/pkg/tui/styles"
)

// TestDegenerateSizesDoNotPanic renders the board — with and without every
// dialog — at the degenerate terminal sizes (0x0, 1x1, …) that real
// terminals and tmux can transiently report. Rendering must never panic.
func TestDegenerateSizesDoNotPanic(t *testing.T) {
	// Theme colors are only bound after ApplyTheme; not parallel because it
	// mutates package-level style state.
	styles.ApplyThemeRef(styles.DefaultThemeRef)

	newModel := func() *model {
		return &model{
			columns: []board.Column{{ID: "dev", Name: "Dev"}, {ID: "done", Name: "Done"}},
			cards: map[string][]*board.Card{
				"dev":  {{ID: "a", Column: "dev", Title: "Card A", Project: "p1"}},
				"done": {{ID: "c", Column: "done", Title: "Card C", Project: "p2"}},
			},
			projects: []board.Project{{Name: "p1"}, {Name: "p2"}},
			scroll:   map[string]int{},
		}
	}

	sizes := [][2]int{{0, 0}, {1, 1}, {2, 2}, {5, 3}, {0, 40}, {80, 0}}
	dialogs := map[string]func() dialog{
		"none":     func() dialog { return nil },
		"help":     func() dialog { return newHelpDialog() },
		"card":     func() dialog { return newCardDialog([]board.Project{{Name: "p1"}, {Name: "p2"}}, "") },
		"prompt":   func() dialog { return newPromptDialog(board.Column{ID: "dev", Name: "Dev"}) },
		"columns":  func() dialog { return newColumnsDialog([]board.Column{{ID: "dev", Name: "Dev"}}) },
		"projects": func() dialog { return newProjectsDialog([]board.Project{{Name: "p1"}}) },
		"diff":     func() dialog { return newDiffDialog("a", "t", "+x\n-y", 3) },
		"confirm":  func() dialog { return newConfirmDialog(&board.Card{ID: "a", Title: "T"}) },
	}

	for _, size := range sizes {
		for name, mk := range dialogs {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], name), func(t *testing.T) {
				m := newModel()
				m.dialog = mk()
				if d, ok := m.dialog.(*projectsDialog); ok {
					// "a" opens the directory picker: exercise that view too.
					_, _ = d.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
				}
				_, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				_ = m.View()

				// The welcome overlay renders on an empty board.
				m.cards = map[string][]*board.Card{}
				m.dialog = nil
				_ = m.View()
			})
		}
	}
}
