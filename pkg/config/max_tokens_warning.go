package config

import (
	"context"
	"log/slog"

	"github.com/docker/docker-agent/pkg/config/latest"
)

// suspiciousMaxTokens is the threshold above which a max_tokens set without a
// discoverable context_size is flagged. Legitimate output caps are small (a
// few thousand tokens); a value this large almost always means max_tokens (the
// per-response OUTPUT cap) was confused with the model's context window, which
// leaves no room for the prompt and makes every request fail (issue #3387).
const suspiciousMaxTokens = 100_000

// warnMaxTokensVsContextWindow flags provider/model configs whose max_tokens is
// set so high that it cannot coexist with a prompt. max_tokens is the maximum
// OUTPUT tokens per response, not the context window; OpenAI-compatible servers
// (e.g. vLLM) reject a request when prompt_tokens + max_tokens exceeds the
// window, so max_tokens >= the window rejects even a one-token "hello".
//
// The check is advisory and best-effort: it inspects each provider and model
// entry independently using that entry's own max_tokens and context_size and
// does not resolve provider->model inheritance. The runtime clamp in the
// provider clients is the actual guard; this warning surfaces the likely
// misconfiguration at load time, including for models whose window is not in
// the models.dev catalogue (where the clamp cannot engage on its own).
//
// The logger is injected so tests can capture warnings without racing on the
// global default logger.
func warnMaxTokensVsContextWindow(ctx context.Context, logger *slog.Logger, cfg *latest.Config) {
	if logger == nil {
		logger = slog.Default()
	}
	for name, p := range cfg.Providers {
		warnMaxTokensEntry(ctx, logger, "provider "+name, p.MaxTokens, latest.ContextSizeFromProviderOpts(p.ProviderOpts))
	}
	for name, m := range cfg.Models {
		warnMaxTokensEntry(ctx, logger, "model "+name, m.MaxTokens, latest.ContextSizeFromProviderOpts(m.ProviderOpts))
	}
}

func warnMaxTokensEntry(ctx context.Context, logger *slog.Logger, loc string, maxTokens *int64, contextSize int64) {
	if maxTokens == nil || *maxTokens <= 0 {
		return
	}
	switch {
	case contextSize > 0 && *maxTokens >= contextSize:
		logger.WarnContext(ctx,
			"max_tokens is the maximum output tokens per response, not the context window; it is >= context_size and will be clamped to leave room for the prompt. Set a smaller max_tokens to silence this",
			"location", loc,
			"max_tokens", *maxTokens,
			"context_size", contextSize,
			"see", "https://github.com/docker/docker-agent/issues/3387",
		)
	case contextSize == 0 && *maxTokens >= suspiciousMaxTokens:
		logger.WarnContext(ctx,
			"max_tokens is the maximum output tokens per response, not the context window; this value is unusually large. If requests fail with 'maximum context length', lower max_tokens or set provider_opts.context_size to the model's real window",
			"location", loc,
			"max_tokens", *maxTokens,
			"see", "https://github.com/docker/docker-agent/issues/3387",
		)
	}
}
