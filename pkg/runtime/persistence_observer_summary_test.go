package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/session"
)

// TestPersistenceObserver_PersistsSummaryCost verifies the cost carried by a
// SessionSummaryEvent reaches the persisted summary item. Without it, a
// reloaded session under-reports TotalCost by everything compaction billed.
func TestPersistenceObserver_PersistsSummaryCost(t *testing.T) {
	t.Parallel()

	store := session.NewInMemorySessionStore()
	obs := newPersistenceObserver(store)
	require.NotNil(t, obs)

	sess := session.New(session.WithID("s1"), session.WithUserMessage("hi"))
	require.NoError(t, store.AddSession(t.Context(), sess))

	obs.OnEvent(t.Context(), sess, SessionSummary(sess.ID, "the summary", "root", 1, 0.002))

	reloaded, err := store.GetSession(t.Context(), sess.ID)
	require.NoError(t, err)

	last := reloaded.Messages[len(reloaded.Messages)-1]
	require.Equal(t, "the summary", last.Summary)
	assert.Equal(t, 1, last.FirstKeptEntry)
	assert.InDelta(t, 0.002, last.Cost, 1e-9, "the event's cost must land on the summary item")
}
