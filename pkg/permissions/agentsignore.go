package permissions

import (
	"path/filepath"
	"strings"

	"github.com/docker/docker-agent/pkg/fsx"
)

var pathToolArgs = []struct {
	tool string
	arg  string
}{
	{"read_file", "path"},
	// read_multiple_files is intentionally omitted: its "paths" argument is an
	// array, not a scalar string. The permission evaluator formats arrays as
	// "[elem1 elem2]", so a scalar glob like "secrets.env" can never match.
	// Per-path enforcement for read_multiple_files happens in the filesystem
	// layer (resolveAndCheckPath called for each element) and is unaffected.
	{"write_file", "path"},
	{"edit_file", "path"},
	{"create_directory", "path"},
	{"remove_directory", "path"},
	{"list_directory", "path"},
	{"directory_tree", "path"},
	{"search_files_content", "path"},
}

func FromAgentsIgnore(startDir string) *Checker {
	path := fsx.FindAgentsIgnore(startDir)
	if path == "" {
		return nil
	}
	patterns, err := fsx.ReadAgentsIgnoreGlobs(path)
	if err != nil || len(patterns) == 0 {
		return nil
	}

	var deny []string
	for _, p := range patterns {
		for _, glob := range permissionGlobsFor(p) {
			for _, t := range pathToolArgs {
				deny = append(deny, t.tool+":"+t.arg+"="+glob)
			}
		}
	}
	if len(deny) == 0 {
		return nil
	}
	return NewCheckerFromRules(nil, nil, deny)
}

func permissionGlobsFor(pattern string) []string {
	p := strings.TrimSpace(pattern)
	if p == "" || strings.HasPrefix(p, "#") || strings.HasPrefix(p, "!") {
		// NOTE: negation patterns (e.g. "!public.key") are intentionally dropped
		// here because a deny-list cannot express "except this path". The
		// filesystem layer re-includes negated paths correctly (gitignore
		// semantics), so actual access is unaffected. However, the advisory
		// /permissions display may show a path as denied even when the agent
		// can actually reach it — for example, if ".agentsignore" contains
		// "*.key" and "!public.key", the display will flag public.key as
		// blocked even though the filesystem layer permits it. This is a
		// known limitation of the advisory permission layer.
		return nil
	}
	p = strings.TrimSuffix(p, "/")
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return nil
	}
	base := filepath.ToSlash(p)
	globs := []string{base, base + "/*"}
	if !strings.Contains(base, "/") {
		globs = append(globs, "*/"+base, "*/"+base+"/*")
	}
	return globs
}
