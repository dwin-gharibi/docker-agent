package board

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/paths"
	"github.com/docker/docker-agent/pkg/userconfig"
)

// The data dir (~/.cagent) may be bind-mounted into a docker sandbox, where
// unix sockets cannot be bound: an agent whose --listen socket lived there
// would die at startup. Control-plane sockets must live in the board's
// local, per-user socket dir instead.
func TestSocketPathOutsideDataDir(t *testing.T) {
	t.Parallel()

	sock := socketPath("abc123")
	assert.False(t, strings.HasPrefix(sock, paths.GetDataDir()+string(filepath.Separator)))

	dir, err := socketDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "abc123.sock"), sock)
}

func TestPlaceholderTitle(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Fix the bug", placeholderTitle("Fix the bug"))
	assert.Equal(t, "First line", placeholderTitle("First line\nsecond line"))
	assert.Equal(t, "Trimmed", placeholderTitle("  Trimmed  "))

	long := placeholderTitle(strings.Repeat("word ", 20))
	assert.LessOrEqual(t, len([]rune(long)), 41)
	assert.True(t, strings.HasSuffix(long, "…"))

	// A long word without boundaries is cut mid-word.
	assert.True(t, strings.HasSuffix(placeholderTitle(strings.Repeat("a", 50)), "…"))
}

func TestColumnsFromConfig(t *testing.T) {
	t.Parallel()

	assert.Equal(t, DefaultColumns, ColumnsFromConfig(nil))

	cols := ColumnsFromConfig([]userconfig.BoardColumn{
		{ID: "todo", Name: "Todo", Emoji: "📝", Prompt: "do it"},
	})
	assert.Equal(t, []Column{{ID: "todo", Name: "Todo", Emoji: "📝", Prompt: "do it"}}, cols)

	// Hand-edited configs are normalized: a missing id is derived from the
	// name, duplicate ids and unusable entries are dropped, a missing name
	// falls back to the id, and whitespace (including newlines, which would
	// break the single-line headers) collapses to single spaces.
	cols = ColumnsFromConfig([]userconfig.BoardColumn{
		{Name: "In\nReview", Emoji: " 🔍 "},
		{ID: "in-review", Name: "Dup"},
		{Emoji: "🚧"},
		{ID: " done "},
	})
	assert.Equal(t, []Column{
		{ID: "in-review", Name: "In Review", Emoji: "🔍"},
		{ID: "done", Name: "done"},
	}, cols)

	// A name that slugs to nothing (non-ASCII) still gets an id, hashed
	// from the name so cards stay attached across restarts.
	hashed := ColumnsFromConfig([]userconfig.BoardColumn{{Name: "レビュー"}})
	require.Len(t, hashed, 1)
	assert.NotEmpty(t, hashed[0].ID)
	assert.Equal(t, "レビュー", hashed[0].Name)
	assert.Equal(t, hashed, ColumnsFromConfig([]userconfig.BoardColumn{{Name: "レビュー"}}))

	// An entirely unusable config falls back to the defaults.
	assert.Equal(t, DefaultColumns, ColumnsFromConfig([]userconfig.BoardColumn{{Emoji: "🚧"}}))
}

func TestColumnID(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "qa-check", columnID("  QA   Check "))
	assert.Equal(t, "fix-2", columnID("Fix_2"))
	assert.Empty(t, columnID("🚧"))
	assert.Empty(t, columnID("  "))
}

func TestCardStatusBusy(t *testing.T) {
	t.Parallel()

	assert.True(t, StatusStarting.Busy())
	assert.True(t, StatusLoading.Busy())
	assert.True(t, StatusAttaching.Busy())
	assert.True(t, StatusRunning.Busy())
	assert.False(t, StatusWaiting.Busy())
	assert.False(t, StatusPaused.Busy())
	assert.False(t, StatusError.Busy())
}

func TestCardStatusStartingUp(t *testing.T) {
	t.Parallel()

	assert.True(t, StatusStarting.StartingUp())
	assert.True(t, StatusLoading.StartingUp())
	assert.True(t, StatusAttaching.StartingUp())
	assert.False(t, StatusRunning.StartingUp())
	assert.False(t, StatusWaiting.StartingUp())
	assert.False(t, StatusPaused.StartingUp())
	assert.False(t, StatusError.StartingUp())
}

func TestNewWorktreeName(t *testing.T) {
	t.Parallel()

	name := newWorktreeName()
	assert.True(t, strings.HasPrefix(name, "board-"))
	assert.NotContains(t, name, "/")
	assert.NotEqual(t, name, newWorktreeName())
}
