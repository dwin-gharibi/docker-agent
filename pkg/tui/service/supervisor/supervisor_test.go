package supervisor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/tools"
)

func newTestSupervisor(ids []string, activeID string) *Supervisor {
	s := &Supervisor{
		runners:      make(map[string]*SessionRunner),
		programReady: make(chan struct{}),
	}
	for _, id := range ids {
		s.runners[id] = &SessionRunner{ID: id}
		s.order = append(s.order, id)
	}
	s.activeID = activeID
	return s
}

func TestCloseSession_FocusesPreviousTab(t *testing.T) {
	t.Parallel()
	// Tabs: [A, B, C], active=C. Close C → expect B.
	s := newTestSupervisor([]string{"A", "B", "C"}, "C")

	next := s.CloseSession("C")

	assert.Equal(t, "B", next)
	assert.Equal(t, "B", s.activeID)
	assert.Equal(t, []string{"A", "B"}, s.order)
}

func TestCloseSession_FocusesPreviousTab_Middle(t *testing.T) {
	t.Parallel()
	// Tabs: [A, B, C], active=B. Close B → expect A.
	s := newTestSupervisor([]string{"A", "B", "C"}, "B")

	next := s.CloseSession("B")

	assert.Equal(t, "A", next)
	assert.Equal(t, "A", s.activeID)
	assert.Equal(t, []string{"A", "C"}, s.order)
}

func TestCloseSession_FirstTab_FocusesNewFirst(t *testing.T) {
	t.Parallel()
	// Tabs: [A, B, C], active=A. Close A → expect B (new first).
	s := newTestSupervisor([]string{"A", "B", "C"}, "A")

	next := s.CloseSession("A")

	assert.Equal(t, "B", next)
	assert.Equal(t, "B", s.activeID)
	assert.Equal(t, []string{"B", "C"}, s.order)
}

func TestCloseSession_LastRemaining(t *testing.T) {
	t.Parallel()
	// Tabs: [A], active=A. Close A → expect "".
	s := newTestSupervisor([]string{"A"}, "A")

	next := s.CloseSession("A")

	assert.Empty(t, next)
	assert.Empty(t, s.activeID)
	assert.Empty(t, s.order)
}

func TestCloseSession_InactiveTab(t *testing.T) {
	t.Parallel()
	// Tabs: [A, B, C], active=A. Close C → active stays A.
	s := newTestSupervisor([]string{"A", "B", "C"}, "A")

	next := s.CloseSession("C")

	assert.Equal(t, "A", next)
	assert.Equal(t, "A", s.activeID)
	assert.Equal(t, []string{"A", "B"}, s.order)
}

func TestCloseSession_NonExistent(t *testing.T) {
	t.Parallel()
	s := newTestSupervisor([]string{"A", "B"}, "A")

	next := s.CloseSession("Z")

	assert.Equal(t, "A", next)
	assert.Equal(t, []string{"A", "B"}, s.order)
}

func TestCloseSession_TwoTabs_CloseSecond(t *testing.T) {
	t.Parallel()
	// Tabs: [A, B], active=B. Close B → expect A.
	s := newTestSupervisor([]string{"A", "B"}, "B")

	next := s.CloseSession("B")

	assert.Equal(t, "A", next)
	assert.Equal(t, "A", s.activeID)
	assert.Equal(t, []string{"A"}, s.order)
}

// TestSetPendingEvent_RoundTrip verifies that SetPendingEvent stores an event
// for a session and that ConsumePendingEvent retrieves and clears it. This
// is the path used to re-stash a background dialog's originating event when
// the user switches away from the tab that opened it (see #2626).
func TestSetPendingEvent_RoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestSupervisor([]string{"A", "B"}, "A")

	type fakeEvent struct{ id int }
	event := &fakeEvent{id: 7}

	s.SetPendingEvent("A", event)

	assert.Equal(t, []tea.Msg{event}, s.runners["A"].PendingEvents, "event is stored on the runner")
	assert.False(t, s.runners["A"].NeedsAttn, "SetPendingEvent must NOT raise NeedsAttn (the user is already aware)")

	got := s.ConsumePendingEvent("A")
	assert.Equal(t, event, got)
	assert.Empty(t, s.runners["A"].PendingEvents, "event is cleared after consumption")
}

// TestSetPendingEvent_Queue verifies that multiple events queued for the same
// inactive session are replayed in FIFO order and that SetPendingEvent
// re-queues at the front, ahead of anything queued behind it (#3584).
func TestSetPendingEvent_Queue(t *testing.T) {
	t.Parallel()
	s := newTestSupervisor([]string{"A"}, "B")

	type fakeEvent struct{ id int }
	first := &fakeEvent{id: 1}
	second := &fakeEvent{id: 2}

	s.runners["A"].PendingEvents = []tea.Msg{first, second}

	// Re-stash a third event (e.g. the live dialog instance for the event the
	// user was looking at) ahead of the two already queued.
	stashed := &fakeEvent{id: 0}
	s.SetPendingEvent("A", stashed)

	assert.Equal(t, stashed, s.ConsumePendingEvent("A"))
	assert.Equal(t, first, s.ConsumePendingEvent("A"))
	assert.Equal(t, second, s.ConsumePendingEvent("A"))
	assert.Nil(t, s.ConsumePendingEvent("A"), "queue is drained")
}

// TestSetPendingEvent_UnknownSession is a no-op (and must not panic).
func TestSetPendingEvent_UnknownSession(t *testing.T) {
	t.Parallel()
	s := newTestSupervisor([]string{"A"}, "A")

	s.SetPendingEvent("does-not-exist", "payload")

	assert.Empty(t, s.runners["A"].PendingEvents, "unrelated runner is untouched")
}

// --- #3217: session-aware stream lifecycle tests ---

// TestIsTopLevelStream covers the isTopLevelStream helper directly.
func TestIsTopLevelStream(t *testing.T) {
	t.Parallel()
	tests := []struct {
		runnerID    string
		evSessionID string
		want        bool
	}{
		{runnerID: "sess-A", evSessionID: "sess-A", want: true},   // exact match → top-level
		{runnerID: "sess-A", evSessionID: "", want: true},         // empty → top-level (backward compat)
		{runnerID: "sess-A", evSessionID: "child-B", want: false}, // different ID → nested
		{runnerID: "sess-A", evSessionID: "sess-B", want: false},  // sibling ID → nested
	}
	for _, tc := range tests {
		got := isTopLevelStream(tc.runnerID, tc.evSessionID)
		assert.Equal(t, tc.want, got,
			"isTopLevelStream(%q, %q)", tc.runnerID, tc.evSessionID)
	}
}

// TestStreamStarted_SubSessionDoesNotDropPendingEvent verifies that a
// StreamStartedEvent carrying a child session ID (nested sub-agent/fork-skill
// stream forwarded through the parent's event channel) does NOT wipe the
// parent runner's pending elicitation event. (#3217)
func TestStreamStarted_SubSessionDoesNotDropPendingEvent(t *testing.T) {
	t.Parallel()
	s := newTestSupervisor([]string{"sess-A", "sess-B"}, "sess-B") // sess-A is background

	elicitation := runtime.ElicitationRequest("confirm?", "form", nil, "", "eid-1", "", "", nil, "agent")
	s.runners["sess-A"].PendingEvents = []tea.Msg{elicitation}
	s.runners["sess-A"].NeedsAttn = true
	s.runners["sess-A"].IsRunning = true // already running a top-level turn

	// A nested sub-session stream starts (different SessionID).
	s.handleRuntimeEvent("sess-A", &runtime.StreamStartedEvent{
		Type:      "stream_started",
		SessionID: "child-xyz",
	})

	require.NotEmpty(t, s.runners["sess-A"].PendingEvents,
		"nested StreamStarted must NOT clear the parent's pending elicitation")
	assert.True(t, s.runners["sess-A"].NeedsAttn,
		"nested StreamStarted must NOT clear NeedsAttn")
	assert.True(t, s.runners["sess-A"].IsRunning,
		"nested StreamStarted must NOT change IsRunning")
}

// TestStreamStopped_SubSessionDoesNotDropPendingEvent verifies that a
// StreamStoppedEvent from a child session does NOT clear the parent's pending
// event, NeedsAttn, or IsRunning. (#3217)
func TestStreamStopped_SubSessionDoesNotDropPendingEvent(t *testing.T) {
	t.Parallel()
	s := newTestSupervisor([]string{"sess-A", "sess-B"}, "sess-B")

	elicitation := runtime.ElicitationRequest("confirm?", "form", nil, "", "eid-2", "", "", nil, "agent")
	s.runners["sess-A"].PendingEvents = []tea.Msg{elicitation}
	s.runners["sess-A"].NeedsAttn = true
	s.runners["sess-A"].IsRunning = true

	// A nested sub-session stream stops (different SessionID).
	s.handleRuntimeEvent("sess-A", &runtime.StreamStoppedEvent{
		Type:      "stream_stopped",
		SessionID: "child-xyz",
	})

	require.NotEmpty(t, s.runners["sess-A"].PendingEvents,
		"nested StreamStopped must NOT clear the parent's pending elicitation")
	assert.True(t, s.runners["sess-A"].NeedsAttn,
		"nested StreamStopped must NOT clear NeedsAttn")
	assert.True(t, s.runners["sess-A"].IsRunning,
		"nested StreamStopped must NOT flip IsRunning to false while parent is still running")
}

// TestStreamStarted_TopLevelSupersedesStalePending verifies that a top-level
// StreamStartedEvent (matching session ID) STILL clears a stale pending event
// — the original intent must be preserved. (#3217)
func TestStreamStarted_TopLevelSupersedesStalePending(t *testing.T) {
	t.Parallel()
	s := newTestSupervisor([]string{"sess-A"}, "sess-A")

	s.runners["sess-A"].PendingEvents = []tea.Msg{runtime.ElicitationRequest(
		"old?", "form", nil, "", "eid-stale", "", "", nil, "agent",
	)}
	s.runners["sess-A"].IsRunning = false

	// New top-level turn starts.
	s.handleRuntimeEvent("sess-A", &runtime.StreamStartedEvent{
		Type:      "stream_started",
		SessionID: "sess-A",
	})

	assert.Empty(t, s.runners["sess-A"].PendingEvents,
		"top-level StreamStarted must supersede any stale pending event")
	assert.True(t, s.runners["sess-A"].IsRunning,
		"top-level StreamStarted must set IsRunning")
}

// TestStreamStopped_TopLevelClearsPendingAndNeedsAttn verifies that a
// top-level StreamStoppedEvent correctly clears all three fields. (#3217)
func TestStreamStopped_TopLevelClearsPendingAndNeedsAttn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pending any // tea.Msg
	}{
		{
			name:    "elicitation pending",
			pending: runtime.ElicitationRequest("q?", "form", nil, "", "eid-3", "", "", nil, "agent"),
		},
		{
			name:    "tool confirmation pending",
			pending: runtime.ToolCallConfirmation(tools.ToolCall{}, tools.Tool{}, "agent", nil),
		},
		{
			name:    "max iterations pending",
			pending: runtime.MaxIterationsReached(10),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestSupervisor([]string{"sess-A"}, "sess-B")
			s.runners["sess-A"].PendingEvents = []tea.Msg{tc.pending}
			s.runners["sess-A"].NeedsAttn = true
			s.runners["sess-A"].IsRunning = true

			s.handleRuntimeEvent("sess-A", &runtime.StreamStoppedEvent{
				Type:      "stream_stopped",
				SessionID: "sess-A",
			})

			assert.Empty(t, s.runners["sess-A"].PendingEvents,
				"top-level StreamStopped must clear PendingEvents")
			assert.False(t, s.runners["sess-A"].NeedsAttn,
				"top-level StreamStopped must clear NeedsAttn")
			assert.False(t, s.runners["sess-A"].IsRunning,
				"top-level StreamStopped must clear IsRunning")
		})
	}
}

// TestStreamStarted_EmptySessionID_TreatedAsTopLevel verifies that an empty
// SessionID is treated as top-level for backward compatibility with emitters
// that omit it. (#3217)
func TestStreamStarted_EmptySessionID_TreatedAsTopLevel(t *testing.T) {
	t.Parallel()
	s := newTestSupervisor([]string{"sess-A"}, "sess-A")

	s.runners["sess-A"].PendingEvents = []tea.Msg{runtime.ElicitationRequest(
		"old?", "form", nil, "", "eid-old", "", "", nil, "agent",
	)}

	// Emitter omits SessionID (empty string).
	s.handleRuntimeEvent("sess-A", &runtime.StreamStartedEvent{
		Type:      "stream_started",
		SessionID: "",
	})

	assert.Empty(t, s.runners["sess-A"].PendingEvents,
		"empty SessionID must be treated as top-level and supersede stale pending event")
	assert.True(t, s.runners["sess-A"].IsRunning)
}

// --- #3584 review item 4: foreground stream stop must not discard a
// still-live detached background job's elicitation ---

// TestStreamStopped_TopLevel_PreservesDetachedBackgroundElicitation is the
// regression test for review item 4: a background job started via
// run_background_agent outlives its parent's top-level stream
// (context.WithoutCancel), so its own live, unanswered elicitation can still
// be queued on this runner when the FOREGROUND stream stops. The old
// unconditional `PendingEvents = nil` wiped it out from under the background
// job's still-blocked waiter goroutine. Only the runner's OWN top-level
// attention events (here, none) are moot; the background elicitation
// (SessionID "bg-child", distinct from the runner's own "sess-A") must
// survive.
func TestStreamStopped_TopLevel_PreservesDetachedBackgroundElicitation(t *testing.T) {
	t.Parallel()
	s := newTestSupervisor([]string{"sess-A"}, "sess-B") // sess-A inactive

	bgElicitation := runtime.ElicitationRequest("bg needs input", "form", nil, "", "eid-bg", "", "bg-child", nil, "agent")
	s.runners["sess-A"].PendingEvents = []tea.Msg{bgElicitation}
	s.runners["sess-A"].NeedsAttn = true
	s.runners["sess-A"].IsRunning = true

	// The foreground (top-level) stream for sess-A stops.
	s.handleRuntimeEvent("sess-A", &runtime.StreamStoppedEvent{
		Type:      "stream_stopped",
		SessionID: "sess-A",
	})

	assert.False(t, s.runners["sess-A"].IsRunning, "the foreground stream itself must still be marked stopped")
	require.Equal(t, []tea.Msg{bgElicitation}, s.runners["sess-A"].PendingEvents,
		"a detached background job's live elicitation must survive the foreground stream's stop")
	assert.True(t, s.runners["sess-A"].NeedsAttn,
		"NeedsAttn must stay true while a background elicitation is still queued")
}

// TestStreamStopped_TopLevel_MixedPending_OnlyForegroundEventsCleared covers
// the more realistic mixed case: a foreground-owned tool confirmation and a
// background job's elicitation are both queued. Stopping the foreground
// stream must clear only the foreground-scoped entry.
func TestStreamStopped_TopLevel_MixedPending_OnlyForegroundEventsCleared(t *testing.T) {
	t.Parallel()
	s := newTestSupervisor([]string{"sess-A"}, "sess-B")

	foregroundConfirmation := runtime.ToolCallConfirmation(tools.ToolCall{}, tools.Tool{}, "agent", nil)
	bgElicitation := runtime.ElicitationRequest("bg needs input", "form", nil, "", "eid-bg", "", "bg-child", nil, "agent")
	s.runners["sess-A"].PendingEvents = []tea.Msg{foregroundConfirmation, bgElicitation}
	s.runners["sess-A"].NeedsAttn = true
	s.runners["sess-A"].IsRunning = true

	s.handleRuntimeEvent("sess-A", &runtime.StreamStoppedEvent{
		Type:      "stream_stopped",
		SessionID: "sess-A",
	})

	require.Equal(t, []tea.Msg{bgElicitation}, s.runners["sess-A"].PendingEvents,
		"only the foreground-scoped tool confirmation must be dropped; the background elicitation stays queued")
	assert.True(t, s.runners["sess-A"].NeedsAttn)
}

// TestStreamStarted_TopLevel_PreservesDetachedBackgroundElicitation mirrors
// the StreamStopped case for a NEW top-level turn starting on the same tab
// while a background job from a previous turn is still live: starting a new
// foreground turn must not orphan the background job's queued elicitation
// either.
func TestStreamStarted_TopLevel_PreservesDetachedBackgroundElicitation(t *testing.T) {
	t.Parallel()
	s := newTestSupervisor([]string{"sess-A"}, "sess-B")

	bgElicitation := runtime.ElicitationRequest("bg needs input", "form", nil, "", "eid-bg2", "", "bg-child-2", nil, "agent")
	s.runners["sess-A"].PendingEvents = []tea.Msg{bgElicitation}
	s.runners["sess-A"].NeedsAttn = true

	s.handleRuntimeEvent("sess-A", &runtime.StreamStartedEvent{
		Type:      "stream_started",
		SessionID: "sess-A",
	})

	assert.True(t, s.runners["sess-A"].IsRunning)
	require.Equal(t, []tea.Msg{bgElicitation}, s.runners["sess-A"].PendingEvents,
		"a new top-level turn must not discard a still-live detached background elicitation")
	assert.True(t, s.runners["sess-A"].NeedsAttn)
}
