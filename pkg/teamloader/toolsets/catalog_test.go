package toolsets

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinToolsetsCatalogMatchesRegistry(t *testing.T) {
	t.Parallel()

	registryTypes := slices.Sorted(maps.Keys(DefaultToolsetCreators()))

	catalogTypes := make([]string, 0, len(BuiltinToolsets))
	for _, ts := range BuiltinToolsets {
		catalogTypes = append(catalogTypes, ts.Type)
	}
	slices.Sort(catalogTypes)

	require.Equal(t, registryTypes, catalogTypes,
		"catalog and registry are out of sync; update pkg/teamloader/toolsets/catalog.go to document exactly the registered toolset types")
}

func TestBuiltinToolsetsHaveSummaries(t *testing.T) {
	t.Parallel()

	for _, ts := range BuiltinToolsets {
		require.NotEmptyf(t, ts.Summary, "toolset %q must have a summary", ts.Type)
	}
}
