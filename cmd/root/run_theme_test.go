package root

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/paths"
	"github.com/docker/docker-agent/pkg/tui/styles"
)

func TestValidateTheme(t *testing.T) {
	t.Parallel()

	t.Run("accepts built-in theme", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, validateTheme("nord"))
	})

	t.Run("accepts default theme", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, validateTheme(styles.DefaultThemeRef))
	})

	t.Run("accepts auto sentinel", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, validateTheme(styles.AutoThemeRef))
	})

	t.Run("rejects unknown theme with helpful message", func(t *testing.T) {
		t.Parallel()
		err := validateTheme("does-not-exist")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does-not-exist")
		assert.Contains(t, err.Error(), "available themes")
		assert.Contains(t, err.Error(), styles.AutoThemeRef)
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		t.Parallel()
		require.Error(t, validateTheme("../../etc/passwd"))
	})
}

func TestApplyThemePrecedence(t *testing.T) {
	// Not parallel: mutates the process-global applied theme via ApplyTheme.
	// Isolate config/data dirs so a developer's real user config (which may
	// pin a theme) cannot influence the precedence assertions.
	dir := t.TempDir()
	paths.SetConfigDir(dir)
	paths.SetDataDir(dir)
	prevDark := styles.TerminalIsDark()
	prevEnabled := styles.AutoThemeEnabled()
	t.Cleanup(func() {
		paths.SetConfigDir("")
		paths.SetDataDir("")
		styles.SetTerminalDark(prevDark)
		styles.SetAutoThemeEnabled(prevEnabled)
	})

	t.Run("override takes precedence and is applied", func(t *testing.T) {
		applyTheme("nord")
		assert.Equal(t, "nord", styles.CurrentTheme().Ref)
	})

	t.Run("invalid override falls back to default theme", func(t *testing.T) {
		// applyTheme tolerates an invalid ref (validateTheme guards the CLI
		// entry point); it must never panic and should apply the default.
		applyTheme("does-not-exist")
		assert.Equal(t, styles.DefaultThemeRef, styles.CurrentTheme().Ref)
	})

	t.Run("empty override applies default when no user config theme", func(t *testing.T) {
		applyTheme("")
		assert.Equal(t, styles.DefaultThemeRef, styles.CurrentTheme().Ref)
	})

	t.Run("auto falls back to dark default without a terminal", func(t *testing.T) {
		// Tests run without a TTY, so detection must skip the query and
		// resolve auto to the dark half of the pair.
		applyTheme("auto")
		assert.Equal(t, styles.DefaultThemeRef, styles.CurrentTheme().Ref)
		assert.True(t, styles.AutoThemeEnabled())
		assert.True(t, styles.TerminalIsDark())
	})

	t.Run("concrete override disables a previously enabled auto theme", func(t *testing.T) {
		applyTheme("auto")
		applyTheme("nord")
		assert.Equal(t, "nord", styles.CurrentTheme().Ref)
		assert.False(t, styles.AutoThemeEnabled())
	})
}
