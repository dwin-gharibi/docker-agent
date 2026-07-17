package sidebar

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/runtime"
)

func (s *testSidebar) feedBudget(ev *runtime.BudgetUsageEvent) {
	updated, _ := s.Update(tea.Msg(ev))
	s.model = updated.(*model)
}

func TestBudgetLine_AbsentWhenUnbudgeted(t *testing.T) {
	t.Parallel()

	m := newTestSidebar(t)
	m.startStream("s1", "root")
	m.recordUsageTokens("s1", "root", 5000, 3000)

	assert.NotContains(t, ansi.Strip(m.tokenUsage(60)), "/$")
}

func TestBudgetLine_ShowsCeilingsAtZero(t *testing.T) {
	t.Parallel()

	m := newTestSidebar(t)
	m.startStream("s1", "root")
	m.feedBudget(&runtime.BudgetUsageEvent{
		SessionID:    "s1",
		AgentContext: runtime.AgentContext{AgentName: "root"},
		Budgets: []runtime.BudgetStatus{{
			Name: "run", MaxCost: 0.50, MaxTokens: 100000, MaxTimeSeconds: 120,
		}},
	})

	out := ansi.Strip(m.tokenUsage(60))
	assert.Contains(t, out, "run")
	assert.Contains(t, out, "$0.00/$0.50")
	assert.Contains(t, out, "/100.0K")
	assert.Contains(t, out, "/2m0s")
}

func TestBudgetLine_ShowsEachBudgetByName(t *testing.T) {
	t.Parallel()

	m := newTestSidebar(t)
	m.startStream("s1", "root")
	m.feedBudget(&runtime.BudgetUsageEvent{
		SessionID:    "s1",
		AgentContext: runtime.AgentContext{AgentName: "developer"},
		Budgets: []runtime.BudgetStatus{
			{Name: "run", Cost: 0.12, MaxCost: 0.50},
			{Name: "tight", Cost: 0.09, MaxCost: 0.10},
		},
	})

	out := ansi.Strip(m.tokenUsage(80))
	assert.Contains(t, out, "run")
	assert.Contains(t, out, "$0.12/$0.50")
	assert.Contains(t, out, "tight")
	assert.Contains(t, out, "$0.09/$0.10")
	assert.Less(t, strings.Index(out, "run"), strings.Index(out, "tight"),
		"run-wide budget leads, then named budgets")
}

func TestBudgetLine_SingleAgentHasNoBreakdown(t *testing.T) {
	t.Parallel()

	m := newTestSidebar(t)
	m.startStream("s1", "root")
	m.feedBudget(&runtime.BudgetUsageEvent{
		SessionID:    "s1",
		AgentContext: runtime.AgentContext{AgentName: "root"},
		Budgets: []runtime.BudgetStatus{{
			Name: "run", Cost: 0.03, MaxCost: 0.50,
			PerAgent: []runtime.AgentBudgetUsage{
				{AgentName: "solo-agent", Cost: 0.03, Tokens: 8000, ActiveSeconds: 12},
			},
		}},
	})

	out := ansi.Strip(m.tokenUsage(60))
	assert.NotContains(t, out, "solo-agent", "single agent: no per-agent row")
}

func TestBudgetLine_MultiAgentBreakdownPerBudget(t *testing.T) {
	t.Parallel()

	m := newTestSidebar(t)
	m.startStream("s1", "root")
	m.feedBudget(&runtime.BudgetUsageEvent{
		SessionID:    "s1",
		AgentContext: runtime.AgentContext{AgentName: "developer"},
		Budgets: []runtime.BudgetStatus{{
			Name: "run", Cost: 0.12, MaxCost: 0.50, Tokens: 12300, MaxTokens: 100000,
			PerAgent: []runtime.AgentBudgetUsage{
				{AgentName: "developer", Cost: 0.09, Tokens: 8000, ActiveSeconds: 72},
				{AgentName: "root", Cost: 0.03, Tokens: 4300, ActiveSeconds: 62},
			},
		}},
	})

	out := ansi.Strip(m.tokenUsage(80))
	assert.Contains(t, out, "developer")
	assert.Contains(t, out, "$0.09")
	assert.Contains(t, out, "1m12s")
	assert.Contains(t, out, "$0.03")
	assert.Contains(t, out, "1m02s")
	assert.Less(t, strings.Index(out, "$0.09"), strings.Index(out, "$0.03"),
		"biggest spender must lead")
}

func TestBudgetLine_NoCostCeilingOmitsAgentCost(t *testing.T) {
	t.Parallel()

	m := newTestSidebar(t)
	m.startStream("s1", "root")
	m.feedBudget(&runtime.BudgetUsageEvent{
		SessionID:    "s1",
		AgentContext: runtime.AgentContext{AgentName: "root"},
		Budgets: []runtime.BudgetStatus{{
			Name: "roomy", Tokens: 500, MaxTokens: 1000,
			PerAgent: []runtime.AgentBudgetUsage{
				{AgentName: "developer", Tokens: 300, ActiveSeconds: 5},
				{AgentName: "root", Tokens: 200, ActiveSeconds: 3},
			},
		}},
	})

	out := ansi.Strip(m.tokenUsage(80))
	assert.Contains(t, out, "roomy")
	assert.NotContains(t, out, "$", "a tokens-only budget must not render costs")
}

func TestBudgetLine_MarksUnpricedSpend(t *testing.T) {
	t.Parallel()

	m := newTestSidebar(t)
	m.startStream("s1", "root")
	m.feedBudget(&runtime.BudgetUsageEvent{
		SessionID:    "s1",
		AgentContext: runtime.AgentContext{AgentName: "root"},
		Budgets: []runtime.BudgetStatus{{
			Name: "run", Cost: 0, MaxCost: 0.50, Unpriced: true,
		}},
	})

	assert.Contains(t, ansi.Strip(m.tokenUsage(80)), "unpriced spend")
}

func TestBudgetLine_UpdatesLive(t *testing.T) {
	t.Parallel()

	m := newTestSidebar(t)
	m.startStream("s1", "root")
	m.feedBudget(&runtime.BudgetUsageEvent{
		SessionID: "s1", AgentContext: runtime.AgentContext{AgentName: "root"},
		Budgets: []runtime.BudgetStatus{{Name: "run", Cost: 0.01, MaxCost: 0.50}},
	})
	assert.Contains(t, ansi.Strip(m.tokenUsage(60)), "$0.01/$0.50")

	m.feedBudget(&runtime.BudgetUsageEvent{
		SessionID: "s1", AgentContext: runtime.AgentContext{AgentName: "root"},
		Budgets: []runtime.BudgetStatus{{Name: "run", Cost: 0.44, MaxCost: 0.50}},
	})
	out := ansi.Strip(m.tokenUsage(60))
	assert.Contains(t, out, "$0.44/$0.50")
	assert.NotContains(t, out, "$0.01/$0.50", "stale reading must be replaced")
}
