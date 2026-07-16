package rag

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker-agent/pkg/rag/database"
	"github.com/docker/docker-agent/pkg/rag/strategy"
	"github.com/docker/docker-agent/pkg/rag/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAbsolutePaths_WithBasePath(t *testing.T) {
	t.Parallel()
	result := GetAbsolutePaths("/base", []string{"relative/file.go", "/absolute/file.go"})
	assert.Equal(t, []string{"/base/relative/file.go", "/absolute/file.go"}, result)
}

func TestGetAbsolutePaths_EmptyBasePath(t *testing.T) {
	t.Parallel()
	// When basePath is empty (OCI/URL sources), relative paths should be
	// resolved against the current working directory instead of producing
	// broken paths like "relative/file.go".
	cwd, err := os.Getwd()
	require.NoError(t, err)

	result := GetAbsolutePaths("", []string{"relative/file.go", "/absolute/file.go"})

	assert.Equal(t, filepath.Join(cwd, "relative", "file.go"), result[0])
	assert.Equal(t, "/absolute/file.go", result[1])
}

func TestGetAbsolutePaths_NilInput(t *testing.T) {
	t.Parallel()
	result := GetAbsolutePaths("/base", nil)
	assert.Nil(t, result)
}

type mockStrategy struct {
	usage types.Usage
}

func (m *mockStrategy) Initialize(ctx context.Context, docPaths []string, chunking strategy.ChunkingConfig) error {
	return nil
}

func (m *mockStrategy) Query(ctx context.Context, query string, numResults int, threshold float64) ([]database.SearchResult, types.Usage, error) {
	return nil, m.usage, nil
}

func (m *mockStrategy) CheckAndReindexChangedFiles(ctx context.Context, docPaths []string, chunking strategy.ChunkingConfig) error {
	return nil
}

func (m *mockStrategy) StartFileWatcher(ctx context.Context, docPaths []string, chunking strategy.ChunkingConfig) error {
	return nil
}

func (m *mockStrategy) Close() error {
	return nil
}

func TestManager_Query_UsageAggregation(t *testing.T) {
	events := make(chan types.Event)
	defer close(events)

	stratA := &mockStrategy{
		usage: types.Usage{
			TotalTokens: 10,
			Cost:        0.1,
			ModelID:     "model-a",
		},
	}
	stratB := &mockStrategy{
		usage: types.Usage{
			TotalTokens: 20,
			Cost:        0.2,
			ModelID:     "model-b",
		},
	}

	cfg := Config{
		StrategyConfigs: []strategy.Config{
			{Name: "stratA", Strategy: stratA},
			{Name: "stratB", Strategy: stratB},
		},
		Results: ResultsConfig{
			Limit: 10,
		},
	}

	mgr, err := New(context.Background(), "test-rag", cfg, events)
	require.NoError(t, err)

	_, usage, err := mgr.Query(context.Background(), "test query")
	require.NoError(t, err)

	assert.Equal(t, int64(30), usage.TotalTokens)
	assert.InDelta(t, 0.3, usage.Cost, 0.0001)
	assert.Contains(t, usage.ModelID, "model-a")
	assert.Contains(t, usage.ModelID, "model-b")
}
