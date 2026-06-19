package root

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/teamloader"
)

// TestRAGToolsetRegistered guards the blank import in toolsets.go: the "rag"
// toolset registers itself with teamloader via init(), so it must resolve in
// the default registry of the CLI binary. If someone drops the blank import,
// "rag" would silently become an unknown toolset type at runtime; this test
// turns that into a build-time failure instead.
func TestRAGToolsetRegistered(t *testing.T) {
	registry := teamloader.NewDefaultToolsetRegistry()

	// A rag toolset with no config fails inside the creator with a config
	// error — not with "unknown toolset type". Reaching that creator-level
	// error proves the "rag" creator is registered.
	_, err := registry.CreateTool(t.Context(), latest.Toolset{Type: "rag"}, "", &config.RuntimeConfig{}, "")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "unknown toolset type",
		"the rag toolset is not registered; check the blank import in cmd/root/toolsets.go")
}
