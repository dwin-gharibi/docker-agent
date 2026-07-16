package builtins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/hooks"
	"github.com/docker/docker-agent/pkg/hooks/builtins"
)

// TestAddGitStatusReportsUntrackedFiles verifies the happy path:
// inside a fresh git repo with an untracked file, the builtin emits
// turn_start additional context that mentions the file by name. We
// don't pin the exact format of `git status --short --branch` (it
// varies slightly across git versions) but the filename has to make
// it through.
func TestAddGitStatusReportsUntrackedFiles(t *testing.T) {
	t.Parallel()

	dir := initGitRepo(t)
	writeFile(t, dir, "NEWFILE.txt", "hello")

	fn := lookup(t, builtins.AddGitStatus)

	out, err := fn(t.Context(), &hooks.Input{SessionID: "s", Cwd: dir}, nil)
	require.NoError(t, err)
	require.NotNil(t, out)
	require.NotNil(t, out.HookSpecificOutput)
	assert.Equal(t, hooks.EventTurnStart, out.HookSpecificOutput.HookEventName,
		"add_git_status must target turn_start, not session_start")
	require.Len(t, out.HookSpecificOutput.InstructionContext, 1)
	assert.Equal(t, "core/git-status", out.HookSpecificOutput.InstructionContext[0].Key)
	assert.Contains(t, out.HookSpecificOutput.InstructionContext[0].Content, "NEWFILE.txt",
		"git status output must mention the untracked file")
}

// TestAddGitStatusNoCwdIsNoop documents the safety behavior: with an
// empty Cwd the builtin returns nil rather than running git in the
// process's current directory (which would leak host context into the
// prompt).
func TestAddGitStatusNoCwdIsNoop(t *testing.T) {
	t.Parallel()

	fn := lookup(t, builtins.AddGitStatus)

	out, err := fn(t.Context(), &hooks.Input{SessionID: "s"}, nil)
	require.NoError(t, err)
	assert.Nil(t, out)

	out, err = fn(t.Context(), nil, nil)
	require.NoError(t, err)
	assert.Nil(t, out)
}

// TestAddGitStatusNotARepoIsRemoved documents the graceful-failure path.
func TestAddGitStatusNotARepoIsNoop(t *testing.T) {
	t.Parallel()

	fn := lookup(t, builtins.AddGitStatus)

	out, err := fn(t.Context(), &hooks.Input{SessionID: "s", Cwd: t.TempDir()}, nil)
	require.NoError(t, err)
	require.Len(t, out.HookSpecificOutput.InstructionContext, 1)
	assert.True(t, out.HookSpecificOutput.InstructionContext[0].Removed)
}
