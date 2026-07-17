package embed

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/model/provider"
	"github.com/docker/docker-agent/pkg/model/provider/base"
	"github.com/docker/docker-agent/pkg/modelsdev"
)

// mockEmbeddingProvider fakes a provider that fails on the 3rd embedding
type mockEmbeddingProvider struct {
	provider.Provider

	callCount int
}

func (m *mockEmbeddingProvider) ID() modelsdev.ID {
	return modelsdev.ID{}
}

func (m *mockEmbeddingProvider) BaseConfig() base.Config {
	return base.Config{}
}

func (m *mockEmbeddingProvider) CreateEmbedding(ctx context.Context, text string) (*base.EmbeddingResult, error) {
	m.callCount++
	if m.callCount == 3 {
		return nil, errors.New("simulated failure")
	}
	return &base.EmbeddingResult{
		Embedding:   []float64{0.1, 0.2, 0.3},
		TotalTokens: 10,
		Cost:        0.001,
	}, nil
}

// verify we implement EmbeddingProvider
var (
	_ provider.EmbeddingProvider = (*mockEmbeddingProvider)(nil)
	_ provider.Provider          = (*mockEmbeddingProvider)(nil)
)

func TestEmbedBatch_PartialUsageOnError(t *testing.T) {
	mockProv := &mockEmbeddingProvider{}
	embedder := New(mockProv, WithBatchSize(10), WithMaxConcurrency(1))

	// Provide 4 texts. The 3rd will fail.
	texts := []string{"text1", "text2", "text3", "text4"}

	embeddings, tokens, err := embedder.EmbedBatch(t.Context(), texts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated failure")

	// EmbedBatch should return nil for embeddings since it failed
	assert.Nil(t, embeddings)

	// Tokens should be 20 (10 from text1 + 10 from text2)
	assert.Equal(t, int64(20), tokens)
}
