package tui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tui/messages"
	"github.com/docker/docker-agent/pkg/tui/styles"
	"github.com/docker/docker-agent/pkg/tui/tuitest"
)

// TestTheme_HotReload is the regression net for the theme watcher wiring
// (issue #3501): editing the active custom theme file while the TUI runs
// must hot-reload it without a restart. It drives the full loop — switch to
// a user theme, edit its file on disk, and wait for the watcher to emit
// ThemeFileChangedMsg, which surfaces as the "Theme hot-reloaded"
// notification. No LLM call is made, so the cassette is empty.
func TestTheme_HotReload(t *testing.T) {
	// The theme registry is process-global; restore the default so other
	// tests never see this test's theme. Registered before newTUI so that
	// (LIFO) it runs only after the program stopped rendering.
	t.Cleanup(func() { styles.ApplyThemeRef(styles.DefaultThemeRef) })
	d := newTUI(t, "testdata/basic.yaml", 120, 40)

	// isolateState (inside newTUI) redirected the data dir to a temp dir, so
	// this writes the user theme where the running TUI expects it.
	themesDir := styles.ThemesDir()
	require.NoError(t, os.MkdirAll(themesDir, 0o755))
	themePath := filepath.Join(themesDir, "hotreload.yaml")
	require.NoError(t, os.WriteFile(themePath, []byte("version: 1\nname: Hot Reload Before\n"), 0o644))

	// Switching themes re-targets the watcher onto the new theme's file.
	d.Send(messages.ChangeThemeMsg{ThemeRef: "user:hotreload"}).
		WaitFor(tuitest.Contains("Theme changed to Hot Reload Before"))

	// The watcher arms asynchronously (ThemeChangedMsg is sequenced after the
	// notification above) and debounces events for 500ms, so a single write
	// could slip in before fsnotify is attached. Re-edit the file on every
	// poll — like a user tweaking colors — until the reload lands. The 700ms
	// interval exceeds the debounce so rewrites cannot starve the timer.
	require.Eventually(t, func() bool {
		require.NoError(t, os.WriteFile(themePath, []byte("version: 1\nname: Hot Reload After\n"), 0o644))
		return strings.Contains(d.Frame(), "Theme hot-reloaded")
	}, 15*time.Second, 700*time.Millisecond, "editing the theme file should hot-reload it")

	require.Eventually(t, func() bool {
		return styles.CurrentTheme().Name == "Hot Reload After"
	}, 3*time.Second, 50*time.Millisecond, "styles.CurrentTheme() should pick up the edited theme")
}
