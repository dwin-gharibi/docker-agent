//go:build !windows

package root

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentRefsInDirSkipsNonRegularFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "real.yaml"), nil, 0o644))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "fifo.yaml"), 0o644))
	// A symlink to a regular config file is kept.
	require.NoError(t, os.Symlink(filepath.Join(dir, "real.yaml"), filepath.Join(dir, "link.yaml")))

	want := []string{filepath.Join(dir, "link.yaml"), filepath.Join(dir, "real.yaml")}
	assert.Equal(t, want, agentRefsInDir(dir))
}
