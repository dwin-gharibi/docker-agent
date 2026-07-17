package service

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/session"
)

// TestSessionState_AgentUsage covers the per-agent usage snapshot store: set,
// last-write-wins replacement, the empty-name guard, and the miss case.
func TestSessionState_AgentUsage(t *testing.T) {
	t.Parallel()

	s := NewSessionState(nil)

	_, ok := s.AgentUsage("root")
	assert.False(t, ok, "no snapshot before any usage is recorded")

	s.SetAgentUsage("session-1", "root", runtime.Usage{ContextLength: 1000, ContextLimit: 10000})
	usage, ok := s.AgentUsage("root")
	assert.True(t, ok)
	assert.Equal(t, int64(1000), usage.ContextLength)

	s.SetAgentUsage("session-1", "root", runtime.Usage{ContextLength: 2000, ContextLimit: 10000})
	usage, ok = s.AgentUsage("root")
	assert.True(t, ok)
	assert.Equal(t, int64(2000), usage.ContextLength, "latest snapshot wins")

	s.SetAgentUsage("session-1", "", runtime.Usage{ContextLength: 3000})
	_, ok = s.AgentUsage("")
	assert.False(t, ok, "snapshots without an agent name are dropped")
}

// TestSessionState_AgentCost covers the cumulative per-agent cost: unknown
// before any usage, replacement of one session's repeated cumulative
// snapshots, addition across distinct sessions, and the ran-at-zero-cost case
// staying distinct from never-ran.
func TestSessionState_AgentCost(t *testing.T) {
	t.Parallel()

	s := NewSessionState(nil)

	_, ok := s.AgentCost("root")
	assert.False(t, ok, "no cost attributable before any usage is recorded")

	// Repeated cumulative snapshots of one session replace, not add.
	s.SetAgentUsage("session-1", "root", runtime.Usage{Cost: 0.10})
	s.SetAgentUsage("session-1", "root", runtime.Usage{Cost: 0.30})
	cost, ok := s.AgentCost("root")
	assert.True(t, ok)
	assert.InDelta(t, 0.30, cost, 1e-9)

	// A second session for the same agent (sub-session or background task) adds.
	s.SetAgentUsage("session-2", "root", runtime.Usage{Cost: 0.05})
	cost, ok = s.AgentCost("root")
	assert.True(t, ok)
	assert.InDelta(t, 0.35, cost, 1e-9)

	// Other agents stay independent.
	s.SetAgentUsage("session-3", "developer", runtime.Usage{Cost: 0.02})
	cost, ok = s.AgentCost("developer")
	assert.True(t, ok)
	assert.InDelta(t, 0.02, cost, 1e-9)

	// An agent that ran at zero cost is attributed with $0, not unknown.
	s.SetAgentUsage("session-4", "local", runtime.Usage{Cost: 0})
	cost, ok = s.AgentCost("local")
	assert.True(t, ok)
	assert.Zero(t, cost)
}

// TestSessionState_AgentCost_SessionHandoffMovesAttribution documents the
// chosen semantics for a session whose events change agent name mid-stream
// (an in-session handoff): the latest attribution wins, so the whole
// session's cumulative cost moves to the new agent. The snapshots carry no
// per-agent split of a shared session, so this never double-counts — at the
// price of crediting the earlier spend to the agent that finished the
// session, and reporting the earlier agent's cost as unknown again when it
// has no other sessions.
func TestSessionState_AgentCost_SessionHandoffMovesAttribution(t *testing.T) {
	t.Parallel()

	s := NewSessionState(nil)

	s.SetAgentUsage("session-1", "root", runtime.Usage{Cost: 0.10})
	s.SetAgentUsage("session-1", "developer", runtime.Usage{Cost: 0.25})

	cost, ok := s.AgentCost("developer")
	assert.True(t, ok)
	assert.InDelta(t, 0.25, cost, 1e-9, "the session's full cumulative cost follows the newest agent")

	_, ok = s.AgentCost("root")
	assert.False(t, ok, "the previous agent no longer owns any session")
}

// restoredCostMessage returns a message item as persisted for an assistant
// response: agent name, per-message cost, and its usage record (present even
// at zero cost).
func restoredCostMessage(agentName string, cost float64) session.Item {
	return session.Item{Message: &session.Message{
		AgentName: agentName,
		Message: chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "done",
			Cost:    cost,
			Usage:   &chat.Usage{InputTokens: 100, OutputTokens: 20},
		},
	}}
}

// TestSessionState_SeedRestoredCosts covers the restored-cost seeding: exact
// per-agent totals from the session tree including nested sub-sessions, the
// zero-cost vs never-ran distinction, honest non-attribution of agent-less
// and compaction costs, and the replace/reset semantics of repeated seeding.
func TestSessionState_SeedRestoredCosts(t *testing.T) {
	t.Parallel()

	// root ($0.02 + $0.03) and a zero-cost local agent directly in the root
	// session; developer ($0.05) in a sub-session holding a nested
	// sub-sub-session for writer ($0.01); an agent-less message ($0.04) and a
	// compaction summary ($0.02) that both lack agent identity.
	sess := session.New()
	sess.Messages = append(sess.Messages,
		restoredCostMessage("root", 0.02),
		restoredCostMessage("root", 0.03),
		restoredCostMessage("local", 0),
		restoredCostMessage("", 0.04),
		session.Item{Summary: "compacted", Cost: 0.02},
	)
	subSub := session.New()
	subSub.Messages = append(subSub.Messages, restoredCostMessage("writer", 0.01))
	sub := session.New(session.WithParentID(sess.ID))
	sub.Messages = append(sub.Messages, restoredCostMessage("developer", 0.05), session.Item{SubSession: subSub})
	sess.Messages = append(sess.Messages, session.Item{SubSession: sub})

	s := NewSessionState(nil)
	// Live state from a previously shown session must not survive the seed.
	s.SetAgentUsage("old-session", "stale", runtime.Usage{ContextLength: 1000, ContextLimit: 10_000, Cost: 0.50})

	s.SeedRestoredCosts(sess)

	cost, ok := s.AgentCost("root")
	assert.True(t, ok)
	assert.InDelta(t, 0.05, cost, 1e-9, "root's own messages add up")

	cost, ok = s.AgentCost("developer")
	assert.True(t, ok)
	assert.InDelta(t, 0.05, cost, 1e-9, "sub-session spend attributes to its agent")

	cost, ok = s.AgentCost("writer")
	assert.True(t, ok)
	assert.InDelta(t, 0.01, cost, 1e-9, "nested sub-sub-session spend attributes too")

	cost, ok = s.AgentCost("local")
	assert.True(t, ok)
	assert.Zero(t, cost, "an agent restored at zero cost is attributed $0, not unknown")

	_, ok = s.AgentCost("idle")
	assert.False(t, ok, "an agent absent from the restored tree stays unknown")

	_, ok = s.AgentCost("stale")
	assert.False(t, ok, "seeding clears the previously shown session's cost state")
	_, ok = s.AgentUsage("stale")
	assert.False(t, ok, "seeding clears usage snapshots too; context is not fabricated")

	// Re-seeding the same session replaces rather than accumulates.
	s.SeedRestoredCosts(sess)
	cost, ok = s.AgentCost("root")
	assert.True(t, ok)
	assert.InDelta(t, 0.05, cost, 1e-9, "repeated seeding does not double values")

	// Seeding with nil resets everything.
	s.SeedRestoredCosts(nil)
	_, ok = s.AgentCost("root")
	assert.False(t, ok)
}

// TestSessionState_SeedRestoredCosts_LiveSnapshotsAddOnlyNewSpend verifies
// the interplay of the restored baseline with subsequent live cumulative
// snapshots: the restored root session's snapshots cover the whole restored
// tree (own + embedded sub-session costs), so only spend beyond the baseline
// is attributed to the live agent, while a fresh live sub-session counts in
// full.
func TestSessionState_SeedRestoredCosts_LiveSnapshotsAddOnlyNewSpend(t *testing.T) {
	t.Parallel()

	// Restored tree: root $0.10, embedded developer sub-session $0.05,
	// unattributed compaction $0.01 — a $0.16 baseline.
	sess := session.New()
	sess.Messages = append(sess.Messages,
		restoredCostMessage("root", 0.10),
		session.Item{Summary: "compacted", Cost: 0.01},
	)
	sub := session.New(session.WithParentID(sess.ID))
	sub.Messages = append(sub.Messages, restoredCostMessage("developer", 0.05))
	sess.Messages = append(sess.Messages, session.Item{SubSession: sub})

	s := NewSessionState(nil)
	s.SeedRestoredCosts(sess)

	// The root session resumes: its live snapshot is cumulative over the
	// restored tree plus $0.02 of new spend.
	s.SetAgentUsage(sess.ID, "root", runtime.Usage{Cost: 0.18})
	cost, ok := s.AgentCost("root")
	assert.True(t, ok)
	assert.InDelta(t, 0.12, cost, 1e-9, "restored total plus only the new spend")

	cost, ok = s.AgentCost("developer")
	assert.True(t, ok)
	assert.InDelta(t, 0.05, cost, 1e-9, "restored sub-session spend keeps its agent")

	// A later cumulative snapshot of the same session replaces, not adds.
	s.SetAgentUsage(sess.ID, "root", runtime.Usage{Cost: 0.20})
	cost, _ = s.AgentCost("root")
	assert.InDelta(t, 0.14, cost, 1e-9)

	// A fresh live sub-session is new spend in full.
	s.SetAgentUsage("live-sub", "developer", runtime.Usage{Cost: 0.03})
	cost, _ = s.AgentCost("developer")
	assert.InDelta(t, 0.08, cost, 1e-9)
}
