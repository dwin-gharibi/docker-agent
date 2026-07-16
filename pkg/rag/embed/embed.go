package embed

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/docker/docker-agent/pkg/model/provider"
)

// Embedder generates vector embeddings for text
type Embedder struct {
	provider       provider.Provider
	batchSize      int // Batch size for API calls
	maxConcurrency int // Maximum concurrent embedding batch requests
}

// Option is a functional option for configuring the Embedder
type Option func(*Embedder)

// WithBatchSize sets the batch size for embedding API calls (default: 50)
func WithBatchSize(size int) Option {
	return func(e *Embedder) {
		e.batchSize = size
	}
}

// WithMaxConcurrency sets the maximum concurrent embedding batch requests (default: 5)
func WithMaxConcurrency(maxConcurrency int) Option {
	return func(e *Embedder) {
		e.maxConcurrency = maxConcurrency
	}
}

// New creates a new embedder using a model provider with optional configuration
func New(p provider.Provider, opts ...Option) *Embedder {
	e := &Embedder{
		provider:       p,
		batchSize:      50,
		maxConcurrency: 5,
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Embed generates an embedding for a single text
func (e *Embedder) Embed(ctx context.Context, text string) ([]float64, int64, error) {
	// Try to use the provider's embedding API if it implements EmbeddingProvider.
	if embeddingProvider, ok := e.provider.(provider.EmbeddingProvider); ok {
		result, err := embeddingProvider.CreateEmbedding(ctx, text)
		if err != nil {
			return nil, 0, err
		}

		slog.DebugContext(ctx, "Embedding generated",
			"provider", e.provider.ID(),
			"tokens", result.TotalTokens,
			"cost", result.Cost)

		return result.Embedding, result.TotalTokens, nil
	}

	// Provider does not support embeddings via the standard interface; fail fast.
	return nil, 0, fmt.Errorf("provider %s does not support embeddings", e.provider.ID())
}

// EmbedBatch generates embeddings for multiple texts using intelligent batching
// If the provider supports batch embeddings, it will use parallel batch API calls
// Otherwise, it falls back to sequential processing
func (e *Embedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, int64, error) {
	if len(texts) == 0 {
		return [][]float64{}, 0, nil
	}

	// Check if provider supports batch embeddings.
	if batchProvider, ok := e.provider.(provider.BatchEmbeddingProvider); ok {
		return e.embedBatchOptimized(ctx, batchProvider, texts)
	}

	// Fall back to sequential processing for providers without batch support
	slog.DebugContext(ctx, "Provider doesn't support batch embeddings, using sequential processing",
		"provider", e.provider.ID(),
		"text_count", len(texts))

	embeddings := make([][]float64, len(texts))
	var totalTokens int64
	for i, text := range texts {
		embedding, tokens, err := e.Embed(ctx, text)
		totalTokens += tokens
		if err != nil {
			return nil, totalTokens, fmt.Errorf("failed to embed text %d: %w", i, err)
		}
		embeddings[i] = embedding
	}

	return embeddings, totalTokens, nil
}

// embedBatchOptimized processes texts in optimized batches with parallel API calls
func (e *Embedder) embedBatchOptimized(ctx context.Context, batchProvider provider.BatchEmbeddingProvider, texts []string) ([][]float64, int64, error) {
	totalTexts := len(texts)
	slog.DebugContext(ctx, "Starting optimized batch embedding",
		"provider", e.provider.ID(),
		"total_texts", totalTexts,
		"batch_size", e.batchSize,
		"max_concurrency", e.maxConcurrency)

	// Pre-allocate results
	embeddings := make([][]float64, totalTexts)
	var mu sync.Mutex
	var totalTokens int64

	// Create errgroup with concurrency limit
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(e.maxConcurrency)

	// Process batches in parallel
	for start := 0; start < totalTexts; start += e.batchSize {
		end := min(start+e.batchSize, totalTexts)

		g.Go(func() error {
			batchTexts := texts[start:end]
			batchNum := start/e.batchSize + 1
			numBatches := (totalTexts + e.batchSize - 1) / e.batchSize

			slog.DebugContext(ctx, "Processing batch",
				"batch", batchNum,
				"total_batches", numBatches,
				"batch_size", len(batchTexts),
				"start_idx", start)

			// Make batch API call
			result, err := batchProvider.CreateBatchEmbedding(ctx, batchTexts)
			if err != nil {
				return fmt.Errorf("batch %d failed: %w", batchNum, err)
			}

			// Store results (mutex protects slice writes)
			mu.Lock()
			copy(embeddings[start:end], result.Embeddings)
			totalTokens += result.TotalTokens
			mu.Unlock()

			slog.DebugContext(ctx, "Batch completed",
				"batch", batchNum,
				"embeddings", len(result.Embeddings),
				"tokens", result.TotalTokens,
				"cost", result.Cost)

			return nil
		})
	}

	// Wait for all batches and return first error if any
	if err := g.Wait(); err != nil {
		return nil, totalTokens, err
	}

	slog.DebugContext(ctx, "Batch embedding completed",
		"provider", e.provider.ID(),
		"total_embeddings", len(embeddings),
		"batches_processed", (totalTexts+e.batchSize-1)/e.batchSize)

	return embeddings, totalTokens, nil
}
