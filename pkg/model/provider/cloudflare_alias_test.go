package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
)

// TestCloudflareAlias_ResolvesTemplatedBaseURL proves the account/gateway-scoped
// Cloudflare aliases resolve their ${...}-templated base URL against the
// environment before the provider is built, and inherit CLOUDFLARE_API_TOKEN as
// the token key. This is the mechanism that makes a non-static alias work
// (issues #3354/#3355), reusing the ${env.X} expansion path from #2261.
func TestCloudflareAlias_ResolvesTemplatedBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		provider    string
		env         map[string]string
		wantBaseURL string
	}{
		{
			name:        "workers-ai interpolates account id",
			provider:    "cloudflare-workers-ai",
			env:         map[string]string{"CLOUDFLARE_ACCOUNT_ID": "acc-123"},
			wantBaseURL: "https://api.cloudflare.com/client/v4/accounts/acc-123/ai/v1",
		},
		{
			name:     "ai-gateway interpolates account and gateway ids",
			provider: "cloudflare-ai-gateway",
			env: map[string]string{
				"CLOUDFLARE_ACCOUNT_ID": "acc-123",
				"CLOUDFLARE_GATEWAY_ID": "gw-9",
			},
			wantBaseURL: "https://gateway.ai.cloudflare.com/v1/acc-123/gw-9/compat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got *latest.ModelConfig
			r := NewRegistry(map[string]providerFactory{"openai": captureFactory(&got)})

			cfg := &latest.ModelConfig{Provider: tt.provider, Model: "some-model"}
			_, err := r.createDirectProvider(t.Context(), cfg, environment.NewMapEnvProvider(tt.env))
			require.NoError(t, err)
			require.NotNil(t, got)

			assert.Equal(t, tt.wantBaseURL, got.BaseURL, "templated base URL must be resolved from the environment")
			assert.Equal(t, "CLOUDFLARE_API_TOKEN", got.TokenKey, "token key must come from the alias")
		})
	}
}

// TestCloudflareAlias_MissingEnvVarErrors verifies an unset account/gateway id
// surfaces as a clear provider-creation error instead of dialing a malformed
// URL, so a misconfigured Cloudflare provider fails loudly and actionably.
func TestCloudflareAlias_MissingEnvVarErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		env      map[string]string
		wantVar  string
	}{
		{
			name:     "workers-ai without account id",
			provider: "cloudflare-workers-ai",
			env:      map[string]string{},
			wantVar:  "CLOUDFLARE_ACCOUNT_ID",
		},
		{
			name:     "ai-gateway without gateway id",
			provider: "cloudflare-ai-gateway",
			env:      map[string]string{"CLOUDFLARE_ACCOUNT_ID": "acc-123"},
			wantVar:  "CLOUDFLARE_GATEWAY_ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := NewRegistry(map[string]providerFactory{"openai": tagFactory("openai")})
			cfg := &latest.ModelConfig{Provider: tt.provider, Model: "some-model"}

			_, err := r.createDirectProvider(t.Context(), cfg, environment.NewMapEnvProvider(tt.env))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantVar)
		})
	}
}
