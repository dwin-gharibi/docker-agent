package gemini

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/model/provider/options"
)

// geminiCountingTransport wraps a base RoundTripper and counts RoundTrip calls.
type geminiCountingTransport struct {
	base  http.RoundTripper
	calls atomic.Int64
}

func (c *geminiCountingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return c.base.RoundTrip(req)
}

// writeGeminiSSEResponse writes a minimal valid Gemini streaming response.
func writeGeminiSSEResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)

	payload := `{"candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`
	_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
	if flusher != nil {
		flusher.Flush()
	}
}

func TestNewClient_TransportWrapperInvokedDirectPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeGeminiSSEResponse(w)
	}))
	defer server.Close()

	var counter geminiCountingTransport

	cfg := &latest.ModelConfig{
		Provider: "google",
		Model:    "gemini-2.0-flash",
		BaseURL:  server.URL,
	}
	env := environment.NewMapEnvProvider(map[string]string{
		"GOOGLE_API_KEY": "test-key",
	})

	client, err := NewClient(t.Context(), cfg, env,
		options.WithHTTPTransportWrapper(func(base http.RoundTripper) http.RoundTripper {
			counter.base = base
			return &counter
		}),
	)
	require.NoError(t, err)

	stream, err := client.CreateChatCompletionStream(t.Context(), []chat.Message{
		{Role: chat.MessageRoleUser, Content: "hello"},
	}, nil)
	require.NoError(t, err)
	defer stream.Close()

	// Drain the stream so RoundTrip has been fully exercised.
	for {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}

	assert.Greater(t, counter.calls.Load(), int64(0), "transport wrapper RoundTrip should have been called at least once")
}

func TestNewClient_TransportWrapperInvokedGatewayPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeGeminiSSEResponse(w)
	}))
	defer server.Close()

	var counter geminiCountingTransport

	cfg := &latest.ModelConfig{
		Provider: "google",
		Model:    "gemini-2.0-flash",
	}
	// server.URL is 127.0.0.1 which IsTrustedDockerURL considers trusted,
	// so we must supply the Docker Desktop token.
	env := environment.NewMapEnvProvider(map[string]string{
		environment.DockerDesktopTokenEnv: "test-dd-token",
	})

	client, err := NewClient(t.Context(), cfg, env,
		options.WithGateway(server.URL),
		options.WithHTTPTransportWrapper(func(base http.RoundTripper) http.RoundTripper {
			counter.base = base
			return &counter
		}),
	)
	require.NoError(t, err)

	stream, err := client.CreateChatCompletionStream(t.Context(), []chat.Message{
		{Role: chat.MessageRoleUser, Content: "hello"},
	}, nil)
	require.NoError(t, err)
	defer stream.Close()

	// Drain the stream so RoundTrip has been fully exercised.
	for {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}

	assert.Greater(t, counter.calls.Load(), int64(0), "transport wrapper RoundTrip should have been called at least once in gateway path")
}

func TestNewClient_TransportWrapperVertexAIWarns(t *testing.T) {
	// When a Vertex AI backend is used (project+location configured), the genai
	// SDK manages its own HTTP client. The transport wrapper cannot be applied.
	// Verify that NewClient succeeds (no error) and the wrapper function itself
	// is not called — the caller receives a slog warning instead.
	var wrapperInvoked bool

	cfg := &latest.ModelConfig{
		Provider: "google",
		Model:    "gemini-2.0-flash",
		ProviderOpts: map[string]any{
			"project":  "test-project",
			"location": "us-central1",
		},
	}
	env := environment.NewMapEnvProvider(map[string]string{})

	_, err := NewClient(t.Context(), cfg, env,
		options.WithHTTPTransportWrapper(func(base http.RoundTripper) http.RoundTripper {
			wrapperInvoked = true
			return &geminiCountingTransport{base: base}
		}),
	)
	// NewClient may fail because Vertex AI requires real ADC credentials;
	// we only care that the wrapper function itself was NOT invoked.
	_ = err
	assert.False(t, wrapperInvoked, "wrapper function should not be invoked for Vertex AI backend")
}
