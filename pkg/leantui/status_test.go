package leantui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/leantui/ui"
	"github.com/docker/docker-agent/pkg/runtime"
)

func TestAgentInfoContextLimitShownBeforeUsage(t *testing.T) {
	t.Parallel()
	m := bareModel(24)

	m.handleEvent(t.Context(), runtime.AgentInfo("root", "test/model", "", "", 200_000))

	assert.Equal(t, int64(200_000), m.status.ContextLimit)
	assert.Contains(t, ui.RenderContext(m.status), "0% · 0/200.0k")
}

func TestTokenUsageEventAggregatesSessionCost(t *testing.T) {
	t.Parallel()
	m := bareModel(24)

	m.handleEvent(t.Context(), runtime.StreamStarted("root-session", "root"))
	m.handleEvent(t.Context(), runtime.NewTokenUsageEvent("root-session", "root", &runtime.Usage{
		InputTokens:   2_000,
		OutputTokens:  1_000,
		ContextLength: 3_000,
		ContextLimit:  10_000,
		Cost:          0.10,
	}))
	m.handleEvent(t.Context(), runtime.StreamStarted("child-session", "developer"))
	m.handleEvent(t.Context(), runtime.NewTokenUsageEvent("child-session", "developer", &runtime.Usage{
		InputTokens:   800,
		OutputTokens:  200,
		ContextLength: 1_000,
		ContextLimit:  20_000,
		Cost:          0.05,
	}))

	assert.Equal(t, int64(1_000), m.status.Tokens)
	assert.InDelta(t, 0.15, m.status.Cost, 0.0001)
	assert.True(t, m.status.CostKnown)
	assert.Contains(t, strings.Join(ui.RenderStatus(m.status, 80), "\n"), "$0.15")

	m.handleEvent(t.Context(), runtime.StreamStopped("child-session", "developer", "normal"))

	assert.Equal(t, int64(3_000), m.status.Tokens)
	assert.InDelta(t, 0.15, m.status.Cost, 0.0001)
}

func TestTokenUsageBeforeStreamUsesFirstSessionAsRoot(t *testing.T) {
	t.Parallel()
	m := bareModel(24)

	m.handleEvent(t.Context(), runtime.NewTokenUsageEvent("root-session", "root", &runtime.Usage{
		InputTokens:   2_000,
		OutputTokens:  1_000,
		ContextLength: 3_000,
		ContextLimit:  10_000,
		Cost:          0.10,
	}))
	m.handleEvent(t.Context(), runtime.NewTokenUsageEvent("child-session", "developer", &runtime.Usage{
		InputTokens:   800,
		OutputTokens:  200,
		ContextLength: 1_000,
		ContextLimit:  20_000,
		Cost:          0.05,
	}))

	assert.Equal(t, "root-session", m.usage.RootSessionID())
	assert.Equal(t, int64(3_000), m.status.Tokens)
	assert.InDelta(t, 0.15, m.status.Cost, 0.0001)
}

func TestEmptySessionUsageDoesNotOverrideSessionScopedUsage(t *testing.T) {
	t.Parallel()
	m := bareModel(24)

	m.handleEvent(t.Context(), runtime.NewTokenUsageEvent("root-session", "root", &runtime.Usage{
		InputTokens:   2_000,
		OutputTokens:  1_000,
		ContextLength: 3_000,
		ContextLimit:  10_000,
		Cost:          0.10,
	}))
	m.handleEvent(t.Context(), runtime.NewTokenUsageEvent("", "root", &runtime.Usage{
		InputTokens:   50,
		ContextLength: 50,
		Cost:          0.99,
	}))

	assert.Equal(t, int64(3_000), m.status.Tokens)
	assert.InDelta(t, 0.10, m.status.Cost, 0.0001)
}
