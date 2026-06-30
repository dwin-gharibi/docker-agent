package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/paths"
	"github.com/docker/docker-agent/pkg/userid"
)

// TestMain points the config dir used by the default [userid.Get] at a
// throw-away temp dir so tests exercising gateway-bound requests never
// read or write the real user-uuid file. The override is set once and
// never mutated, so it stays safe for parallel tests. Tests needing a
// deterministic UUID inject their own resolver via [withCagentIDSource].
func TestMain(m *testing.M) {
	//nolint:forbidigo // TestMain has no *testing.T, so t.TempDir is unavailable.
	dir, err := os.MkdirTemp("", "httpclient-test-config-*")
	if err != nil {
		panic(err)
	}

	paths.SetConfigDir(dir)

	code := m.Run()

	paths.SetConfigDir("")
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		opts       []Opt
		wantHeader string
		wantValue  string
	}{
		{
			name:       "WithModel sets X-Cagent-Model",
			opts:       []Opt{WithModel("gpt-4o")},
			wantHeader: "X-Cagent-Model",
			wantValue:  "gpt-4o",
		},
		{
			name:       "WithModelName sets X-Cagent-Model-Name",
			opts:       []Opt{WithModelName("my-fast-model")},
			wantHeader: "X-Cagent-Model-Name",
			wantValue:  "my-fast-model",
		},
		{
			name:       "WithModelName skips header when empty",
			opts:       []Opt{WithModelName("")},
			wantHeader: "X-Cagent-Model-Name",
			wantValue:  "",
		},
		{
			name:       "WithProvider sets X-Cagent-Provider",
			opts:       []Opt{WithProvider("openai")},
			wantHeader: "X-Cagent-Provider",
			wantValue:  "openai",
		},
		{
			name:       "compression is disabled to support SSE streaming",
			wantHeader: "Accept-Encoding",
			wantValue:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			headers := doRequest(t, tt.opts...)

			if tt.wantValue != "" {
				assert.Equal(t, tt.wantValue, headers.Get(tt.wantHeader))
			} else {
				assert.Empty(t, headers.Get(tt.wantHeader))
			}
		})
	}
}

// doRequest creates an HTTP client with the given options, sends a GET request
// to a test server, and returns the headers the server received.
func doRequest(t *testing.T, opts ...Opt) http.Header {
	t.Helper()
	return doRequestWithCtx(t, t.Context(), opts...)
}

// doRequestWithCtx is like doRequest but uses the supplied context for
// the outbound request, so callers can exercise context-derived header
// injection (e.g. session ID propagation).
func doRequestWithCtx(t *testing.T, ctx context.Context, opts ...Opt) http.Header {
	t.Helper()

	var capturedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
	}))
	defer srv.Close()

	client := NewHTTPClient(ctx, opts...)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, http.NoBody)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	return capturedHeaders
}

func TestSessionIDHeader_GatewayBoundOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		ctxSessionID   string
		opts           []Opt
		wantHeaderSent bool
	}{
		{
			name:           "session ID present, gateway-bound (X-Cagent-Forward set) → header sent",
			ctxSessionID:   "sess-abc",
			opts:           []Opt{WithProxiedBaseURL("https://gateway.example/v1")},
			wantHeaderSent: true,
		},
		{
			name:           "session ID present, no X-Cagent-Forward → header skipped",
			ctxSessionID:   "sess-abc",
			opts:           nil,
			wantHeaderSent: false,
		},
		{
			name:           "no session ID on context, gateway-bound → header skipped",
			ctxSessionID:   "",
			opts:           []Opt{WithProxiedBaseURL("https://gateway.example/v1")},
			wantHeaderSent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			if tt.ctxSessionID != "" {
				ctx = ContextWithSessionID(ctx, tt.ctxSessionID)
			}
			headers := doRequestWithCtx(t, ctx, tt.opts...)

			if tt.wantHeaderSent {
				assert.Equal(t, tt.ctxSessionID, headers.Get("X-Cagent-Session-Id"))
			} else {
				assert.Empty(t, headers.Get("X-Cagent-Session-Id"))
			}
		})
	}
}

func TestContextWithSessionID_RoundTrip(t *testing.T) {
	t.Parallel()

	assert.Empty(t, SessionIDFromContext(t.Context()), "empty context yields empty session ID")
	ctx := ContextWithSessionID(t.Context(), "sess-xyz")
	assert.Equal(t, "sess-xyz", SessionIDFromContext(ctx))
}

func TestCagentIDHeader_GatewayBoundOnly(t *testing.T) {
	t.Parallel()

	// Seed a fixed UUID into an isolated resolver so the value is
	// deterministic and the test touches neither the real config dir
	// nor any global state — letting it run in parallel.
	const stored = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "user-uuid"), []byte(stored), 0o600))
	idSource := userid.New(dir).Get
	require.Equal(t, stored, idSource(), "seeded resolver must return the stored UUID")

	tests := []struct {
		name           string
		opts           []Opt
		wantHeaderSent bool
	}{
		{
			name:           "gateway-bound (X-Cagent-Forward set) → X-Cagent-Id sent",
			opts:           []Opt{WithProxiedBaseURL("https://gateway.example/v1")},
			wantHeaderSent: true,
		},
		{
			name:           "no X-Cagent-Forward → X-Cagent-Id skipped",
			opts:           nil,
			wantHeaderSent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := append([]Opt{withCagentIDSource(idSource)}, tt.opts...)
			headers := doRequest(t, opts...)

			if tt.wantHeaderSent {
				assert.Equal(t, stored, headers.Get("X-Cagent-Id"))
			} else {
				assert.Empty(t, headers.Get("X-Cagent-Id"))
			}
		})
	}
}
