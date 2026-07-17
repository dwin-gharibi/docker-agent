package completions

import (
	"context"

	"github.com/docker/docker-agent/pkg/tui/components/completion"
)

type Completion interface {
	Trigger() string
	Items() []completion.Item
	RequiresEmptyEditor() bool
	// MatchMode returns how items should be filtered (fuzzy or prefix)
	MatchMode() completion.MatchMode
}

// AsyncLoader is an optional interface for completions that support async loading.
// This allows the editor to load items in the background without blocking the UI.
type AsyncLoader interface {
	// LoadInitialItemsAsync loads a shallow set of items quickly (e.g., 2 levels deep, ~100 files).
	// Returns a channel that receives initial items for immediate display.
	LoadInitialItemsAsync(ctx context.Context) <-chan []completion.Item

	// LoadItemsAsync loads all items in a background goroutine with context support.
	// It returns a channel that receives the items when loading is complete.
	LoadItemsAsync(ctx context.Context) <-chan []completion.Item
}

// ArgumentCompleter is an optional interface for a Completion source that can
// also complete a command's ARGUMENT (as opposed to the command name
// itself). It is command-agnostic: it works for any commands.Item exposing a
// CompleteArgument provider, not just /toolset-restart.
type ArgumentCompleter interface {
	// ArgumentItems returns the completion items for the argument of the
	// command found at the start of line (e.g. "/toolset-restart git"), and
	// whether line matched a command with argument candidates at all. A
	// false second value means the caller should not open a popup (unknown
	// command, or a command without an argument provider).
	ArgumentItems(line string) ([]completion.Item, bool)
}
