package tui

import (
	"image/color"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/board"
	"github.com/docker/docker-agent/pkg/tui/styles"
)

// TestStatusColor pins the status→color contract shared with the web board:
// starting/loading/attaching=blue, running=orange, waiting=green,
// paused=white, error=red.
func TestStatusColor(t *testing.T) {
	// Theme colors are only bound after ApplyTheme; not parallel because it
	// mutates package-level style state.
	styles.ApplyThemeRef(styles.DefaultThemeRef)

	tests := []struct {
		status board.CardStatus
		want   color.Color
	}{
		{board.StatusStarting, styles.Info},
		{board.StatusLoading, styles.Info},
		{board.StatusAttaching, styles.Info},
		{board.StatusRunning, styles.Warning},
		{board.StatusPaused, styles.White},
		{board.StatusError, styles.Error},
		{board.StatusWaiting, styles.Success},
		{board.CardStatus("unknown"), styles.Success}, // falls back to waiting
	}
	for _, test := range tests {
		assert.Equal(t, test.want, statusColor(test.status), "status %q", test.status)
	}
}
