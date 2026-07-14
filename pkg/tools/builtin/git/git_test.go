package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
)

var fixedTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func sig() *object.Signature {
	return &object.Signature{Name: "Alice", Email: "alice@example.com", When: fixedTime}
}

func newRepo(t *testing.T) (string, *gogit.Repository) {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)
	return dir, repo
}

func addCommit(t *testing.T, repo *gogit.Repository, dir string, files map[string]string, msg string) plumbing.Hash {
	t.Helper()
	wt, err := repo.Worktree()
	require.NoError(t, err)
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
		_, err := wt.Add(name)
		require.NoError(t, err)
	}
	h, err := wt.Commit(msg, &gogit.CommitOptions{Author: sig()})
	require.NoError(t, err)
	return h
}

func TestGitStatus(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	addCommit(t, repo, dir, map[string]string{"a.txt": "hello\nworld\n"}, "initial")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new\n"), 0o644))

	res, err := New(dir).status(context.Background(), StatusArgs{})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Output)
	require.Contains(t, res.Output, "a.txt")
	require.Contains(t, res.Output, "b.txt")
}

func TestGitStatusClean(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	addCommit(t, repo, dir, map[string]string{"a.txt": "x\n"}, "initial")

	res, err := New(dir).status(context.Background(), StatusArgs{})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "clean")
}

func TestGitLog(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	addCommit(t, repo, dir, map[string]string{"a.txt": "1\n"}, "first commit")
	addCommit(t, repo, dir, map[string]string{"a.txt": "2\n"}, "second commit")

	res, err := New(dir).log(context.Background(), LogArgs{})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Output)
	require.Contains(t, res.Output, "first commit")
	require.Contains(t, res.Output, "second commit")

	res2, err := New(dir).log(context.Background(), LogArgs{Limit: 1})
	require.NoError(t, err)
	require.Contains(t, res2.Output, "second commit")
	require.NotContains(t, res2.Output, "first commit")
}

func TestGitBranches(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	addCommit(t, repo, dir, map[string]string{"a.txt": "x\n"}, "initial")

	head, err := repo.Head()
	require.NoError(t, err)
	current := head.Name().Short()

	res, err := New(dir).branches(context.Background(), BranchesArgs{})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Output)
	require.Contains(t, res.Output, current)
	require.Contains(t, res.Output, "*")
}

func TestGitShow(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	addCommit(t, repo, dir, map[string]string{"a.txt": "one\ntwo\n"}, "add a.txt")

	res, err := New(dir).show(context.Background(), ShowArgs{})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Output)
	require.Contains(t, res.Output, "add a.txt")
	require.Contains(t, res.Output, "Alice")
	require.Contains(t, res.Output, "a.txt")
}

func TestGitBlame(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	addCommit(t, repo, dir, map[string]string{"a.txt": "line one\nline two\n"}, "initial")

	res, err := New(dir).blame(context.Background(), BlameArgs{Path: "a.txt"})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Output)
	require.Contains(t, res.Output, "alice@example.com")
	require.Contains(t, res.Output, "line one")
}

func TestGitBlameRequiresPath(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	addCommit(t, repo, dir, map[string]string{"a.txt": "x\n"}, "initial")

	res, err := New(dir).blame(context.Background(), BlameArgs{})
	require.NoError(t, err)
	require.True(t, res.IsError)
}

func TestGitNotARepo(t *testing.T) {
	t.Parallel()
	res, err := New(t.TempDir()).status(context.Background(), StatusArgs{})
	require.NoError(t, err)
	require.True(t, res.IsError)
}

func TestGitToolSetInterfaces(t *testing.T) {
	t.Parallel()
	ts := New(t.TempDir())
	toolz, err := ts.Tools(context.Background())
	require.NoError(t, err)
	require.Len(t, toolz, 5)
	require.NotEmpty(t, ts.Instructions())
}
