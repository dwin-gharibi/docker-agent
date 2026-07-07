package sidebar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/builtin/todo"
)

// newVisibilityTestSidebar builds a sidebar with data in every optional
// section so hiding each one is observable.
func newVisibilityTestSidebar(tb testing.TB) *testSidebar {
	tb.Helper()
	s := newTestSidebar(tb)
	s.sessionState.SetCurrentAgentName("root")
	s.SetTeamInfo([]runtime.AgentDetails{{Name: "root", Provider: "openai", Model: "gpt-4"}})
	s.SetToolsetInfo(12, false)
	s.recordUsageTokens("session-1", "root", 500, 500)
	return s
}

func renderedSections(s *testSidebar) string {
	return ansi.Strip(strings.Join(s.renderSections(40), "\n"))
}

func TestRenderSections_AllVisibleByDefault(t *testing.T) {
	t.Parallel()

	s := newVisibilityTestSidebar(t)
	out := renderedSections(s)

	assert.Contains(t, out, "Token Usage")
	assert.Contains(t, out, "Agent")
	assert.Contains(t, out, "Tools")
	assert.Contains(t, out, "12 tools available")
}

func TestRenderSections_HideUsage(t *testing.T) {
	t.Parallel()

	s := newVisibilityTestSidebar(t)
	s.SetSectionVisibility(SectionVisibility{HideUsage: true})
	out := renderedSections(s)

	assert.NotContains(t, out, "Token Usage")
	assert.Contains(t, out, "Tools", "other sections stay visible")
}

func TestRenderSections_HideTools(t *testing.T) {
	t.Parallel()

	s := newVisibilityTestSidebar(t)
	s.SetSectionVisibility(SectionVisibility{HideTools: true})
	out := renderedSections(s)

	assert.NotContains(t, out, "12 tools available")
	assert.Contains(t, out, "Token Usage")
}

func TestRenderSections_HideAgentsClearsClickZones(t *testing.T) {
	t.Parallel()

	s := newVisibilityTestSidebar(t)

	s.renderSections(40)
	assert.NotEmpty(t, s.agentClickZones, "visible agents register click zones")

	s.SetSectionVisibility(SectionVisibility{HideAgents: true})
	out := renderedSections(s)

	assert.NotContains(t, out, "openai/gpt-4")
	assert.Empty(t, s.agentClickZones, "hidden agents must not keep stale click zones")
}

func TestCollapsedViewModel_HideUsage(t *testing.T) {
	t.Parallel()

	s := newVisibilityTestSidebar(t)

	vm := s.computeCollapsedViewModel(60)
	assert.NotEmpty(t, vm.UsageSummary)

	s.SetSectionVisibility(SectionVisibility{HideUsage: true})
	vm = s.computeCollapsedViewModel(60)
	assert.Empty(t, vm.UsageSummary, "collapsed band omits usage when hidden")
}

func TestCollapsedInfoLine_ShowsAgentsToolsTodos(t *testing.T) {
	t.Parallel()

	s := newVisibilityTestSidebar(t)
	require.NoError(t, s.SetTodos(&tools.ToolCallResult{Meta: []todo.Todo{
		{Description: "first", Status: "completed"},
		{Description: "second", Status: "pending"},
	}}))

	info := ansi.Strip(s.collapsedInfoLine())
	assert.Contains(t, info, "▶ root")
	assert.Contains(t, info, "12 tools")
	assert.Contains(t, info, "1/2 todos")

	vm := s.computeCollapsedViewModel(60)
	assert.NotEmpty(t, vm.InfoLine, "band view model carries the info line")
}

func TestCollapsedInfoLine_HonorsVisibility(t *testing.T) {
	t.Parallel()

	s := newVisibilityTestSidebar(t)
	require.NoError(t, s.SetTodos(&tools.ToolCallResult{Meta: []todo.Todo{
		{Description: "first", Status: "pending"},
	}}))

	s.SetSectionVisibility(SectionVisibility{HideAgents: true, HideTodos: true})
	info := ansi.Strip(s.collapsedInfoLine())
	assert.NotContains(t, info, "▶ root")
	assert.NotContains(t, info, "todos")
	assert.Contains(t, info, "12 tools", "tools stay visible")

	s.SetSectionVisibility(SectionVisibility{HideAgents: true, HideTools: true, HideTodos: true})
	assert.Empty(t, s.collapsedInfoLine(), "hiding every section removes the line")
}

func TestCollapsedLineCount_GrowsWithInfoLine(t *testing.T) {
	t.Parallel()

	s := newVisibilityTestSidebar(t)

	withInfo := s.computeCollapsedViewModel(60).LineCount()
	s.SetSectionVisibility(SectionVisibility{HideAgents: true, HideTools: true, HideTodos: true})
	withoutInfo := s.computeCollapsedViewModel(60).LineCount()

	assert.Equal(t, withoutInfo+1, withInfo, "the info line adds one band line")
}

func TestSetSectionVisibility_NoopWhenUnchanged(t *testing.T) {
	t.Parallel()

	s := newTestSidebar(t)
	s.renderSections(40)
	s.cacheDirty = false

	s.SetSectionVisibility(SectionVisibility{})
	assert.False(t, s.cacheDirty, "identical visibility must not invalidate the cache")

	s.SetSectionVisibility(SectionVisibility{HideTodos: true})
	assert.True(t, s.cacheDirty)
}

func TestSetMirroredPadding_SwapsEdgePadding(t *testing.T) {
	t.Parallel()

	s := newTestSidebar(t)
	defaults := DefaultLayoutConfig()

	s.SetMirroredPadding(true)
	assert.Equal(t, defaults.PaddingRight, s.layoutCfg.PaddingLeft, "left padding moves to the chat side")
	assert.Equal(t, defaults.PaddingLeft, s.layoutCfg.PaddingRight)

	s.cacheDirty = false
	s.SetMirroredPadding(true)
	assert.False(t, s.cacheDirty, "reapplying the same padding must not invalidate the cache")

	s.SetMirroredPadding(false)
	assert.Equal(t, defaults, s.layoutCfg)
}
