package root

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/session"
)

// localBackendWithStore returns a localBackend whose session store is the
// supplied in-memory one, bypassing the lazy SQLite open so ResumeWorkingDir
// can be tested without touching disk.
func localBackendWithStore(f *runExecFlags, store session.Store) *localBackend {
	b := &localBackend{flags: f}
	b.storeOnce.Do(func() { b.store = store })
	return b
}

func TestResumeWorkingDir(t *testing.T) {
	t.Run("returns the stored working dir of an existing session", func(t *testing.T) {
		dir := t.TempDir()
		store := session.NewInMemorySessionStore()
		require.NoError(t, store.AddSession(t.Context(), session.New(
			session.WithID("card-1"),
			session.WithWorkingDir(dir),
		)))

		b := localBackendWithStore(&runExecFlags{sessionID: "card-1"}, store)

		got, ok := b.ResumeWorkingDir(t.Context())
		assert.True(t, ok)
		assert.Equal(t, dir, got)
	})

	t.Run("no --session", func(t *testing.T) {
		b := localBackendWithStore(&runExecFlags{}, session.NewInMemorySessionStore())
		_, ok := b.ResumeWorkingDir(t.Context())
		assert.False(t, ok)
	})

	t.Run("relative ref is resume-only and not peeked", func(t *testing.T) {
		b := localBackendWithStore(&runExecFlags{sessionID: "-1"}, session.NewInMemorySessionStore())
		_, ok := b.ResumeWorkingDir(t.Context())
		assert.False(t, ok)
	})

	t.Run("unknown ID", func(t *testing.T) {
		b := localBackendWithStore(&runExecFlags{sessionID: "missing"}, session.NewInMemorySessionStore())
		_, ok := b.ResumeWorkingDir(t.Context())
		assert.False(t, ok)
	})

	t.Run("session without a stored working dir", func(t *testing.T) {
		store := session.NewInMemorySessionStore()
		require.NoError(t, store.AddSession(t.Context(), session.New(session.WithID("card-2"))))

		b := localBackendWithStore(&runExecFlags{sessionID: "card-2"}, store)
		_, ok := b.ResumeWorkingDir(t.Context())
		assert.False(t, ok)
	})

	t.Run("stored dir that no longer exists", func(t *testing.T) {
		store := session.NewInMemorySessionStore()
		require.NoError(t, store.AddSession(t.Context(), session.New(
			session.WithID("card-3"),
			session.WithWorkingDir("/no/such/directory"),
		)))

		b := localBackendWithStore(&runExecFlags{sessionID: "card-3"}, store)
		_, ok := b.ResumeWorkingDir(t.Context())
		assert.False(t, ok)
	})
}
