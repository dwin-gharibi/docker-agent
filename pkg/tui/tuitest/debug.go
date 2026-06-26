package tuitest

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// liveFrames streams each captured frame to stderr. Use with `go test -v` for
// an approximate live view of what the harness sees.
var liveFrames = flag.Bool("tuitest.live", false, "stream captured TUI frames to stderr while tests run")

// dumpFrames writes every captured frame to testdata/frames/<test-name>/NNNN.txt.
// It is useful when a WaitFor fails in CI: upload the directory as an artifact
// and inspect the frame sequence after the fact.
var dumpFrames = flag.Bool("tuitest.frames", false, "dump captured TUI frames to testdata/frames/<test-name>/NNNN.txt")

// liveWriter is overridden by unit tests.
var liveWriter io.Writer = os.Stderr

var liveMu sync.Mutex

// frameSink receives captured frames for optional debugging side effects.
type frameSink interface {
	frame(frame string)
	err() error
}

type debugSink struct {
	mu       sync.Mutex
	live     bool
	dumpDir  string
	index    int
	firstErr error
}

func newDebugSink(tb testing.TB) frameSink {
	tb.Helper()

	if !*liveFrames && !*dumpFrames {
		return nil
	}

	s := &debugSink{live: *liveFrames}
	if *dumpFrames {
		s.dumpDir = filepath.Join(goldenDir, "frames", safeTestName(tb.Name()))
		if err := os.RemoveAll(s.dumpDir); err != nil {
			tb.Fatalf("tuitest: clearing frame dump dir %s: %v", s.dumpDir, err)
		}
		if err := os.MkdirAll(s.dumpDir, 0o750); err != nil {
			tb.Fatalf("tuitest: creating frame dump dir %s: %v", s.dumpDir, err)
		}
		tb.Logf("tuitest: dumping frames to %s", s.dumpDir)
	}
	return s
}

func (s *debugSink) frame(frame string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.firstErr != nil {
		return
	}

	s.index++
	if s.live {
		// Clear screen + home cursor, then render the current frame. This keeps
		// the output readable in an attached terminal while still being plain text
		// enough to inspect in `go test -v` logs.
		liveMu.Lock()
		_, err := fmt.Fprintf(liveWriter, "\x1b[2J\x1b[H--- tuitest frame %04d ---\n%s\n", s.index, frame)
		liveMu.Unlock()
		if err != nil {
			s.firstErr = err
			return
		}
	}
	if s.dumpDir != "" {
		path := filepath.Join(s.dumpDir, fmt.Sprintf("%04d.txt", s.index))
		if err := os.WriteFile(path, []byte(frame), 0o600); err != nil {
			s.firstErr = err
		}
	}
}

func (s *debugSink) err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.firstErr
}

func safeTestName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}
