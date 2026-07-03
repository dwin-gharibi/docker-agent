package leantui

import (
	"context"
	"time"

	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/tools"
	tuitypes "github.com/docker/docker-agent/pkg/tui/types"
)

// handleEvent applies a single runtime event emitted by the App to the model,
// updating the conversation, tool state, status footer, or busy state.
func (m *model) handleEvent(ctx context.Context, ev any) {
	switch e := ev.(type) {
	case *runtime.StreamStartedEvent:
		m.busy = true
		m.trackStreamStarted(e.SessionID)
	case *runtime.StreamStoppedEvent:
		m.trackStreamStopped()
		m.handleStreamStopped(ctx)
	case *runtime.AgentChoiceReasoningEvent:
		m.appendPending(blockReasoning, e.Content)
	case *runtime.AgentChoiceEvent:
		m.appendPending(blockAssistant, e.Content)
	case *runtime.PartialToolCallEvent:
		m.flushPending()
		toolDef := tools.Tool{Name: e.ToolCall.Function.Name}
		if e.ToolDefinition != nil {
			toolDef = *e.ToolDefinition
		}
		m.toolz.upsert(e.GetAgentName(), e.ToolCall, toolDef, tuitypes.ToolStatusPending)
	case *runtime.ToolCallEvent:
		m.flushPending()
		m.toolz.upsert(e.GetAgentName(), e.ToolCall, e.ToolDefinition, tuitypes.ToolStatusRunning)
	case *runtime.ToolCallOutputEvent:
		if tv := m.toolz.get(e.ToolCallID); tv != nil && tv.message != nil {
			tv.message.AppendToolOutput(e.Output)
			if tv.message.ToolStatus == tuitypes.ToolStatusPending {
				tv.message.ToolStatus = tuitypes.ToolStatusRunning
				if tv.message.StartedAt == nil {
					now := time.Now()
					tv.message.StartedAt = &now
				}
			}
		}
	case *runtime.ToolCallResponseEvent:
		m.finishTool(e)
	case *runtime.ToolCallConfirmationEvent:
		m.toolz.remove(toolViewID(e.ToolCall))
		toolDef := ensureToolDefinition(e.ToolCall, e.ToolDefinition)
		m.confirm = &confirmState{
			tool:     toolDef.Name,
			toolView: *newToolView(e.GetAgentName(), e.ToolCall, toolDef, tuitypes.ToolStatusConfirmation),
		}
	case *runtime.TokenUsageEvent:
		m.setTokenUsage(e.SessionID, e.Usage)
	case *runtime.AgentInfoEvent:
		m.status.agent = e.AgentName
		if m.sessionState != nil {
			m.sessionState.SetCurrentAgentName(e.AgentName)
		}
		if e.Model != "" {
			m.status.model = e.Model
		}
		if e.ContextLimit > 0 {
			m.status.contextLimit = e.ContextLimit
		}
	case *runtime.TeamInfoEvent:
		m.applyTeamInfo(ctx, e)
	case *runtime.SessionCompactionEvent:
		m.handleSessionCompaction(ctx, e)
	case *runtime.ErrorEvent:
		m.flushPending()
		m.addNotice("✗ ", e.Error, stError())
	case *runtime.WarningEvent:
		m.addNotice("⚠ ", e.Message, stWarning())
	case *runtime.ShellOutputEvent:
		output := e.Output
		m.addBlock(func(w int) []string { return renderToolOutput(output, w) })
	case *runtime.AgentSwitchingEvent:
		if e.Switching && e.ToAgent != "" {
			m.addNotice("→ ", "Switching to "+e.ToAgent, stMuted())
		}
	case *runtime.MaxIterationsReachedEvent:
		m.addNotice("⚠ ", "Maximum iterations reached.", stWarning())
	case *runtime.ModelFallbackEvent:
		m.addNotice("⚠ ", "Model "+e.FailedModel+" failed, falling back to "+e.FallbackModel+".", stWarning())
	}
}

func (m *model) handleStreamStopped(ctx context.Context) {
	if m.finishBusy(ctx) {
		return
	}

	if m.app != nil && m.app.ShouldExitAfterFirstResponse() {
		m.quit()
	}
}

func (m *model) handleSessionCompaction(ctx context.Context, e *runtime.SessionCompactionEvent) {
	switch e.Status {
	case "started":
		m.busy = true
	case "completed":
		m.finishBusy(ctx)
	}
}

// finishBusy clears the busy state at the end of a run and starts the next
// queued message, if any. It reports whether a queued run was started.
func (m *model) finishBusy(ctx context.Context) bool {
	m.flushPending()
	m.busy = false
	m.runCancel = nil

	if len(m.queue) > 0 {
		next := m.queue[0]
		m.queue = m.queue[1:]
		m.startRun(ctx, next, nil)
		return true
	}
	return false
}

func (m *model) appendPending(kind blockKind, content string) {
	if content == "" {
		return
	}
	if m.pending == nil || m.pending.kind != kind {
		m.flushPending()
		m.pending = &pendingBlock{kind: kind}
	}
	m.pending.text.WriteString(content)
}

// flushPending finalizes the in-progress streamed block into the conversation.
func (m *model) flushPending() {
	if m.pending == nil {
		return
	}
	text := m.pending.text.String()
	kind := m.pending.kind
	m.pending = nil

	switch kind {
	case blockReasoning:
		m.addBlock(func(w int) []string { return renderReasoningLines(text, w) })
	case blockAssistant:
		m.addBlock(func(w int) []string { return renderAssistantLines(text, w) })
	}
}

func (m *model) finishTool(e *runtime.ToolCallResponseEvent) {
	view := m.toolz.finish(e)
	if view == nil {
		return
	}
	m.addBlock(func(w int) []string { return renderToolWithState(view, w, 0, m.sessionState) })
}

func (m *model) applyTeamInfo(ctx context.Context, e *runtime.TeamInfoEvent) {
	if m.sessionState != nil {
		m.sessionState.SetAvailableAgents(e.AvailableAgents)
		m.sessionState.SetCurrentAgentName(e.CurrentAgent)
	}
	for _, a := range e.AvailableAgents {
		if a.Name != e.CurrentAgent {
			continue
		}
		m.status.agent = a.Name
		switch {
		case a.Provider != "" && a.Model != "":
			m.status.model = a.Provider + "/" + a.Model
		case a.Model != "":
			m.status.model = a.Model
		}
		m.status.thinking = a.Thinking
	}
	m.refreshCommands(ctx)
}
