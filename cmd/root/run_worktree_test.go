package root

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/cli"
	"github.com/docker/docker-agent/pkg/paths"
	"github.com/docker/docker-agent/pkg/worktree"
)

// TestCleanupWorktreeAutoRemovesWhenClean verifies a pristine worktree is
// removed without prompting.
func TestCleanupWorktreeAutoRemovesWhenClean(t *testing.T) {
	wt := createTestWorktree(t)

	var f runExecFlags
	f.cleanupWorktree(t.Context(), discardPrinter(), wt)

	assert.NoDirExists(t, wt.Dir)
}

// TestCleanupWorktreeKeepsDirtyWithoutConfirmation verifies that a worktree
// holding uncommitted work is preserved when the user does not confirm
// removal (here: stdin is at EOF, so the prompt reads no "yes").
func TestCleanupWorktreeKeepsDirtyWithoutConfirmation(t *testing.T) {
	wt := createTestWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(wt.Dir, "untracked.txt"), []byte("x"), 0o644))

	setStdin(t, "") // EOF => prompt reads no answer => keep.

	var f runExecFlags
	f.cleanupWorktree(t.Context(), discardPrinter(), wt)

	assert.DirExists(t, wt.Dir)
}

// TestCleanupWorktreeRemovesDirtyOnConfirmation verifies that explicit
// confirmation discards a worktree that still holds work.
func TestCleanupWorktreeRemovesDirtyOnConfirmation(t *testing.T) {
	wt := createTestWorktree(t)
	require.NoError(t, os.WriteFile(filepath.Join(wt.Dir, "untracked.txt"), []byte("x"), 0o644))

	setStdin(t, "y\n")

	var f runExecFlags
	f.cleanupWorktree(t.Context(), discardPrinter(), wt)

	assert.NoDirExists(t, wt.Dir)
}

func discardPrinter() *cli.Printer {
	return cli.NewPrinter(io.Discard)
}

// setStdin replaces os.Stdin with a pipe pre-loaded with the given input for
// the duration of the test.
func setStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	if input != "" {
		_, err = w.WriteString(input)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())

	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old; _ = r.Close() })
}

func createTestWorktree(t *testing.T) *worktree.Worktree {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test User"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("A"), 0o644))
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-m", "init"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}

	paths.SetDataDir(t.TempDir())
	t.Cleanup(func() { paths.SetDataDir("") })

	wt, err := worktree.Create(t.Context(), dir, "")
	require.NoError(t, err)
	return wt
}
