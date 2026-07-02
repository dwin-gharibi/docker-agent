package config

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/config/latest"
)

func TestWarnMaxTokens_ExceedsContextSize(t *testing.T) {
	t.Parallel()

	maxTokens := int64(262144)
	cfg := &latest.Config{
		Providers: map[string]latest.ProviderConfig{
			"my_llm": {
				MaxTokens:    &maxTokens,
				ProviderOpts: map[string]any{"context_size": 262144},
			},
		},
	}

	out := captureWarnings(t, func(ctx context.Context, logger *slog.Logger) {
		warnMaxTokensVsContextWindow(ctx, logger, cfg)
	})

	assert.Contains(t, out, "provider my_llm")
	assert.Contains(t, out, "context window")
	assert.Contains(t, out, "3387")
}

// TestWarnMaxTokens_LargeValueWithoutContextSize covers the reporter's exact
// config: max_tokens set to a context-window-sized value with no context_size,
// so the window is not discoverable at load time. The instructional warning is
// the only signal that reaches this user.
func TestWarnMaxTokens_LargeValueWithoutContextSize(t *testing.T) {
	t.Parallel()

	maxTokens := int64(262144)
	cfg := &latest.Config{
		Providers: map[string]latest.ProviderConfig{
			"my_llm": {MaxTokens: &maxTokens},
		},
	}

	out := captureWarnings(t, func(ctx context.Context, logger *slog.Logger) {
		warnMaxTokensVsContextWindow(ctx, logger, cfg)
	})

	assert.Contains(t, out, "provider my_llm")
	assert.Contains(t, out, "context_size")
}

func TestWarnMaxTokens_ReasonableOutputCapIsSilent(t *testing.T) {
	t.Parallel()

	providerMax := int64(4096)
	modelMax := int64(8192)
	cfg := &latest.Config{
		Providers: map[string]latest.ProviderConfig{
			"my_llm": {MaxTokens: &providerMax},
		},
		Models: map[string]latest.ModelConfig{
			"main": {
				MaxTokens:    &modelMax,
				ProviderOpts: map[string]any{"context_size": 262144},
			},
		},
	}

	out := captureWarnings(t, func(ctx context.Context, logger *slog.Logger) {
		warnMaxTokensVsContextWindow(ctx, logger, cfg)
	})

	assert.Empty(t, out, "a sensible output cap below the window must not warn")
}

func TestWarnMaxTokens_ModelExceedsContextSize(t *testing.T) {
	t.Parallel()

	maxTokens := int64(200000)
	cfg := &latest.Config{
		Models: map[string]latest.ModelConfig{
			"main": {
				MaxTokens:    &maxTokens,
				ProviderOpts: map[string]any{"context_size": 131072},
			},
		},
	}

	out := captureWarnings(t, func(ctx context.Context, logger *slog.Logger) {
		warnMaxTokensVsContextWindow(ctx, logger, cfg)
	})

	assert.Contains(t, out, "model main")
	assert.Contains(t, out, "3387")
}
