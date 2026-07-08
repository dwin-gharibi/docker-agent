package genai

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/docker/docker-agent/pkg/chat"
)

func TestProviderNameForConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"openai", ProviderOpenAI},
		{"openai_chatcompletions", ProviderOpenAI},
		{"openai_responses", ProviderOpenAI},
		{"anthropic", ProviderAnthropic},
		{"amazon-bedrock", ProviderAWSBedrock},
		{"google", ProviderGCPGenAI},
		{"vertexai", ProviderGCPVertexAI},
		{"azure", ProviderAzureAI},
		{"dmr", ProviderDMR},
		{"custom-provider", "custom-provider"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ProviderNameForConfig(tt.in))
		})
	}
}

func TestClassifyError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"context canceled", context.Canceled, "context_canceled"},
		{"context deadline", context.DeadlineExceeded, "deadline_exceeded"},
		{"rate limit", errors.New("HTTP 429 Too Many Requests"), "rate_limit"},
		{"context length", errors.New("context_length_exceeded: prompt too large"), "context_length_exceeded"},
		{"unauthorized", errors.New("HTTP 401 Unauthorized"), "auth"},
		{"forbidden", errors.New("HTTP 403 Forbidden"), "forbidden"},
		{"content policy", errors.New("response blocked by content filter"), "content_policy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ClassifyError(tt.err))
		})
	}
}

// fakeStream produces a fixed sequence of chunks then EOF.
type fakeStream struct {
	chunks []chat.MessageStreamResponse
	idx    int
	closed bool
}

func (f *fakeStream) Recv() (chat.MessageStreamResponse, error) {
	if f.idx >= len(f.chunks) {
		return chat.MessageStreamResponse{}, io.EOF
	}
	r := f.chunks[f.idx]
	f.idx++
	return r, nil
}

func (f *fakeStream) Close() { f.closed = true }

func TestStartChatAndWrapStream(t *testing.T) {
	t.Parallel()

	stream := &fakeStream{
		chunks: []chat.MessageStreamResponse{
			{
				ID:    "resp-1",
				Model: "claude-sonnet-4",
				Choices: []chat.MessageStreamChoice{
					{Delta: chat.MessageDelta{Content: "hello"}},
				},
			},
			{
				Choices: []chat.MessageStreamChoice{
					{FinishReason: chat.FinishReasonStop},
				},
				Usage: &chat.Usage{
					InputTokens:       100,
					OutputTokens:      50,
					CachedInputTokens: 20,
					CacheWriteTokens:  10,
				},
			},
		},
	}

	ctx, span := StartChat(t.Context(), ChatRequest{
		Provider:  ProviderAnthropic,
		Model:     "claude-sonnet-4",
		Stream:    true,
		MaxTokens: 4096,
	})
	require.NotNil(t, span)
	require.NotNil(t, ctx)

	wrapped := WrapStream(span, stream)

	// Drain the stream.
	for {
		resp, err := wrapped.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		_ = resp
	}
	wrapped.Close()
	assert.True(t, stream.closed)

	// Re-closing should be a no-op (the wrapper guards against
	// double-Close, which would otherwise emit two End() calls).
	wrapped.Close()
}

func TestWrapStreamNilSpanReturnsOriginal(t *testing.T) {
	t.Parallel()
	s := &fakeStream{}
	got := WrapStream(nil, s)
	assert.Same(t, s, got)
}

// installRecordingTracer replaces the global OTel tracer with an in-memory
// SDK tracer for the duration of the test and returns the span exporter so
// callers can inspect recorded spans.
func installRecordingTracer(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(t.Context())
		otel.SetTracerProvider(prev)
	})
	return exp
}

// errorStream yields an optional finish-reason chunk then returns a
// transport error on the next Recv. When finishWithError is true it returns
// both the finish reason and the error in the same Recv call.
type errorStream struct {
	finishFirst     bool
	finishWithError bool
	sent            bool
	err             error
}

func (s *errorStream) Recv() (chat.MessageStreamResponse, error) {
	if s.finishWithError && !s.sent {
		s.sent = true
		return chat.MessageStreamResponse{
			Choices: []chat.MessageStreamChoice{
				{FinishReason: chat.FinishReasonStop},
			},
		}, s.err
	}
	if s.finishFirst && !s.sent {
		s.sent = true
		return chat.MessageStreamResponse{
			Choices: []chat.MessageStreamChoice{
				{FinishReason: chat.FinishReasonStop},
			},
		}, nil
	}
	return chat.MessageStreamResponse{}, s.err
}

func (s *errorStream) Close() {}

// TestWrapStream_ErrorBeforeFinishReason verifies that a transport error
// arriving before any finish reason is recorded on the span as an error.
// Mutates the global OTel tracer provider; cannot run in parallel.
func TestWrapStream_ErrorBeforeFinishReason(t *testing.T) {
	exp := installRecordingTracer(t)

	_, span := StartChat(t.Context(), ChatRequest{
		Provider: ProviderAnthropic,
		Model:    "claude-haiku-4-5",
		Stream:   true,
	})
	require.NotNil(t, span)

	stream := &errorStream{finishFirst: false, err: errors.New("http2: response body closed")}
	wrapped := WrapStream(span, stream)

	_, err := wrapped.Recv()
	require.Error(t, err)
	wrapped.Close()

	spans := exp.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, sdktrace.Status{Code: codes.Error, Description: "http2: response body closed"}, spans[0].Status)
}

// TestWrapStream_ErrorAfterFinishReason verifies that a transport error
// arriving after a terminal finish reason is NOT recorded on the span —
// it is benign teardown from the consumer closing the body early.
// Mutates the global OTel tracer provider; cannot run in parallel.
func TestWrapStream_ErrorAfterFinishReason(t *testing.T) {
	exp := installRecordingTracer(t)

	_, span := StartChat(t.Context(), ChatRequest{
		Provider: ProviderAnthropic,
		Model:    "claude-haiku-4-5",
		Stream:   true,
	})
	require.NotNil(t, span)

	stream := &errorStream{finishFirst: true, err: errors.New("http2: response body closed")}
	wrapped := WrapStream(span, stream)

	// First Recv delivers finish_reason=stop.
	_, err := wrapped.Recv()
	require.NoError(t, err)

	// Second Recv returns the transport error — should be suppressed.
	_, err = wrapped.Recv()
	require.Error(t, err)
	wrapped.Close()

	spans := exp.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, sdktrace.Status{Code: codes.Unset}, spans[0].Status,
		"transport error after finish_reason must not mark the span as failed")
}

// TestWrapStream_FinishReasonAndErrorSameRecv verifies the edge case where a
// provider returns both a terminal finish reason and a transport error in the
// same Recv call — the span should still be treated as successful.
// Mutates the global OTel tracer provider; cannot run in parallel.
func TestWrapStream_FinishReasonAndErrorSameRecv(t *testing.T) {
	exp := installRecordingTracer(t)

	_, span := StartChat(t.Context(), ChatRequest{
		Provider: ProviderAnthropic,
		Model:    "claude-haiku-4-5",
		Stream:   true,
	})
	require.NotNil(t, span)

	stream := &errorStream{finishWithError: true, err: errors.New("http2: response body closed")}
	wrapped := WrapStream(span, stream)

	_, err := wrapped.Recv()
	require.Error(t, err)
	wrapped.Close()

	spans := exp.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, sdktrace.Status{Code: codes.Unset}, spans[0].Status,
		"finish_reason and transport error in same Recv must not mark the span as failed")
}

func TestServerAddressFromURL(t *testing.T) {
	t.Parallel()
	host, port := ServerAddressFromURL("https://api.anthropic.com:443/v1/messages")
	assert.Equal(t, "api.anthropic.com", host)
	assert.Equal(t, 443, port)

	host, port = ServerAddressFromURL("https://api.openai.com/v1/chat/completions")
	assert.Equal(t, "api.openai.com", host)
	assert.Equal(t, 0, port)

	host, port = ServerAddressFromURL("")
	assert.Empty(t, host)
	assert.Equal(t, 0, port)
}
