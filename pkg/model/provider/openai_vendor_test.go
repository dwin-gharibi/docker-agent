package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/config/latest"
)

// TestApplyProviderDefaults_OpenAIVendorResolution covers the same
// resolution cases the old ProviderOpts-marker mechanism used to test, but
// against isOpenAIVendor applied to the enhanced (post-defaults) config —
// the vendor decision pkg/model/provider/factory.go now threads to the
// OpenAI client via options.WithOpenAIVendor instead of writing it into
// ProviderOpts. See defaults.go's doc comment on isOpenAIVendor.
func TestApplyProviderDefaults_OpenAIVendorResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		config          *latest.ModelConfig
		customProviders map[string]latest.ProviderConfig
		want            bool
	}{
		{
			name:   "named custom provider with omitted underlying provider defaults to OpenAI vendor",
			config: &latest.ModelConfig{Provider: "my_openai", Model: "gpt-5.6"},
			customProviders: map[string]latest.ProviderConfig{
				"my_openai": {BaseURL: "https://api.openai.com/v1", TokenKey: "MY_KEY"},
			},
			want: true,
		},
		{
			name:   "named custom provider with explicit openai_responses api_type",
			config: &latest.ModelConfig{Provider: "my_gateway", Model: "gpt-5.6"},
			customProviders: map[string]latest.ProviderConfig{
				"my_gateway": {BaseURL: "https://gateway.example.com/v1", APIType: "openai_responses"},
			},
			want: true,
		},
		{
			name:   "custom provider explicitly pointed at a non-OpenAI vendor is not recognized",
			config: &latest.ModelConfig{Provider: "claude_gateway", Model: "claude-x"},
			customProviders: map[string]latest.ProviderConfig{
				"claude_gateway": {Provider: "anthropic", BaseURL: "https://gateway.example.com"},
			},
			want: false,
		},
		{
			name:   "known alias (xai) is never recognized even though its api_type resolves to openai",
			config: &latest.ModelConfig{Provider: "xai", Model: "gpt-5.6"},
			want:   false,
		},
		{
			name:   "known alias (mistral) is never recognized",
			config: &latest.ModelConfig{Provider: "mistral", Model: "gpt-5.6-sol"},
			want:   false,
		},
		{
			name:   "direct anthropic provider is not recognized",
			config: &latest.ModelConfig{Provider: "anthropic", Model: "claude-sonnet-4-0"},
			want:   false,
		},
		{
			name:   "unknown provider without any OpenAI api_type is not recognized",
			config: &latest.ModelConfig{Provider: "totally-unknown", Model: "some-model"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := applyProviderDefaults(tt.config, tt.customProviders)

			assert.Equal(t, tt.want, isOpenAIVendor(got))
			// The vendor decision must never leak into the public, user-
			// controllable ProviderOpts map (that was the security bug this
			// mechanism replaced): a user-set provider_opts.openai_vendor is
			// unrelated pass-through config now, not a trusted signal.
			_, hasKey := got.ProviderOpts["openai_vendor"]
			assert.False(t, hasKey, "applyProviderDefaults must not stamp an openai_vendor key into ProviderOpts")
		})
	}
}

// TestIsUnrecognizedOpenAIProtocolProvider unit-tests the predicate directly,
// independent of the defaults-merging pipeline.
func TestIsUnrecognizedOpenAIProtocolProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config *latest.ModelConfig
		want   bool
	}{
		{
			name:   "unknown provider with explicit openai_chatcompletions api_type",
			config: &latest.ModelConfig{Provider: "my_openai", ProviderOpts: map[string]any{"api_type": "openai_chatcompletions"}},
			want:   true,
		},
		{
			name:   "unknown provider with explicit openai_responses api_type",
			config: &latest.ModelConfig{Provider: "my_openai", ProviderOpts: map[string]any{"api_type": "openai_responses"}},
			want:   true,
		},
		{
			name:   "unknown provider with a non-OpenAI api_type",
			config: &latest.ModelConfig{Provider: "my_openai", ProviderOpts: map[string]any{"api_type": "anthropic"}},
			want:   false,
		},
		{
			name:   "known alias (xai) with an OpenAI-shaped api_type is excluded",
			config: &latest.ModelConfig{Provider: "xai", ProviderOpts: map[string]any{"api_type": "openai_chatcompletions"}},
			want:   false,
		},
		{
			name:   "known alias (azure) with its default api_type is excluded",
			config: &latest.ModelConfig{Provider: "azure"},
			want:   false,
		},
		{
			name:   "unknown provider name falling back to itself as provider type",
			config: &latest.ModelConfig{Provider: "some-random-name"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isUnrecognizedOpenAIProtocolProvider(tt.config))
		})
	}
}

// TestIsOpenAIVendor_UnrecognizedCustomProvider is a regression guard scoped
// tightly to the third-review finding: isOpenAIVendor must recognize the
// documented "named custom provider with omitted underlying provider"
// pattern in addition to the direct/chatgpt/azure/qualified-model cases
// already covered by modelinfo.IsOpenAIVendor.
func TestIsOpenAIVendor_UnrecognizedCustomProvider(t *testing.T) {
	t.Parallel()

	cfg := &latest.ModelConfig{
		Provider:     "my_openai",
		Model:        "gpt-5.6",
		ProviderOpts: map[string]any{"api_type": "openai_responses"},
	}
	require.True(t, isOpenAIVendor(cfg))

	cfg2 := &latest.ModelConfig{
		Provider:     "xai",
		Model:        "gpt-5.6",
		ProviderOpts: map[string]any{"api_type": "openai_chatcompletions"},
	}
	require.False(t, isOpenAIVendor(cfg2))
}
