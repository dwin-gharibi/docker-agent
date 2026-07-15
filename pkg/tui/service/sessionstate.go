package service

import (
	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tui/styles"
	"github.com/docker/docker-agent/pkg/tui/types"
	"github.com/docker/docker-agent/pkg/userconfig"
)

// PauseState describes how /pause is currently affecting the runtime loop
// for a session, so the TUI can show whether the system is winding down a
// roundtrip before pausing or fully paused.
type PauseState int

const (
	// PauseNone means the runtime is not paused.
	PauseNone PauseState = iota
	// PausePausing means /pause was requested but the runtime is still
	// finishing the in-flight LLM request and its tool calls.
	PausePausing
	// PausePaused means the runtime has reached an iteration boundary and is
	// idle until the user resumes.
	PausePaused
)

// SessionStateReader provides read-only access to session state.
// Components that only need to read state should depend on this interface
// rather than the full SessionState, following the principle of least privilege.
type SessionStateReader interface {
	SplitDiffView() bool
	ExpandThinking() bool
	YoloMode() bool
	HideToolResults() bool
	CurrentAgentName() string
	PreviousMessage() *types.Message
	SessionTitle() string
	AvailableAgents() []runtime.AgentDetails
	GetCurrentAgent() runtime.AgentDetails
	PauseState() PauseState
}

// Verify SessionState implements SessionStateReader
var _ SessionStateReader = (*SessionState)(nil)

// SessionState holds shared state across the TUI application.
// This provides a centralized location for state that needs to be
// accessible by multiple components.
type SessionState struct {
	splitDiffView   bool
	expandThinking  bool
	yoloMode        bool
	hideToolResults bool
	sessionTitle    string

	previousMessage  *types.Message
	currentAgentName string
	availableAgents  []runtime.AgentDetails
	pauseState       PauseState

	// agentUsage holds the latest token-usage snapshot per agent name,
	// fed by TokenUsageEvents (including those of sub-sessions and
	// background agent tasks). It backs the per-agent context display in
	// the sidebar roster and the agent-details dialog.
	agentUsage map[string]runtime.Usage
}

func NewSessionState(s *session.Session) *SessionState {
	settings := userconfig.Get()
	state := &SessionState{
		splitDiffView:  settings.GetSplitDiffView(),
		expandThinking: settings.GetExpandThinking(),
	}
	if s != nil {
		state.yoloMode = s.ToolsApproved
		state.hideToolResults = s.HideToolResults
		state.sessionTitle = s.Title
	}
	return state
}

func (s *SessionState) SplitDiffView() bool {
	return s.splitDiffView
}

func (s *SessionState) ExpandThinking() bool {
	if s == nil {
		return true
	}
	return s.expandThinking
}

func (s *SessionState) SetExpandThinking(expandThinking bool) {
	s.expandThinking = expandThinking
}

func (s *SessionState) ToggleSplitDiffView() {
	s.splitDiffView = !s.splitDiffView
}

func (s *SessionState) SetSplitDiffView(enabled bool) {
	s.splitDiffView = enabled
}

func (s *SessionState) YoloMode() bool {
	return s.yoloMode
}

func (s *SessionState) SetYoloMode(yoloMode bool) {
	s.yoloMode = yoloMode
}

func (s *SessionState) HideToolResults() bool {
	return s.hideToolResults
}

func (s *SessionState) ToggleHideToolResults() {
	s.hideToolResults = !s.hideToolResults
}

func (s *SessionState) SetHideToolResults(hideToolResults bool) {
	s.hideToolResults = hideToolResults
}

func (s *SessionState) CurrentAgentName() string {
	return s.currentAgentName
}

func (s *SessionState) SetCurrentAgentName(currentAgentName string) {
	s.currentAgentName = currentAgentName
}

func (s *SessionState) PreviousMessage() *types.Message {
	return s.previousMessage
}

func (s *SessionState) SetPreviousMessage(previousMessage *types.Message) {
	s.previousMessage = previousMessage
}

func (s *SessionState) SessionTitle() string {
	return s.sessionTitle
}

func (s *SessionState) SetSessionTitle(sessionTitle string) {
	s.sessionTitle = sessionTitle
}

func (s *SessionState) AvailableAgents() []runtime.AgentDetails {
	return s.availableAgents
}

func (s *SessionState) SetAvailableAgents(availableAgents []runtime.AgentDetails) {
	s.availableAgents = availableAgents

	names := make([]string, len(availableAgents))
	for i, a := range availableAgents {
		names[i] = a.Name
	}
	styles.SetAgentOrder(names)
}

func (s *SessionState) PauseState() PauseState {
	if s == nil {
		return PauseNone
	}
	return s.pauseState
}

func (s *SessionState) SetPauseState(state PauseState) {
	s.pauseState = state
}

func (s *SessionState) GetCurrentAgent() runtime.AgentDetails {
	for _, agent := range s.availableAgents {
		if agent.Name == s.currentAgentName {
			return agent
		}
	}

	return runtime.AgentDetails{}
}

// SetAgentUsage records the latest token-usage snapshot for the named agent.
// Each snapshot carries cumulative totals for the agent's most recent
// session, so the last write always reflects its current context usage.
func (s *SessionState) SetAgentUsage(agentName string, usage runtime.Usage) {
	if agentName == "" {
		return
	}
	if s.agentUsage == nil {
		s.agentUsage = make(map[string]runtime.Usage)
	}
	s.agentUsage[agentName] = usage
}

// AgentUsage returns the latest token-usage snapshot recorded for the named
// agent, and whether one exists (an agent that has not run yet has none).
func (s *SessionState) AgentUsage(agentName string) (runtime.Usage, bool) {
	usage, ok := s.agentUsage[agentName]
	return usage, ok
}
