package board

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/paths"
	"github.com/docker/docker-agent/pkg/userconfig"
)

func TestNormalizeProjectPath(t *testing.T) {
	abs, err := normalizeProjectPath("/some/repo")
	require.NoError(t, err)
	assert.Equal(t, "/some/repo", abs)

	// Empty and blank paths are rejected: they would silently validate
	// against the board's working directory.
	_, err = normalizeProjectPath("")
	require.Error(t, err)
	_, err = normalizeProjectPath("   ")
	require.Error(t, err)

	// A leading ~ expands to the home directory.
	abs, err = normalizeProjectPath("~/src/repo")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(paths.GetHomeDir(), "src", "repo"), abs)

	// Relative paths are anchored to the current directory.
	abs, err = normalizeProjectPath("repo")
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(abs))
}

func TestUpdateProject(t *testing.T) {
	paths.SetConfigDir(t.TempDir())
	t.Cleanup(func() { paths.SetConfigDir("") })

	repo := newLocalRepo(t)
	repo2 := newLocalRepo(t)

	cfg, err := userconfig.Load()
	require.NoError(t, err)
	store, err := OpenStore(filepath.Join(t.TempDir(), "cards.json"))
	require.NoError(t, err)
	app := &App{ctx: t.Context(), config: cfg, columns: DefaultColumns, store: store, onChanged: func() {}}

	require.NoError(t, app.AddProject(Project{Name: "one", Path: repo}))
	require.NoError(t, app.AddProject(Project{Name: "two", Path: repo2}))
	require.NoError(t, store.InsertCard(&Card{ID: "card1", Project: "one"}))
	require.NoError(t, store.InsertCard(&Card{ID: "card2", Project: "two"}))

	// Rename, repoint, and set the agent in one update; order is preserved
	// and existing cards follow the rename.
	require.NoError(t, app.UpdateProject("one", Project{Name: "renamed", Path: repo2, Agent: "coder"}))
	projects := app.Projects()
	require.Len(t, projects, 2)
	assert.Equal(t, Project{Name: "renamed", Path: repo2, Agent: "coder"}, projects[0])
	card, err := store.GetCard("card1")
	require.NoError(t, err)
	assert.Equal(t, "renamed", card.Project)
	card, err = store.GetCard("card2")
	require.NoError(t, err)
	assert.Equal(t, "two", card.Project)

	// Unknown project, duplicate name, and AddProject-style validation.
	require.Error(t, app.UpdateProject("one", Project{Name: "x", Path: repo}))
	require.Error(t, app.UpdateProject("renamed", Project{Name: "two", Path: repo}))
	require.Error(t, app.UpdateProject("renamed", Project{Name: "", Path: repo}))
	require.Error(t, app.UpdateProject("renamed", Project{Name: "renamed", Path: ""}))
	require.Error(t, app.UpdateProject("renamed", Project{Name: "renamed", Path: t.TempDir()})) // not a git repo

	// Keeping the name while changing other fields is not a duplicate.
	require.NoError(t, app.UpdateProject("two", Project{Name: "two", Path: repo}))

	// The update is persisted to the config file.
	reloaded, err := userconfig.Load()
	require.NoError(t, err)
	require.Len(t, reloaded.Board.Projects, 2)
	assert.Equal(t, "renamed", reloaded.Board.Projects[0].Name)
	assert.Equal(t, "coder", reloaded.Board.Projects[0].Agent)
}

func TestMoveProject(t *testing.T) {
	paths.SetConfigDir(t.TempDir())
	t.Cleanup(func() { paths.SetConfigDir("") })

	cfg, err := userconfig.Load()
	require.NoError(t, err)
	cfg.Board = &userconfig.Board{Projects: []userconfig.BoardProject{
		{Name: "a", Path: "/a"}, {Name: "b", Path: "/b"}, {Name: "c", Path: "/c"},
	}}
	app := &App{ctx: t.Context(), config: cfg, columns: DefaultColumns}

	names := func() []string {
		var out []string
		for _, p := range app.Projects() {
			out = append(out, p.Name)
		}
		return out
	}

	require.NoError(t, app.MoveProject("c", -1))
	assert.Equal(t, []string{"a", "c", "b"}, names())

	// Moves past either end clamp to a no-op.
	require.NoError(t, app.MoveProject("a", -1))
	require.NoError(t, app.MoveProject("b", 5))
	assert.Equal(t, []string{"a", "c", "b"}, names())

	require.Error(t, app.MoveProject("nope", 1))

	// The new order is persisted to the config file.
	reloaded, err := userconfig.Load()
	require.NoError(t, err)
	assert.Equal(t, "c", reloaded.Board.Projects[1].Name)
}

func TestAttachCommandExplainsFailedRelaunch(t *testing.T) {
	t.Parallel()

	store := testStore(t)
	require.NoError(t, store.InsertCard(&Card{ID: "c1", Status: StatusError, Session: "s", Worktree: t.TempDir()}))

	// The session cannot be recreated: there is no pane holding the error,
	// so attach must report why the relaunch failed instead of a bare
	// "session is gone".
	sessions := &crashingSessions{newErr: errors.New("tmux: bad working directory")}
	c := newController(t.Context(), store, sessions, func() {})
	card, err := store.GetCard("c1")
	require.NoError(t, err)
	c.resume(card)

	app := &App{ctx: t.Context(), store: store, sessions: sessions, controller: c, onChanged: func() {}}
	_, err = app.AttachCommand("c1")
	assert.ErrorContains(t, err, "bad working directory")
}
