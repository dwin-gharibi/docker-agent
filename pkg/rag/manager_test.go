package rag

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/rag/database"
	"github.com/docker/docker-agent/pkg/rag/strategy"
)

// blockingStrategy deliberately ignores ctx and blocks Initialize and Query
// until release is closed.
type blockingStrategy struct {
	started chan struct{}
	release chan struct{}
}

func newBlockingStrategy(release chan struct{}) *blockingStrategy {
	return &blockingStrategy{started: make(chan struct{}), release: release}
}

func (s *blockingStrategy) block() {
	close(s.started)
	<-s.release
}

func (s *blockingStrategy) Initialize(context.Context, []string, strategy.ChunkingConfig) error {
	s.block()
	return nil
}

func (s *blockingStrategy) Query(context.Context, string, int, float64) ([]database.SearchResult, error) {
	s.block()
	return nil, nil
}

func (s *blockingStrategy) CheckAndReindexChangedFiles(context.Context, []string, strategy.ChunkingConfig) error {
	return nil
}

func (s *blockingStrategy) StartFileWatcher(context.Context, []string, strategy.ChunkingConfig) error {
	return nil
}

func (s *blockingStrategy) Close() error { return nil }

func TestInitializeReturnsOnContextCancellationWithBlockedStrategy(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	blocked := newBlockingStrategy(release)

	cfg := Config{
		StrategyConfigs: []strategy.Config{{Name: "blocked", Strategy: blocked}},
	}
	m, err := New(t.Context(), "test", cfg, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- m.Initialize(ctx) }()

	<-blocked.started
	cancel()

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Initialize did not return after context cancellation")
	}
}

func TestQueryReturnsOnContextCancellationWithBlockedStrategy(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	first := newBlockingStrategy(release)
	second := newBlockingStrategy(release)

	// Two strategies so Query takes the multi-strategy fan-in path.
	cfg := Config{
		StrategyConfigs: []strategy.Config{
			{Name: "first", Strategy: first, Limit: 5},
			{Name: "second", Strategy: second, Limit: 5},
		},
	}
	m, err := New(t.Context(), "test", cfg, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	type queryResult struct {
		results []database.SearchResult
		err     error
	}
	resCh := make(chan queryResult, 1)
	go func() {
		results, err := m.Query(ctx, "some query")
		resCh <- queryResult{results: results, err: err}
	}()

	<-first.started
	<-second.started
	cancel()

	select {
	case res := <-resCh:
		require.ErrorIs(t, res.err, context.Canceled)
		assert.Nil(t, res.results)
	case <-time.After(2 * time.Second):
		t.Fatal("Query did not return after context cancellation")
	}
}

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
