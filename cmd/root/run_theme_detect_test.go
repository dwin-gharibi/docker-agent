//go:build !windows

package root

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTerminal answers the OSC 11 background query on the master side of a
// pty pair with the given color response, followed by a DA1 answer (the
// fallback query lipgloss uses to terminate the read early). An empty
// response simulates a terminal that never answers.
func fakeTerminal(t *testing.T, ptmx *os.File, response string) {
	t.Helper()
	go func() {
		var seen []byte
		buf := make([]byte, 256)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				return
			}
			seen = append(seen, buf[:n]...)
			if response != "" && bytes.Contains(seen, []byte("]11;?")) {
				_, _ = ptmx.WriteString(response)
				return
			}
		}
	}()
}

// TestTerminalHasDarkBackground exercises the real OSC 11 round-trip through
// a pty pair: raw-mode query on the slave side, fake terminal answering on
// the master side.
func TestTerminalHasDarkBackground(t *testing.T) {
	t.Parallel()

	t.Run("non-tty falls back to dark without querying", func(t *testing.T) {
		t.Parallel()
		devNull, err := os.Open(os.DevNull)
		require.NoError(t, err)
		defer devNull.Close()

		start := time.Now()
		assert.True(t, terminalHasDarkBackground(devNull, devNull))
		assert.Less(t, time.Since(start), time.Second, "non-tty detection must not wait on a query")
	})

	t.Run("light background answer is detected", func(t *testing.T) {
		t.Parallel()
		ptmx, tty, err := pty.Open()
		require.NoError(t, err)
		defer ptmx.Close()
		defer tty.Close()

		fakeTerminal(t, ptmx, "\x1b]11;rgb:fafa/fafa/fafa\x1b\\\x1b[?62;22c")
		assert.False(t, terminalHasDarkBackground(tty, tty), "near-white background must be detected as light")
	})

	t.Run("dark background answer is detected", func(t *testing.T) {
		t.Parallel()
		ptmx, tty, err := pty.Open()
		require.NoError(t, err)
		defer ptmx.Close()
		defer tty.Close()

		fakeTerminal(t, ptmx, "\x1b]11;rgb:1c1c/1c1c/2222\x1b\\\x1b[?62;22c")
		assert.True(t, terminalHasDarkBackground(tty, tty))
	})

	t.Run("terminal that never answers falls back to dark without hanging", func(t *testing.T) {
		t.Parallel()
		ptmx, tty, err := pty.Open()
		require.NoError(t, err)
		defer ptmx.Close()
		defer tty.Close()

		fakeTerminal(t, ptmx, "")

		start := time.Now()
		assert.True(t, terminalHasDarkBackground(tty, tty))
		assert.Less(t, time.Since(start), 10*time.Second, "detection must be bounded by the query timeout")
	})
}
