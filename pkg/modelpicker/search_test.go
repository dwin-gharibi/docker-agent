package modelpicker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/runtime"
)

func TestFilterUsesFuzzyModelMetadataSearch(t *testing.T) {
	t.Parallel()

	choices := []runtime.ModelChoice{
		{Name: "Claude Sonnet", Ref: "sonnet", Provider: "anthropic", Model: "claude-sonnet-4-6"},
		{Name: "GPT Five", Ref: "gpt", Provider: "openai", Model: "gpt-5"},
	}

	matches := Filter(choices, "cldsn46")
	require.Len(t, matches, 1)
	assert.Equal(t, "sonnet", matches[0].Ref)
}

func TestFilterEmptyQueryPreservesOrder(t *testing.T) {
	t.Parallel()

	choices := []runtime.ModelChoice{{Ref: "second"}, {Ref: "first"}}
	assert.Equal(t, choices, Filter(choices, ""))
}
