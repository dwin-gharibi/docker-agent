package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/session"
)

// TestPersistenceObserver_PersistsErrorEvent verifies that an ErrorEvent is
// recorded as a session error item so it survives a reload and is included in
// a JSON export.
func TestPersistenceObserver_PersistsErrorEvent(t *testing.T) {
	t.Parallel()

	store := session.NewInMemorySessionStore()
	obs := newPersistenceObserver(store)
	require.NotNil(t, obs)

	sess := session.New(session.WithID("s1"), session.WithUserMessage("hi"))
	require.NoError(t, store.AddSession(t.Context(), sess))

	ev := ErrorWithCodeForSession(sess.ID, ErrorCodeModelError, "model stream failed")
	obs.OnEvent(t.Context(), sess, ev)

	reloaded, err := store.GetSession(t.Context(), sess.ID)
	require.NoError(t, err)

	var errItem *session.Error
	for _, item := range reloaded.Messages {
		if item.IsError() {
			errItem = item.Error
			break
		}
	}
	require.NotNil(t, errItem, "ErrorEvent must be persisted as an error item")
	assert.Equal(t, "model stream failed", errItem.Message)
	assert.Equal(t, ErrorCodeModelError, errItem.Code)
}

// TestPersistenceObserver_SkipsSubSessionError verifies that errors from
// sub-sessions are not persisted into the parent (the parent absorbs only its
// own scoped events).
func TestPersistenceObserver_SkipsSubSessionError(t *testing.T) {
	t.Parallel()

	store := session.NewInMemorySessionStore()
	obs := newPersistenceObserver(store)
	require.NotNil(t, obs)

	sess := session.New(session.WithID("parent"), session.WithUserMessage("hi"))
	require.NoError(t, store.AddSession(t.Context(), sess))

	// An error tagged with a different session id must be filtered out.
	ev := ErrorWithCodeForSession("other-session", ErrorCodeModelError, "child failed")
	obs.OnEvent(t.Context(), sess, ev)

	reloaded, err := store.GetSession(t.Context(), sess.ID)
	require.NoError(t, err)
	for _, item := range reloaded.Messages {
		assert.False(t, item.IsError(), "sub-session error must not be persisted into parent")
	}
}
