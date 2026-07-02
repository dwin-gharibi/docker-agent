package providerutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRerankScores(t *testing.T) {
	t.Parallel()

	t.Run("valid JSON", func(t *testing.T) {
		t.Parallel()
		scores, err := ParseRerankScores(`{"scores":[0.9,0.1,0.5]}`, 3)
		require.NoError(t, err)
		assert.Equal(t, []float64{0.9, 0.1, 0.5}, scores)
	})

	t.Run("surrounding whitespace", func(t *testing.T) {
		t.Parallel()
		scores, err := ParseRerankScores("  \n{\"scores\":[1]}\n ", 1)
		require.NoError(t, err)
		assert.Equal(t, []float64{1}, scores)
	})

	t.Run("JSON embedded in prose", func(t *testing.T) {
		t.Parallel()
		scores, err := ParseRerankScores(`Here are the scores: {"scores":[0.3,0.7]} hope that helps`, 2)
		require.NoError(t, err)
		assert.Equal(t, []float64{0.3, 0.7}, scores)
	})

	t.Run("wrong score count", func(t *testing.T) {
		t.Parallel()
		_, err := ParseRerankScores(`{"scores":[0.9]}`, 2)
		assert.EqualError(t, err, "expected 2 scores, got 1")
	})

	t.Run("wrong score count in prose-embedded JSON", func(t *testing.T) {
		t.Parallel()
		_, err := ParseRerankScores(`scores below: {"scores":[0.9]}`, 2)
		assert.EqualError(t, err, "expected 2 scores, got 1")
	})

	t.Run("invalid payload", func(t *testing.T) {
		t.Parallel()
		_, err := ParseRerankScores("not json at all", 1)
		assert.ErrorContains(t, err, "invalid rerank JSON")
	})
}
