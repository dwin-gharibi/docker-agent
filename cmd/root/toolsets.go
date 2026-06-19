package root

// Blank-import optional toolsets so they register themselves with teamloader
// (via their init()) and are available in the default toolset registry.
//
// These toolsets are deliberately kept out of teamloader's static import graph
// so embedders that don't need them aren't forced to link their dependencies.
// "rag" in particular pulls in a cgo tree-sitter dependency; importing it here
// keeps the docker-agent CLI's behavior unchanged while letting lighter
// embedders opt out.
import (
	_ "github.com/docker/docker-agent/pkg/tools/builtin/rag"
)
