package environment

import (
	"context"
	"log/slog"
	"strings"
)

// onePasswordPrefix marks an environment value as a 1Password secret reference
// (e.g. "op://vault/item/field") that must be resolved with the `op` CLI.
const onePasswordPrefix = "op://"

// OnePasswordProvider decorates another Provider and resolves 1Password secret
// references. When the wrapped provider returns a value starting with "op://",
// the value is treated as a secret reference and resolved using the `op read`
// CLI command. All other values are passed through unchanged.
type OnePasswordProvider struct {
	provider Provider
	// resolve turns a "op://..." reference into its secret value. It is a field
	// so tests can inject a fake resolver without relying on the `op` binary.
	resolve func(ctx context.Context, reference string) (string, bool)
}

type OnePasswordNotAvailableError struct{}

func (OnePasswordNotAvailableError) Error() string {
	return "op (1Password CLI) is not installed"
}

// NewOnePasswordProvider wraps provider so that "op://" references are resolved
// with the `op` CLI. If the `op` binary is not installed, provider is returned
// unchanged so that values are passed through untouched.
func NewOnePasswordProvider(provider Provider) Provider {
	path, err := lookupBinary("op", OnePasswordNotAvailableError{})
	if err != nil {
		return provider
	}

	return &OnePasswordProvider{
		provider: provider,
		resolve: func(ctx context.Context, reference string) (string, bool) {
			return runCommand(ctx, "1password", path, "read", reference)
		},
	}
}

func (p *OnePasswordProvider) Get(ctx context.Context, name string) (string, bool) {
	value, found := p.provider.Get(ctx, name)
	if !found || !strings.HasPrefix(value, onePasswordPrefix) {
		return value, found
	}

	resolved, ok := p.resolve(ctx, value)
	if !ok {
		slog.WarnContext(ctx, "Failed to resolve 1Password secret reference", "name", name)
		return "", false
	}

	return resolved, true
}
