package environment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOnePasswordProvider_Get(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		stored      map[string]string
		resolve     func(ctx context.Context, reference string) (string, bool)
		lookup      string
		wantValue   string
		wantFound   bool
		wantRefSeen string
	}{
		{
			name:      "plain value is passed through",
			stored:    map[string]string{"API_KEY": "plain-secret"},
			lookup:    "API_KEY",
			wantValue: "plain-secret",
			wantFound: true,
		},
		{
			name:        "op reference is resolved",
			stored:      map[string]string{"API_KEY": "op://vault/item/field"},
			lookup:      "API_KEY",
			wantValue:   "resolved-secret",
			wantFound:   true,
			wantRefSeen: "op://vault/item/field",
		},
		{
			name:      "missing variable is not resolved",
			stored:    map[string]string{},
			lookup:    "API_KEY",
			wantFound: false,
		},
		{
			name:   "failed resolution reports not found",
			stored: map[string]string{"API_KEY": "op://vault/item/field"},
			resolve: func(context.Context, string) (string, bool) {
				return "", false
			},
			lookup:    "API_KEY",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var refSeen string
			resolve := tt.resolve
			if resolve == nil {
				resolve = func(_ context.Context, reference string) (string, bool) {
					refSeen = reference
					return "resolved-secret", true
				}
			}

			provider := &OnePasswordProvider{
				provider: NewMapEnvProvider(tt.stored),
				resolve:  resolve,
			}

			value, found := provider.Get(t.Context(), tt.lookup)
			assert.Equal(t, tt.wantFound, found)
			assert.Equal(t, tt.wantValue, value)
			if tt.wantRefSeen != "" {
				assert.Equal(t, tt.wantRefSeen, refSeen)
			}
		})
	}
}

func TestNewOnePasswordProvider_NoBinaryPassesThrough(t *testing.T) {
	t.Parallel()

	// On a system without `op`, the wrapped provider is returned untouched and
	// "op://" values are passed through unchanged.
	if _, err := lookupBinary("op", OnePasswordNotAvailableError{}); err == nil {
		t.Skip("op binary is installed; skipping pass-through test")
	}

	base := NewMapEnvProvider(map[string]string{"API_KEY": "op://vault/item/field"})
	provider := NewOnePasswordProvider(base)

	value, found := provider.Get(t.Context(), "API_KEY")
	assert.True(t, found)
	assert.Equal(t, "op://vault/item/field", value)
}
