package root

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGettingStarted_DispatchMechanics validates the plumbing the
// getting-started command relies on: the run command is reachable from the
// root, exposes the hidden --tour flag, and has the PersistentPreRunE hook
// the dispatch invokes. The interactive TTY path itself is covered by the
// tuitest e2e suite.
func TestGettingStarted_DispatchMechanics(t *testing.T) {
	t.Parallel()

	root := NewRootCmd()

	runCmd, _, err := root.Find([]string{"run"})
	require.NoError(t, err)
	require.Equal(t, "run", runCmd.Name())
	require.NotNil(t, runCmd.PersistentPreRunE)

	require.NoError(t, runCmd.PersistentFlags().Set("tour", "true"))

	tourFlag := runCmd.PersistentFlags().Lookup("tour")
	require.NotNil(t, tourFlag)
	assert.Equal(t, "true", tourFlag.Value.String())
	assert.True(t, tourFlag.Hidden)
}

func TestGettingStarted_RegisteredOnRoot(t *testing.T) {
	t.Parallel()

	cmd, _, err := NewRootCmd().Find([]string{"getting-started"})
	require.NoError(t, err)
	assert.Equal(t, "getting-started", cmd.Name())
	assert.Contains(t, cmd.Aliases, "tour")

	// The alias resolves to the same command.
	viaAlias, _, err := NewRootCmd().Find([]string{"tour"})
	require.NoError(t, err)
	assert.Equal(t, "getting-started", viaAlias.Name())
}
