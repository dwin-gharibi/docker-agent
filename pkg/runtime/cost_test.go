package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/modelsdev"
)

// TestUsageHasTokens covers the helper that suppresses the missing-price
// warning for empty/no-op turns. The per-message cost arithmetic and its
// nil/unpriced branches are exercised by TestComputeMessageCost in
// after_llm_call_test.go, which shares the same computeMessageCost source.
func TestUsageHasTokens(t *testing.T) {
	t.Parallel()
	assert.False(t, usageHasTokens(nil), "nil usage has no tokens")
	assert.False(t, usageHasTokens(&chat.Usage{}), "zero usage has no tokens")
	assert.True(t, usageHasTokens(&chat.Usage{InputTokens: 1}))
	assert.True(t, usageHasTokens(&chat.Usage{OutputTokens: 1}))
	assert.True(t, usageHasTokens(&chat.Usage{CachedInputTokens: 1}))
	assert.True(t, usageHasTokens(&chat.Usage{CacheWriteTokens: 1}))
}

// TestApplyConfigCost pins the config price-table override: it must take
// precedence over the catalogue, price uncatalogued models, never mutate the
// (shared, store-cached) catalogue entry, and be a no-op when unset.
func TestApplyConfigCost(t *testing.T) {
	t.Parallel()

	id := modelsdev.NewID("custom", "my-model")
	usage := &chat.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}

	t.Run("nil override is a no-op", func(t *testing.T) {
		t.Parallel()
		m := &modelsdev.Model{Cost: &modelsdev.Cost{Input: 3}}
		assert.Same(t, m, applyConfigCost(m, id, nil))
		assert.Nil(t, applyConfigCost(nil, id, nil))
	})

	t.Run("override replaces the catalogue price without mutating it", func(t *testing.T) {
		t.Parallel()
		catalogued := &modelsdev.Model{Name: "My Model", Cost: &modelsdev.Cost{Input: 3, Output: 15}}
		m := applyConfigCost(catalogued, id, &latest.CostConfig{Input: 1.25, Output: 5})

		cost := computeMessageCost(usage, m)
		require.NotNil(t, cost)
		assert.InEpsilon(t, 6.25, *cost, 0.0001)
		assert.Equal(t, "My Model", m.Name, "catalogue metadata must be preserved")
		assert.InEpsilon(t, 3.0, catalogued.Cost.Input, 0.0001, "the cached catalogue entry must not be mutated")
	})

	t.Run("override prices an uncatalogued model", func(t *testing.T) {
		t.Parallel()
		m := applyConfigCost(nil, id, &latest.CostConfig{Input: 2, Output: 4})

		cost := computeMessageCost(usage, m)
		require.NotNil(t, cost, "a config cost must make an uncatalogued model priced")
		assert.InEpsilon(t, 6.0, *cost, 0.0001)
		assert.Equal(t, "my-model", m.Name, "synthesized entry carries the model name for telemetry")
	})

	t.Run("all-zero override is priced free, not unpriced", func(t *testing.T) {
		t.Parallel()
		m := applyConfigCost(nil, id, &latest.CostConfig{})

		cost := computeMessageCost(usage, m)
		require.NotNil(t, cost, "a zero price table still counts as priced")
		assert.Zero(t, *cost)
	})
}
