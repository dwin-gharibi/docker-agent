package base

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/httpclient"
)

type fakeEnv map[string]string

func (f fakeEnv) Get(_ context.Context, name string) (string, bool) {
	v, ok := f[name]
	return v, ok
}

func TestVerifyDockerGatewayAuth(t *testing.T) {
	t.Parallel()

	t.Run("non-docker gateway needs no token", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, VerifyDockerGatewayAuth(t.Context(), fakeEnv{}, "https://gateway.example.com"))
	})

	t.Run("trusted docker gateway with token", func(t *testing.T) {
		t.Parallel()
		env := fakeEnv{environment.DockerDesktopTokenEnv: "jwt"}
		assert.NoError(t, VerifyDockerGatewayAuth(t.Context(), env, "https://api.docker.com/models"))
	})

	t.Run("trusted docker gateway without token", func(t *testing.T) {
		t.Parallel()
		err := VerifyDockerGatewayAuth(t.Context(), fakeEnv{}, "https://api.docker.com/models")
		assert.EqualError(t, err, "sorry, you first need to sign in Docker Desktop to use the Docker AI Gateway")
	})
}

func TestGatewayAuthToken(t *testing.T) {
	t.Parallel()

	t.Run("non-docker gateway returns empty token", func(t *testing.T) {
		t.Parallel()
		token, err := GatewayAuthToken(t.Context(), fakeEnv{}, "https://gateway.example.com")
		require.NoError(t, err)
		assert.Empty(t, token)
	})

	t.Run("trusted docker gateway returns fresh token", func(t *testing.T) {
		t.Parallel()
		env := fakeEnv{environment.DockerDesktopTokenEnv: "jwt"}
		token, err := GatewayAuthToken(t.Context(), env, "https://api.docker.com/models")
		require.NoError(t, err)
		assert.Equal(t, "jwt", token)
	})

	t.Run("trusted docker gateway without token", func(t *testing.T) {
		t.Parallel()
		_, err := GatewayAuthToken(t.Context(), fakeEnv{}, "https://api.docker.com/models")
		assert.EqualError(t, err, NoDesktopTokenErrorMessage)
	})
}

func TestGatewayHTTPOptions(t *testing.T) {
	t.Parallel()

	apply := func(opts []httpclient.Opt) *httpclient.HTTPOptions {
		o := &httpclient.HTTPOptions{Header: make(http.Header)}
		for _, opt := range opts {
			opt(o)
		}
		return o
	}

	gatewayURL, err := url.Parse("https://api.docker.com/models?tier=pro")
	require.NoError(t, err)

	t.Run("default base URL and identity headers", func(t *testing.T) {
		t.Parallel()
		cfg := &latest.ModelConfig{Provider: "openai", Model: "gpt-4o", Name: "smart"}
		o := apply(GatewayHTTPOptions(gatewayURL, "https://api.openai.com/v1", cfg, false))
		assert.Equal(t, "https://api.openai.com/v1", o.Header.Get("X-Cagent-Forward"))
		assert.Equal(t, "openai", o.Header.Get("X-Cagent-Provider"))
		assert.Equal(t, "gpt-4o", o.Header.Get("X-Cagent-Model"))
		assert.Equal(t, "smart", o.Header.Get("X-Cagent-Model-Name"))
		assert.Equal(t, "pro", o.Query.Get("tier"))
		assert.Empty(t, o.Header.Get("X-Cagent-GeneratingTitle"))
	})

	t.Run("model base_url overrides the default", func(t *testing.T) {
		t.Parallel()
		cfg := &latest.ModelConfig{Provider: "openai", Model: "gpt-4o", BaseURL: "https://example.com/v1"}
		o := apply(GatewayHTTPOptions(gatewayURL, "https://api.openai.com/v1", cfg, false))
		assert.Equal(t, "https://example.com/v1", o.Header.Get("X-Cagent-Forward"))
	})

	t.Run("title generation marker", func(t *testing.T) {
		t.Parallel()
		cfg := &latest.ModelConfig{Provider: "openai", Model: "gpt-4o"}
		o := apply(GatewayHTTPOptions(gatewayURL, "https://api.openai.com/v1", cfg, true))
		assert.Equal(t, "1", o.Header.Get("X-Cagent-GeneratingTitle"))
	})
}
