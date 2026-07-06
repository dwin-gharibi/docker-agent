package sidebar

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/runtime"
)

// recordAgentUsage feeds a TokenUsageEvent through the sidebar's normal entry
// point, as the chat page does for live, sub-session, and background events.
func recordAgentUsage(m *model, sessionID, agentName string, contextLen, contextLimit int64) {
	m.SetTokenUsage(&runtime.TokenUsageEvent{
		SessionID:    sessionID,
		AgentContext: runtime.AgentContext{AgentName: agentName},
		Usage: &runtime.Usage{
			InputTokens:   contextLen / 2,
			OutputTokens:  contextLen - contextLen/2,
			ContextLength: contextLen,
			ContextLimit:  contextLimit,
		},
	})
}

// TestAgentRosterShowsPerAgentContextPercent verifies line 2 of each roster
// entry carries the agent's own context percentage, right-aligned, once that
// agent has emitted usage — and stays bare for agents that have not run.
func TestAgentRosterShowsPerAgentContextPercent(t *testing.T) {
	t.Parallel()

	m := newAgentPanelSidebar(t, 40,
		runtime.AgentDetails{Name: "root", Provider: "anthropic", Model: "opus", Thinking: "high"},
		runtime.AgentDetails{Name: "developer", Provider: "openai", Model: "gpt-5.4", Thinking: "off"},
		runtime.AgentDetails{Name: "idle", Provider: "openai", Model: "gpt-4o", Thinking: "off"},
	)

	recordAgentUsage(m, "session-root", "root", 30_000, 100_000)
	recordAgentUsage(m, "session-child", "developer", 42_000, 100_000)

	_, rootLine2 := agentLines(m, "root")
	assert.True(t, strings.HasSuffix(strings.TrimRight(rootLine2, " "), "30%"),
		"root's line 2 ends with its context percent, got %q", rootLine2)
	assert.Contains(t, rootLine2, "anthropic/opus", "model text stays on line 2")

	_, devLine2 := agentLines(m, "developer")
	assert.True(t, strings.HasSuffix(strings.TrimRight(devLine2, " "), "42%"),
		"developer's line 2 ends with its context percent, got %q", devLine2)

	_, idleLine2 := agentLines(m, "idle")
	assert.NotContains(t, idleLine2, "%", "agents that never ran show no percent")
}

// TestAgentContextPercent_LatestSnapshotWins verifies a fresh delegation to the
// same agent (new sub-session) replaces its displayed context usage.
func TestAgentContextPercent_LatestSnapshotWins(t *testing.T) {
	t.Parallel()

	m := newAgentPanelSidebar(t, 40,
		runtime.AgentDetails{Name: "root", Provider: "anthropic", Model: "opus"},
	)

	recordAgentUsage(m, "session-1", "root", 80_000, 100_000)
	assert.Equal(t, "80%", m.agentContextPercent("root"))

	recordAgentUsage(m, "session-2", "root", 10_000, 100_000)
	assert.Equal(t, "10%", m.agentContextPercent("root"))
}

// TestAgentContextPercent_UnknownLimit verifies no percent is shown when the
// context limit is unknown (e.g. harness-backed agents) or nothing was
// recorded for the agent.
func TestAgentContextPercent_UnknownLimit(t *testing.T) {
	t.Parallel()

	m := newAgentPanelSidebar(t, 40,
		runtime.AgentDetails{Name: "root", Provider: "anthropic", Model: "opus"},
	)

	assert.Empty(t, m.agentContextPercent("root"), "no usage recorded yet")

	recordAgentUsage(m, "session-1", "root", 30_000, 0)
	assert.Empty(t, m.agentContextPercent("root"), "no percent without a context limit")

	_, line2 := agentLines(m, "root")
	assert.NotContains(t, line2, "%")
}

// TestAgentContextPercent_BackgroundAgentUsage verifies a usage event from a
// detached background sub-session (forwarded out-of-band by the runtime, with
// its own session id) surfaces on the background agent's roster line without
// disturbing the parent's active-session accounting.
func TestAgentContextPercent_BackgroundAgentUsage(t *testing.T) {
	t.Parallel()

	m := newAgentPanelSidebar(t, 40,
		runtime.AgentDetails{Name: "root", Provider: "anthropic", Model: "opus"},
		runtime.AgentDetails{Name: "worker", Provider: "openai", Model: "gpt-5.4"},
	)

	// Parent runs on the main session; the background worker's events carry
	// its own sub-session id and interleave with the parent's.
	m.rootSessionID = "session-root"
	recordAgentUsage(m, "session-root", "root", 20_000, 100_000)
	recordAgentUsage(m, "bg-session", "worker", 55_000, 100_000)

	_, workerLine2 := agentLines(m, "worker")
	assert.True(t, strings.HasSuffix(strings.TrimRight(workerLine2, " "), "55%"),
		"background worker's line 2 ends with its context percent, got %q", workerLine2)

	assert.Equal(t, "20%", m.contextPercent(),
		"the main context gauge keeps tracking the active session, not the background one")
}

// TestAgentRosterLine2ReservesRoomForPercent verifies the model text yields
// space to the percent instead of colliding with it at narrow widths.
func TestAgentRosterLine2ReservesRoomForPercent(t *testing.T) {
	t.Parallel()

	m := newAgentPanelSidebar(t, 28,
		runtime.AgentDetails{Name: "root", Provider: "anthropic", Model: "claude-sonnet-4-6"},
	)
	recordAgentUsage(m, "session-1", "root", 100_000, 100_000)

	_, line2 := agentLines(m, "root")
	require.NotEmpty(t, line2)
	trimmed := strings.TrimRight(line2, " ")
	assert.True(t, strings.HasSuffix(trimmed, " 100%"),
		"percent stays separated from the truncated model, got %q", line2)
	assert.Contains(t, line2, "…", "overflowing model is still left-truncated")
	assert.Contains(t, line2, "-4-6", "informative model tail survives")
}
