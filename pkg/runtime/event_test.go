package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/session"
)

func TestSessionUsageCarriesCompactionThreshold(t *testing.T) {
	t.Parallel()

	sess := session.New(session.WithUserMessage("hi"))
	sess.InputTokens = 600
	sess.OutputTokens = 400

	u := SessionUsage(sess, 100_000)
	assert.Zero(t, u.CompactionThreshold, "threshold is 0 (unknown) when omitted")

	u = SessionUsage(sess, 100_000, 0.75)
	assert.InDelta(t, 0.75, u.CompactionThreshold, 0.0001)
	assert.Equal(t, int64(100_000), u.ContextLimit)
	assert.Equal(t, int64(1_000), u.ContextLength)
}

// costItem returns an assistant-message item carrying the given cost.
func costItem(agentName string, cost float64) session.Item {
	return session.Item{Message: &session.Message{
		AgentName: agentName,
		Message:   chat.Message{Role: chat.MessageRoleAssistant, Content: "done", Cost: cost},
	}}
}

// TestSessionUsageCostIncludesEmbeddedSubSessions reproduces the restored
// session shape: the root's direct cost plus an embedded sub-session that
// will never emit its own events. The emitted cost must include both, and a
// post-compaction emission (see compactWithReason) must increase it by the
// summary cost instead of dropping back to the root's own cost.
func TestSessionUsageCostIncludesEmbeddedSubSessions(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sess.Messages = append(sess.Messages, costItem("root", 0.10))
	sub := session.New(session.WithParentID(sess.ID))
	sub.Messages = append(sub.Messages, costItem("developer", 0.05))
	sess.Messages = append(sess.Messages, session.Item{SubSession: sub})

	u := SessionUsage(sess, 100_000)
	assert.InDelta(t, 0.15, u.Cost, 1e-9, "restored total = own + embedded sub-session cost")

	sess.ApplyCompaction(100, 0, session.Item{Summary: "summary", Cost: 0.01})
	u = SessionUsage(sess, 100_000)
	assert.InDelta(t, 0.16, u.Cost, 1e-9, "compaction increases the emitted total")
}

// TestSessionUsageCostExcludesLiveSubSessions pins the live aggregation
// contract: a sub-session that ran during this process reported its own cost
// through its own events, so attaching it to the parent must not inflate the
// parent's emitted cost.
func TestSessionUsageCostExcludesLiveSubSessions(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sess.Messages = append(sess.Messages, costItem("root", 0.10))

	child := session.New(session.WithParentID(sess.ID))
	child.Messages = append(child.Messages, costItem("developer", 0.05))
	assert.InDelta(t, 0.05, SessionUsage(child, 100_000).Cost, 1e-9, "the child emits its own cost")

	sess.AddLiveSubSession(child)
	assert.InDelta(t, 0.10, SessionUsage(sess, 100_000).Cost, 1e-9, "the parent keeps emitting only its own cost")
}
