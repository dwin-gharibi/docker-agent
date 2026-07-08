// Package tour persists the state of the interactive getting-started tour
// across runs. The state lives next to the first-run marker in the user's
// config directory, separate from session data.
package tour

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker-agent/pkg/paths"
)

// Status is the persisted outcome of the first-run tour offer.
type Status string

const (
	// StatusUnanswered means the user has neither started the tour nor
	// declined it permanently: the first-run offer may be shown.
	StatusUnanswered Status = ""
	// StatusDone means the tour was started at least once.
	StatusDone Status = "done"
	// StatusNever means the user asked to never be offered the tour again.
	StatusNever Status = "never"
)

// noTourEnvVars suppress the first-run tour offer in scripted environments,
// following the DOCKER_AGENT_HIDE_TELEMETRY_BANNER precedent.
var noTourEnvVars = []string{"DOCKER_AGENT_NO_TOUR", "CAGENT_NO_TOUR"}

func statePath() string {
	return filepath.Join(paths.GetConfigDir(), ".cagent_tour")
}

// ReadStatus returns the persisted tour status. A missing file, a read error,
// or unknown content all read as StatusUnanswered.
func ReadStatus() Status {
	data, err := os.ReadFile(statePath())
	if err != nil {
		return StatusUnanswered
	}
	switch status := Status(strings.TrimSpace(string(data))); status {
	case StatusDone, StatusNever:
		return status
	default:
		return StatusUnanswered
	}
}

func writeStatus(status Status) error {
	if err := os.MkdirAll(paths.GetConfigDir(), 0o700); err != nil {
		return err
	}
	//nolint:gosec // marker file with no sensitive content
	return os.WriteFile(statePath(), []byte(string(status)+"\n"), 0o644)
}

// MarkDone records that the tour was started, so the first-run offer is not
// shown again.
func MarkDone() error { return writeStatus(StatusDone) }

// MarkNever records that the user never wants to see the tour offer again.
func MarkNever() error { return writeStatus(StatusNever) }

// DisabledByEnv reports whether the tour offer is suppressed via environment
// variable (DOCKER_AGENT_NO_TOUR=1 or CAGENT_NO_TOUR=1). An explicit request
// to run the tour is still honored.
func DisabledByEnv(getenv func(string) string) bool {
	for _, name := range noTourEnvVars {
		if getenv(name) == "1" {
			return true
		}
	}
	return false
}

// ShouldOffer reports whether the first-run tour offer should be shown: the
// user has neither taken the tour nor opted out, and no environment variable
// suppresses it.
func ShouldOffer(getenv func(string) string) bool {
	return !DisabledByEnv(getenv) && ReadStatus() == StatusUnanswered
}
