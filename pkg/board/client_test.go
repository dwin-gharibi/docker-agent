package board

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveUnix runs an HTTP handler on a unix socket and returns the socket path.
func serveUnix(t *testing.T, handler http.Handler) string {
	t.Helper()
	// Not t.TempDir(): its per-test path is long enough to overflow the
	// ~104-byte unix sun_path limit under long test names.
	dir, err := os.MkdirTemp("", "board-client") //nolint:forbidigo,usetesting // see above
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socket := filepath.Join(dir, "cp.sock")
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "unix", socket)
	require.NoError(t, err)
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return socket
}

// Heartbeat comments are invisible to the event callback, and once one has
// been seen, a stream that goes silent is aborted with an error (instead of
// blocking forever on a hung transport) so the watcher reconnects.
func TestStreamEventsIdleWatchdogAbortsSilentStream(t *testing.T) {
	old := streamIdleTimeout
	streamIdleTimeout = 50 * time.Millisecond
	t.Cleanup(func() { streamIdleTimeout = old })

	socket := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := w.(http.Flusher)
		fmt.Fprint(w, ": ping\n\n")
		fmt.Fprint(w, "data: {\"type\":\"stream_started\"}\n\n")
		f.Flush()
		// Then hang without closing, like a wedged transport.
		<-r.Context().Done()
	}))

	c := newClient(socket, "sess-1")
	var got []event
	err := c.StreamEvents(t.Context(), 0, func(ev event) bool {
		got = append(got, ev)
		return true
	})
	require.ErrorIs(t, err, errStreamIdle)
	require.Len(t, got, 1, "heartbeat comments must not reach the callback")
	assert.Equal(t, eventStreamStarted, got[0].Type)
}

func TestStreamEventsHeartbeatLinesResetWatchdog(t *testing.T) {
	old := streamIdleTimeout
	streamIdleTimeout = 50 * time.Millisecond
	t.Cleanup(func() { streamIdleTimeout = old })

	socket := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := w.(http.Flusher)
		for range 4 {
			fmt.Fprint(w, ": ping\n")
			f.Flush()
			time.Sleep(20 * time.Millisecond) //nolint:forbidigo // heartbeat cadence is under test
		}
		fmt.Fprint(w, "data: {\"type\":\"stream_stopped\"}\n\n")
		f.Flush()
		<-r.Context().Done()
	}))

	c := newClient(socket, "sess-1")
	err := c.StreamEvents(t.Context(), 0, func(ev event) bool {
		assert.Equal(t, eventStreamStopped, ev.Type)
		return false
	})
	require.NoError(t, err)
}

// Without any heartbeat from the server (an older docker-agent), the watchdog
// stays unarmed: a quiet stream is left alone and events keep flowing.
func TestStreamEventsNoHeartbeatNoWatchdog(t *testing.T) {
	old := streamIdleTimeout
	streamIdleTimeout = 50 * time.Millisecond
	t.Cleanup(func() { streamIdleTimeout = old })

	socket := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"type\":\"stream_started\"}\n\n")
		f.Flush()
		time.Sleep(120 * time.Millisecond) //nolint:forbidigo // real quiet time is the thing under test
		fmt.Fprint(w, "data: {\"type\":\"stream_stopped\"}\n\n")
	}))

	c := newClient(socket, "sess-1")
	var got []event
	err := c.StreamEvents(t.Context(), 0, func(ev event) bool {
		got = append(got, ev)
		return len(got) < 2
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
}
