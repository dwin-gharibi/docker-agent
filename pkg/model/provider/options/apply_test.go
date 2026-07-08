package options

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApply(t *testing.T) {
	t.Parallel()

	t.Run("empty yields zero options", func(t *testing.T) {
		t.Parallel()
		m := Apply()
		assert.Empty(t, m.Gateway())
		assert.False(t, m.GeneratingTitle())
	})

	t.Run("applies opts in order", func(t *testing.T) {
		t.Parallel()
		m := Apply(WithGateway("first"), WithGateway("second"), WithMaxTokens(42))
		assert.Equal(t, "second", m.Gateway())
		assert.Equal(t, int64(42), m.MaxTokens())
	})

	t.Run("skips nil opts", func(t *testing.T) {
		t.Parallel()
		m := Apply(nil, WithGateway("gw"))
		assert.Equal(t, "gw", m.Gateway())
	})
}

type staticTransport struct{ base http.RoundTripper }

func (s *staticTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return s.base.RoundTrip(req)
}

func TestWrapTransport(t *testing.T) {
	t.Parallel()

	t.Run("no wrapper keeps transport", func(t *testing.T) {
		t.Parallel()
		client := &http.Client{Transport: http.DefaultTransport}
		m := Apply()
		m.WrapTransport(t.Context(), client)
		assert.Same(t, http.DefaultTransport, client.Transport)
	})

	t.Run("wrapper replaces transport and receives the original", func(t *testing.T) {
		t.Parallel()
		client := &http.Client{Transport: http.DefaultTransport}
		m := Apply(WithHTTPTransportWrapper(func(base http.RoundTripper) http.RoundTripper {
			return &staticTransport{base: base}
		}))
		m.WrapTransport(t.Context(), client)
		wrapped, ok := client.Transport.(*staticTransport)
		require.True(t, ok)
		assert.Same(t, http.DefaultTransport, wrapped.base)
	})

	t.Run("nil-returning wrapper keeps original transport", func(t *testing.T) {
		t.Parallel()
		client := &http.Client{Transport: http.DefaultTransport}
		m := Apply(WithHTTPTransportWrapper(func(http.RoundTripper) http.RoundTripper { return nil }))
		m.WrapTransport(t.Context(), client)
		assert.Same(t, http.DefaultTransport, client.Transport)
	})

	t.Run("nil client is a no-op", func(t *testing.T) {
		t.Parallel()
		m := Apply(WithHTTPTransportWrapper(func(base http.RoundTripper) http.RoundTripper { return base }))
		assert.NotPanics(t, func() { m.WrapTransport(t.Context(), nil) })
	})
}
