package ui_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUIDoesNotImportRuntimePackages(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"github.com/docker/docker-agent/pkg/runtime",
		"github.com/docker/docker-agent/pkg/app",
		"github.com/docker/docker-agent/pkg/effort",
		"github.com/docker/docker-agent/pkg/session",
		"github.com/docker/docker-agent/pkg/sessiontitle",
		"github.com/docker/docker-agent/pkg/gitbranch",
		"github.com/docker/docker-agent/pkg/tui/messages",
		"github.com/docker/docker-agent/pkg/chat",
	}

	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range parsed.Imports {
			path := strings.Trim(imp.Path.Value, "\"")
			for _, denied := range forbidden {
				require.NotEqualf(t, denied, path, "%s imports forbidden runtime package %s", file, path)
			}
		}
	}
}
