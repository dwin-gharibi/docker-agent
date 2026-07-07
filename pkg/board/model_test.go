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
