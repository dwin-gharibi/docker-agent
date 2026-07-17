//go:build !windows

package image

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupportsKittyGraphics(t *testing.T) {
	t.Parallel()

	t.Run("non-terminal", func(t *testing.T) {
		file, err := os.Open(os.DevNull)
		require.NoError(t, err)
		defer file.Close()

		start := time.Now()
		assert.False(t, SupportsKittyGraphics(file, file))
		assert.Less(t, time.Since(start), kittyProbeTimeout)
	})

	t.Run("supported", func(t *testing.T) {
		ptmx, tty, err := pty.Open()
		require.NoError(t, err)
		defer ptmx.Close()
		defer tty.Close()

		go answerKittyProbe(ptmx, kittyProbeOK)
		assert.True(t, SupportsKittyGraphics(tty, tty))
	})

	t.Run("unsupported", func(t *testing.T) {
		ptmx, tty, err := pty.Open()
		require.NoError(t, err)
		defer ptmx.Close()
		defer tty.Close()

		go answerKittyProbe(ptmx, "\x1b_Gi="+kittyProbeID+";ENOTSUP\x1b\\")
		assert.False(t, SupportsKittyGraphics(tty, tty))
	})
}

func answerKittyProbe(terminal *os.File, response string) {
	var seen []byte
	buf := make([]byte, 128)
	for {
		n, err := terminal.Read(buf)
		if err != nil {
			return
		}
		seen = append(seen, buf[:n]...)
		if bytes.Contains(seen, []byte(kittyProbeQuery)) {
			_, _ = terminal.WriteString(response)
			return
		}
	}
}
