package sidebar

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tui/service"
)

func TestSidebar_HandleClickType_Agent(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	sessionState.SetCurrentAgentName("agent1")
	sb := New(sessionState)

	m := sb.(*model)
	m.sessionHasContent = true
	m.titleGenerated = true
	m.sessionTitle = "Test"
	m.currentAgent = "agent1"
	m.availableAgents = []runtime.AgentDetails{
		{Name: "agent1", Provider: "openai", Model: "gpt-4", Description: "First agent"},
		{Name: "agent2", Provider: "anthropic", Model: "claude", Description: "Second agent"},
	}
	m.width = 40
	m.height = 50

	// Force a render to populate agentClickZones
	_ = sb.View()

	paddingLeft := m.layoutCfg.PaddingLeft

	// Verify clicking on agent1 lines returns ClickAgent with "agent1"
	foundAgent1 := false
	foundAgent2 := false
	for y := range len(m.cachedLines) {
		result, agentName := sb.HandleClickType(paddingLeft+2, y)
		if result == ClickAgent {
			if agentName == "agent1" {
				foundAgent1 = true
			}
			if agentName == "agent2" {
				foundAgent2 = true
			}
		}
	}
	assert.True(t, foundAgent1, "should be able to click on agent1")
	assert.True(t, foundAgent2, "should be able to click on agent2")
}

// TestSidebar_AgentClickZones_EveryRenderedLineMapped verifies that every
// rendered line an agent emits is registered as a click zone for that agent.
// The mapping is produced explicitly during rendering (agentLineOwners), so a
// multi-line agent block is fully clickable, not just its first line.
func TestSidebar_AgentClickZones_EveryRenderedLineMapped(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	sessionState.SetCurrentAgentName("agent1")
	sb := New(sessionState)

	m := sb.(*model)
	m.sessionHasContent = true
	m.titleGenerated = true
	m.sessionTitle = "Test"
	m.currentAgent = "agent1"
	m.availableAgents = []runtime.AgentDetails{
		{Name: "agent1", Provider: "openai", Model: "gpt-4", Description: "First agent", Thinking: "high"},
		{Name: "agent2", Provider: "anthropic", Model: "claude", Description: "Second agent", Thinking: "off"},
	}
	m.width = 40
	m.height = 50

	_ = sb.View()

	// Each agent contributes at least one non-blank owned line.
	counts := map[string]int{}
	for _, owner := range m.agentLineOwners {
		if owner != "" {
			counts[owner]++
		}
	}
	assert.Positive(t, counts["agent1"], "agent1 should own rendered lines")
	assert.Positive(t, counts["agent2"], "agent2 should own rendered lines")
	// agent2 is a non-current roster agent: its row spans two lines (name+badge
	// then the indented model), and BOTH must map to it so a click on either
	// switches to the agent.
	assert.Equal(t, 2, counts["agent2"], "a roster agent owns both of its two row lines")

	// The number of click zones equals the number of owned (non-blank) lines:
	// every owned line is clickable.
	owned := 0
	for _, owner := range m.agentLineOwners {
		if owner != "" {
			owned++
		}
	}
	assert.Len(t, m.agentClickZones, owned, "every owned line should be a click zone")
}

// TestSidebar_BuildAgentClickZones_NoBlankSeparators verifies the click-zone
// builder relies purely on explicit per-line ownership, not on blank-line
// separators. A compact roster with one line per agent and no blank lines
// between them must still map each line to the correct agent.
func TestSidebar_BuildAgentClickZones_NoBlankSeparators(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	m := New(sessionState).(*model)

	// Simulate a compact roster: three agents, one rendered line each, no
	// blank separators (the future layout this refactor unblocks).
	m.agentLineOwners = []string{"agent1", "agent2", "agent3"}

	const agentSectionStart = 5
	m.buildAgentClickZones(agentSectionStart)

	const tabHeaderLines = 2
	require.Len(t, m.agentClickZones, 3)
	assert.Equal(t, "agent1", m.agentClickZones[agentSectionStart+tabHeaderLines+0])
	assert.Equal(t, "agent2", m.agentClickZones[agentSectionStart+tabHeaderLines+1])
	assert.Equal(t, "agent3", m.agentClickZones[agentSectionStart+tabHeaderLines+2])
}

// TestSidebar_BuildAgentClickZones_SkipsBlankOwners verifies that blank
// separator lines (empty owner) are not registered as click zones, while the
// surrounding owned lines keep their correct content-line offsets.
func TestSidebar_BuildAgentClickZones_SkipsBlankOwners(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sessionState := service.NewSessionState(sess)
	m := New(sessionState).(*model)

	// agent1 spans two lines, a blank separator follows, then agent2.
	m.agentLineOwners = []string{"agent1", "agent1", "", "agent2"}

	const agentSectionStart = 0
	m.buildAgentClickZones(agentSectionStart)

	const tabHeaderLines = 2
	require.Len(t, m.agentClickZones, 3)
	assert.Equal(t, "agent1", m.agentClickZones[tabHeaderLines+0])
	assert.Equal(t, "agent1", m.agentClickZones[tabHeaderLines+1])
	_, blankMapped := m.agentClickZones[tabHeaderLines+2]
	assert.False(t, blankMapped, "blank separator line should not be clickable")
	assert.Equal(t, "agent2", m.agentClickZones[tabHeaderLines+3])
}
