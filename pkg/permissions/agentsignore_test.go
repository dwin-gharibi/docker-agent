package permissions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/fsx"
)

func writeIgnore(t *testing.T, dir, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, fsx.AgentsIgnoreFile), []byte(body), 0o644))
}

func TestFromAgentsIgnoreAbsent(t *testing.T) {
	assert.Nil(t, FromAgentsIgnore(t.TempDir()))
}

func TestFromAgentsIgnoreNoPatterns(t *testing.T) {
	dir := t.TempDir()
	writeIgnore(t, dir, "# nothing here\n\n")
	assert.Nil(t, FromAgentsIgnore(dir))
}

func TestFromAgentsIgnoreDeniesReadAndWrite(t *testing.T) {
	dir := t.TempDir()
	writeIgnore(t, dir, "secrets.env\n")

	c := FromAgentsIgnore(dir)
	require.NotNil(t, c)

	for _, tool := range []string{"read_file", "write_file", "edit_file"} {
		assert.Equalf(t, Deny, c.CheckWithArgs(tool, map[string]any{"path": "secrets.env"}),
			"%s on an ignored path must be denied", tool)
	}
	assert.Equal(t, Ask, c.CheckWithArgs("read_file", map[string]any{"path": "README.md"}),
		"unignored paths are untouched")
}

func TestFromAgentsIgnoreCoversNestedPaths(t *testing.T) {
	dir := t.TempDir()
	writeIgnore(t, dir, "secrets.env\n")

	c := FromAgentsIgnore(dir)
	require.NotNil(t, c)
	assert.Equal(t, Deny, c.CheckWithArgs("read_file", map[string]any{"path": "config/secrets.env"}))
}

func TestFromAgentsIgnoreDirectoryCoversContents(t *testing.T) {
	dir := t.TempDir()
	writeIgnore(t, dir, "build/\n")

	c := FromAgentsIgnore(dir)
	require.NotNil(t, c)
	assert.Equal(t, Deny, c.CheckWithArgs("read_file", map[string]any{"path": "build/out.bin"}))
}

func TestFromAgentsIgnoreSkipsNegations(t *testing.T) {
	dir := t.TempDir()
	writeIgnore(t, dir, "!keep.key\n")

	assert.Nil(t, FromAgentsIgnore(dir),
		"a negation-only file must not produce a deny rule for keep.key")
}

func TestPermissionGlobsFor(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		want    []string
	}{
		{"secrets.env", []string{"secrets.env", "secrets.env/*", "*/secrets.env", "*/secrets.env/*"}},
		{"build/", []string{"build", "build/*", "*/build", "*/build/*"}},
		{"docs/notes.md", []string{"docs/notes.md", "docs/notes.md/*"}},
		{"!keep.key", nil},
		{"# comment", nil},
		{"", nil},
		{"/", nil},
	} {
		assert.Equalf(t, tc.want, permissionGlobsFor(tc.pattern), "pattern %q", tc.pattern)
	}
}
