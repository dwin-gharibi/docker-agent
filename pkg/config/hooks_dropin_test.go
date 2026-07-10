package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/paths"
)

func writeHookDropIn(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func TestLoadHookDropIns_MissingDir(t *testing.T) {
	t.Parallel()
	assert.Nil(t, loadHookDropIns(filepath.Join(t.TempDir(), "hooks.d")))
}

func TestLoadHookDropIns_LexicographicOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeHookDropIn(t, dir, "20-second.yaml", "stop:\n  - type: command\n    command: echo second\n")
	writeHookDropIn(t, dir, "10-first.yaml", "stop:\n  - type: command\n    command: echo first\n")

	hooks := loadHookDropIns(dir)
	require.NotNil(t, hooks)
	require.Len(t, hooks.Stop, 2)
	assert.Equal(t, "echo first", hooks.Stop[0].Command)
	assert.Equal(t, "echo second", hooks.Stop[1].Command)
}

func TestLoadHookDropIns_MergesAcrossEvents(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeHookDropIn(t, dir, "a.yaml", `
session_start:
  - type: command
    command: echo start
pre_tool_use:
  - matcher: shell
    hooks:
      - type: command
        command: audit.sh
`)
	writeHookDropIn(t, dir, "b.yml", "stop:\n  - type: command\n    command: echo stop\n")

	hooks := loadHookDropIns(dir)
	require.NotNil(t, hooks)
	require.Len(t, hooks.SessionStart, 1)
	assert.Equal(t, "echo start", hooks.SessionStart[0].Command)
	require.Len(t, hooks.PreToolUse, 1)
	assert.Equal(t, "shell", hooks.PreToolUse[0].Matcher)
	require.Len(t, hooks.Stop, 1)
	assert.Equal(t, "echo stop", hooks.Stop[0].Command)
}

// Single-mapping hook events must work in drop-ins like in settings.hooks,
// and strict parsing must keep rejecting typo'd fields inside the mapping.
func TestReadHookDropIn_SingleMapping(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	writeHookDropIn(t, dir, "10-notify.yaml", "stop:\n  type: command\n  command: echo done\n")
	hooks, err := readHookDropIn(filepath.Join(dir, "10-notify.yaml"))
	require.NoError(t, err)
	require.Len(t, hooks.Stop, 1)
	assert.Equal(t, "echo done", hooks.Stop[0].Command)

	writeHookDropIn(t, dir, "20-typo.yaml", "stop:\n  type: command\n  commannd: typo\n")
	_, err = readHookDropIn(filepath.Join(dir, "20-typo.yaml"))
	require.Error(t, err)
}

func TestLoadHookDropIns_SkipsMalformedFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeHookDropIn(t, dir, "10-broken-yaml.yaml", "stop: [unclosed\n")
	writeHookDropIn(t, dir, "20-unknown-key.yaml", "not_a_hook_event:\n  - type: command\n    command: echo hi\n")
	writeHookDropIn(t, dir, "30-invalid-hook-type.yaml", "stop:\n  - type: teleport\n    command: echo hi\n")
	writeHookDropIn(t, dir, "40-valid.yaml", "stop:\n  - type: command\n    command: echo ok\n")

	hooks := loadHookDropIns(dir)
	require.NotNil(t, hooks)
	require.Len(t, hooks.Stop, 1)
	assert.Equal(t, "echo ok", hooks.Stop[0].Command)
}

func TestLoadHookDropIns_IgnoresNonYAMLAndSubdirs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeHookDropIn(t, dir, "README.md", "# not yaml")
	writeHookDropIn(t, dir, "50-disabled.yaml.bak", "stop:\n  - type: command\n    command: echo no\n")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub.yaml"), 0o700))
	writeHookDropIn(t, filepath.Join(dir, "sub.yaml"), "inner.yaml", "stop:\n  - type: command\n    command: echo nested\n")

	assert.Nil(t, loadHookDropIns(dir))
}

func TestLoadHookDropIns_EmptyAndCommentOnlyFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeHookDropIn(t, dir, "a.yaml", "")
	writeHookDropIn(t, dir, "b.yaml", "# comment only\n")

	assert.Nil(t, loadHookDropIns(dir))
}

func TestLoadHookDropIns_UsesConfigDir(t *testing.T) {
	// Not parallel: overrides the process-global config dir.
	configDir := t.TempDir()
	paths.SetConfigDir(configDir)
	t.Cleanup(func() { paths.SetConfigDir("") })

	hooksDir := filepath.Join(configDir, "hooks.d")
	require.NoError(t, os.MkdirAll(hooksDir, 0o700))
	writeHookDropIn(t, hooksDir, "a.yaml", "stop:\n  - type: command\n    command: echo hi\n")

	hooks := LoadHookDropIns()
	require.NotNil(t, hooks)
	require.Len(t, hooks.Stop, 1)
	assert.Equal(t, "echo hi", hooks.Stop[0].Command)
}
