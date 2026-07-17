package tui_test

import (
	"testing"
	"time"

	"github.com/docker/docker-agent/pkg/tui/tuitest"
)

// TestChat_DegenerateResizeDoesNotPanic drives the full TUI through
// degenerate terminal sizes (0x0, 1x1, …) that real terminals and tmux can
// transiently report. Rendering at those sizes used to panic (negative
// strings.Repeat count in the resize handle); the TUI must survive and
// recover once a sane size returns.
func TestChat_DegenerateResizeDoesNotPanic(t *testing.T) {
	d := newTUI(t, "testdata/basic.yaml", 120, 40)

	d.WaitFor(tuitest.Contains("Type your message here"))

	// Submit a prompt first so the busy chrome (working indicator, resize
	// handle suffix) can render during the sweep when replay is still
	// streaming; the sizes themselves are what must never panic.
	d.Type("What's 2+2?").Enter()
	for _, size := range [][2]int{{0, 0}, {1, 1}, {2, 2}, {1, 40}, {120, 1}, {3, 3}} {
		d.Resize(size[0], size[1])
		d.WaitForStable(50 * time.Millisecond)
	}

	d.Resize(120, 40)
	d.WaitFor(tuitest.Contains("2 + 2 equals 4."))
}
