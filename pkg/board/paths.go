package board

import (
	"path/filepath"
	"strings"

	"github.com/docker/docker-agent/pkg/paths"
)

// The board's project list and card state live in files shared across
// environments with different home directories — e.g. a host and a docker
// sandbox that mirrors the host's home-relative mounts (~/src, ~/.cagent).
// Paths under home are therefore persisted as "~/…" and expanded against
// the current home on load, so the same state works in both.

// contractHome replaces a current-home prefix with "~", using forward
// slashes so the stored form is platform-neutral.
func contractHome(path string) string {
	home := paths.GetHomeDir()
	if home == "" || path == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rel, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + filepath.ToSlash(rel)
	}
	return path
}

// expandHome resolves a leading "~" against the current home directory.
// Other paths are returned unchanged.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		return filepath.Join(paths.GetHomeDir(), strings.TrimPrefix(path[1:], "/"))
	}
	return path
}
