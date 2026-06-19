package rag

import (
	"context"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/teamloader"
	"github.com/docker/docker-agent/pkg/tools"
)

// init registers the "rag" toolset with teamloader. This package is kept out of
// teamloader's static import graph (it pulls in a cgo tree-sitter dependency via
// pkg/rag), so the "rag" toolset is only linked into binaries that blank-import
// this package. The docker-agent CLI does so; embedders that don't need RAG can
// omit it and build without the cgo dependency.
//
//nolint:gochecknoinits // Intentional: self-registers the optional rag toolset so a blank import enables it without pulling cgo into teamloader's import graph.
func init() {
	teamloader.RegisterToolsetCreator("rag", func(ctx context.Context, toolset latest.Toolset, parentDir string, runConfig *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
		return CreateToolSet(ctx, toolset, parentDir, runConfig)
	})
}
