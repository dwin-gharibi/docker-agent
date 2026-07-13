package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/api"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/concurrent"
	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/sessiontitle"
	"github.com/docker/docker-agent/pkg/tools"
)

// fakeRuntime is a minimal Runtime that records concurrent RunStream calls.
type fakeRuntime struct {
	runtime.Runtime

	concurrentStreams atomic.Int32
	maxConcurrent     atomic.Int32
	// release, when non-nil, keeps the stream open until it is closed or
	// the stream context is cancelled; when nil the stream ends at once.
	release chan struct{}
}

func (f *fakeRuntime) RunStream(ctx context.Context, _ *session.Session) <-chan runtime.Event {
	cur := f.concurrentStreams.Add(1)
	for {
		old := f.maxConcurrent.Load()
		if cur <= old || f.maxConcurrent.CompareAndSwap(old, cur) {
			break
		}
	}

	ch := make(chan runtime.Event)
	go func() {
		if f.release != nil {
			select {
			case <-f.release:
			case <-ctx.Done():
			}
		}
		f.concurrentStreams.Add(-1)
		close(ch)
	}()
	return ch
}

func (f *fakeRuntime) Resume(_ context.Context, _ runtime.ResumeRequest) {}

func (f *fakeRuntime) Steer(_ context.Context, _ runtime.QueuedMessage) error { return nil }

func (f *fakeRuntime) FollowUp(_ context.Context, _ runtime.QueuedMessage) error { return nil }

func (f *fakeRuntime) ResumeElicitation(_ context.Context, _ tools.ElicitationAction, _ map[string]any, _ ...string) error {
	return nil
}

func (f *fakeRuntime) CurrentAgentName(context.Context) string { return "root" }

// SupportsModelSwitching reports false by default. Tests that exercise
// the /models endpoints embed fakeRuntime and override this.
func (f *fakeRuntime) SupportsModelSwitching() bool { return false }

func newTestSessionManager(t *testing.T, sess *session.Session, fake *fakeRuntime) *SessionManager {
	t.Helper()

	ctx := t.Context()
	store := session.NewInMemorySessionStore()
	require.NoError(t, store.AddSession(ctx, sess))

	sm := &SessionManager{
		runtimeSessions:   concurrent.NewMap[string, *activeRuntimes](),
		deletedSessions:   concurrent.NewMap[string, *activeRuntimes](),
		eventLogs:         concurrent.NewMap[string, *pumpedEventLog](),
		followUpInjectors: concurrent.NewMap[string, FollowUpInjector](),
		followUpKeys:      concurrent.NewMap[string, *idempotencyCache](),
		sessionStore:      store,
		Sources:           config.Sources{},
		runConfig:         &config.RuntimeConfig{},
		sessionReady:      make(chan struct{}),
	}

	// Pre-register a runtime for this session so RunSession skips agent loading.
	sm.runtimeSessions.Store(sess.ID, &activeRuntimes{
		runtime:  fake,
		session:  sess,
		titleGen: (*sessiontitle.Generator)(nil),
	})

	return sm
}

// TestAttachRuntime_RegistersRuntimeForExternalDriver verifies that a
// pre-built runtime is reachable through the manager API after AttachRuntime.
// This is what lets the TUI hand its in-process runtime to an HTTP server.
func TestAttachRuntime_RegistersRuntimeForExternalDriver(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := session.NewInMemorySessionStore()
	sess := session.New()
	require.NoError(t, store.AddSession(ctx, sess))

	sm := NewSessionManager(ctx, config.Sources{}, store, 0, &config.RuntimeConfig{})
	fake := &fakeRuntime{}
	sm.AttachRuntime(t.Context(), sess.ID, fake, sess)

	// Steer routes through the attached runtime, not a freshly built one.
	require.NoError(t, sm.SteerSession(ctx, sess.ID, []api.Message{{Content: "hi"}}))
}

// TestRunSession_ConcurrentRequestReturnsErrSessionBusy verifies that a
// second RunSession call on a session that is already streaming returns
// ErrSessionBusy instead of silently interleaving messages.
func TestRunSession_ConcurrentRequestReturnsErrSessionBusy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	release := make(chan struct{})
	fake := &fakeRuntime{release: release}
	sm := newTestSessionManager(t, sess, fake)

	// Start the first stream. RunSession acquires the streaming lock
	// synchronously, so the session is busy as soon as it returns.
	ch1, err := sm.RunSession(ctx, sess.ID, "agent", "root", []api.Message{
		{Content: "first"},
	}, "")
	require.NoError(t, err)

	// The second request should fail immediately with ErrSessionBusy.
	_, err = sm.RunSession(ctx, sess.ID, "agent", "root", []api.Message{
		{Content: "second"},
	}, "")
	require.ErrorIs(t, err, ErrSessionBusy)

	// Let the first stream complete and drain it.
	close(release)
	for range ch1 {
	}

	// After the first stream finishes, a new request should succeed.
	ch3, err := sm.RunSession(ctx, sess.ID, "agent", "root", []api.Message{
		{Content: "third"},
	}, "")
	require.NoError(t, err)
	for range ch3 {
	}
}

// TestRunSession_MessagesNotAddedWhenBusy verifies that when a session
// is busy, the rejected request does not mutate the session's messages.
func TestRunSession_MessagesNotAddedWhenBusy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	release := make(chan struct{})
	fake := &fakeRuntime{release: release}
	sm := newTestSessionManager(t, sess, fake)

	ch1, err := sm.RunSession(ctx, sess.ID, "agent", "root", []api.Message{
		{Content: "first"},
	}, "")
	require.NoError(t, err)

	msgCountBefore := len(sess.GetAllMessages())

	_, err = sm.RunSession(ctx, sess.ID, "agent", "root", []api.Message{
		{Content: "should not be added"},
	}, "")
	require.ErrorIs(t, err, ErrSessionBusy)

	// Messages should not have been added.
	assert.Len(t, sess.GetAllMessages(), msgCountBefore)

	close(release)
	for range ch1 {
	}
}

// TestAddMessage_RejectsWhileSessionStreaming verifies the 409-busy guard
// added for issue #3590: AddMessage must reject with ErrSessionBusy while
// the session has an active RunStream. session.Session.mu already makes the
// append itself race-free, but a message injected mid-stream (mid-tool-call
// in particular) can still desynchronize the turn from what the model/tools
// expect, so the API layer also rejects it outright.
func TestAddMessage_RejectsWhileSessionStreaming(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	release := make(chan struct{})
	fake := &fakeRuntime{release: release}
	sm := newTestSessionManager(t, sess, fake)

	ch, err := sm.RunSession(ctx, sess.ID, "agent", "root", []api.Message{{Content: "hi"}}, "")
	require.NoError(t, err)

	err = sm.AddMessage(ctx, sess.ID, session.UserMessage("should be rejected"))
	require.ErrorIs(t, err, ErrSessionBusy)

	close(release)
	for range ch {
	}

	// After the stream ends, AddMessage must succeed normally.
	require.NoError(t, sm.AddMessage(ctx, sess.ID, session.UserMessage("accepted")))
}

// TestUpdateMessage_RejectsWhileSessionStreaming mirrors
// TestAddMessage_RejectsWhileSessionStreaming for UpdateMessage.
func TestUpdateMessage_RejectsWhileSessionStreaming(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	release := make(chan struct{})
	fake := &fakeRuntime{release: release}
	sm := newTestSessionManager(t, sess, fake)

	msgID, err := sm.sessionStore.AddMessage(ctx, sess.ID, session.UserMessage("original"))
	require.NoError(t, err)
	msgIDStr := strconv.FormatInt(msgID, 10)

	ch, err := sm.RunSession(ctx, sess.ID, "agent", "root", []api.Message{{Content: "hi"}}, "")
	require.NoError(t, err)

	err = sm.UpdateMessage(ctx, sess.ID, msgIDStr, session.UserMessage("should be rejected"))
	require.ErrorIs(t, err, ErrSessionBusy)

	close(release)
	for range ch {
	}

	// After the stream ends, UpdateMessage must succeed normally.
	require.NoError(t, sm.UpdateMessage(ctx, sess.ID, msgIDStr, session.UserMessage("accepted")))
}

// TestAttachedStream_AddMessageAndUpdateMessageRejectWhileStreaming pins the
// fix for the other #3590 blocker: runtimes attached via AttachRuntime
// stream directly through RunStream (see pkg/app.App.Run/Retry/
// RunWithMessage), never going through RunSession, which is the only place
// that used to acquire activeRuntimes.streaming. Before the fix nothing held
// that lock for an attached stream, so AddMessage/UpdateMessage wrongly
// succeeded during a genuinely active attached stream instead of returning
// ErrSessionBusy. AttachRuntime now returns the same lock RunSession uses;
// the App holds it for the duration of every direct RunStream call (see
// app.WithStreamGuard/acquireStreamGuard). This test drives a real stream
// through that lock exactly the way the App does — NOT through
// sm.RunSession — to prove the guard covers the attached path too.
func TestAttachedStream_AddMessageAndUpdateMessageRejectWhileStreaming(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := session.NewInMemorySessionStore()
	sess := session.New()
	require.NoError(t, store.AddSession(ctx, sess))

	msgID, err := store.AddMessage(ctx, sess.ID, session.UserMessage("original"))
	require.NoError(t, err)
	msgIDStr := strconv.FormatInt(msgID, 10)

	sm := NewSessionManager(ctx, config.Sources{}, store, 0, &config.RuntimeConfig{})
	release := make(chan struct{})
	fake := &fakeRuntime{release: release}
	guard := sm.AttachRuntime(ctx, sess.ID, fake, sess)

	// Simulate the TUI/attached owner streaming directly through the
	// runtime, exactly like pkg/app.App.acquireStreamGuard + Run do — NOT
	// through sm.RunSession.
	guard.Lock()
	ch := fake.RunStream(ctx, sess)

	err = sm.AddMessage(ctx, sess.ID, session.UserMessage("should be rejected"))
	require.ErrorIs(t, err, ErrSessionBusy)

	err = sm.UpdateMessage(ctx, sess.ID, msgIDStr, session.UserMessage("should be rejected"))
	require.ErrorIs(t, err, ErrSessionBusy)

	close(release)
	for range ch {
	}
	guard.Unlock()

	// After the attached stream ends, both must succeed normally.
	require.NoError(t, sm.AddMessage(ctx, sess.ID, session.UserMessage("accepted")))
	require.NoError(t, sm.UpdateMessage(ctx, sess.ID, msgIDStr, session.UserMessage("accepted")))
}

// blockingStore wraps a session.Store and blocks inside AddMessage/
// UpdateMessage until release is closed, letting a test pause the manager
// mid-mutation — after the busy check has already passed — to observe
// whether a concurrent attached stream can slip in before the mutation
// actually completes. entered is closed the instant the blocked call is
// reached, so the test can synchronize on it instead of sleeping.
type blockingStore struct {
	session.Store

	release chan struct{}
	entered chan struct{}
}

func (s *blockingStore) AddMessage(ctx context.Context, sessionID string, msg *session.Message) (int64, error) {
	close(s.entered)
	<-s.release
	return s.Store.AddMessage(ctx, sessionID, msg)
}

func (s *blockingStore) UpdateMessage(ctx context.Context, messageID int64, msg *session.Message) error {
	close(s.entered)
	<-s.release
	return s.Store.UpdateMessage(ctx, messageID, msg)
}

// assertAttachedGuardBlockedDuringMutation drives the reviewer's
// deterministic "blocking-store" probe (#3590 finding A1): it starts
// mutate (an AddMessage or UpdateMessage call) against a store that blocks
// mid-write, waits for the busy check inside mutate to have already passed
// (store.entered closes), and then tries to acquire the attached-stream
// guard exactly the way pkg/app.App.acquireStreamGuard does (a plain
// Lock(), not TryLock()). Before the #3590 fix, AddMessage/UpdateMessage
// released activeRuntimes.streaming immediately after the busy check, so
// the guard acquisition below would succeed while mutate was still
// blocked inside the store write — an attached stream starting between
// the busy check and the mutation completing. With the fix (the streaming
// lock held via defer until mutate returns), the guard acquisition must
// stay blocked until mutate has fully returned.
func assertAttachedGuardBlockedDuringMutation(t *testing.T, guard sync.Locker, store *blockingStore, mutate func() error) {
	t.Helper()

	mutateErrCh := make(chan error, 1)
	go func() { mutateErrCh <- mutate() }()

	select {
	case <-store.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the mutation to reach the blocking store")
	}

	guardAcquired := make(chan struct{})
	go func() {
		guard.Lock()
		close(guardAcquired)
	}()

	select {
	case <-guardAcquired:
		t.Fatal("attached stream guard acquired before the REST mutation completed")
	case <-time.After(100 * time.Millisecond):
		// Expected: the guard stays held across the in-flight mutation.
	}

	close(store.release)
	require.NoError(t, <-mutateErrCh)

	select {
	case <-guardAcquired:
		guard.Unlock()
	case <-time.After(2 * time.Second):
		t.Fatal("attached stream guard never acquired after the REST mutation completed")
	}
}

// TestReview_AttachedGuardCannotStartBetweenBusyCheckAndMutation_AddMessage
// is the reviewer's deterministic regression probe for #3590 finding A1:
// AddMessage must hold activeRuntimes.streaming across its entire mutation,
// not just across the busy check, otherwise an attached stream (the only
// consumer of that lock outside RunSession — see AttachRuntime) can start
// in the gap between the check passing and the store write completing.
func TestReview_AttachedGuardCannotStartBetweenBusyCheckAndMutation_AddMessage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	inner := session.NewInMemorySessionStore()
	require.NoError(t, inner.AddSession(ctx, sess))
	store := &blockingStore{Store: inner, release: make(chan struct{}), entered: make(chan struct{})}

	sm := NewSessionManager(ctx, config.Sources{}, store, 0, &config.RuntimeConfig{})
	guard := sm.AttachRuntime(ctx, sess.ID, &fakeRuntime{}, sess)

	assertAttachedGuardBlockedDuringMutation(t, guard, store, func() error {
		return sm.AddMessage(ctx, sess.ID, session.UserMessage("mutating"))
	})
}

// TestReview_AttachedGuardCannotStartBetweenBusyCheckAndMutation_UpdateMessage
// mirrors the AddMessage probe above for UpdateMessage.
func TestReview_AttachedGuardCannotStartBetweenBusyCheckAndMutation_UpdateMessage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	inner := session.NewInMemorySessionStore()
	require.NoError(t, inner.AddSession(ctx, sess))
	msgID, err := inner.AddMessage(ctx, sess.ID, session.UserMessage("original"))
	require.NoError(t, err)
	msgIDStr := strconv.FormatInt(msgID, 10)

	store := &blockingStore{Store: inner, release: make(chan struct{}), entered: make(chan struct{})}

	sm := NewSessionManager(ctx, config.Sources{}, store, 0, &config.RuntimeConfig{})
	guard := sm.AttachRuntime(ctx, sess.ID, &fakeRuntime{}, sess)

	assertAttachedGuardBlockedDuringMutation(t, guard, store, func() error {
		return sm.UpdateMessage(ctx, sess.ID, msgIDStr, session.UserMessage("mutating"))
	})
}

// TestServer_AddMessage_Returns409WhileSessionStreaming and
// TestServer_UpdateMessage_Returns409WhileSessionStreaming drive the actual
// HTTP handlers (not just SessionManager) to pin the 409-busy guard added
// for issue #3590 end to end: ErrSessionBusy from the manager must surface
// as echo.NewHTTPError(http.StatusConflict, ...), mirroring how runAgent
// already maps it.
func TestServer_AddMessage_Returns409WhileSessionStreaming(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	release := make(chan struct{})
	fake := &fakeRuntime{release: release}
	sm := newTestSessionManager(t, sess, fake)
	srv := NewWithManager(sm, "")

	ch, err := sm.RunSession(ctx, sess.ID, "agent", "root", []api.Message{{Content: "hi"}}, "")
	require.NoError(t, err)

	body, err := json.Marshal(api.AddMessageRequest{Message: session.UserMessage("should be rejected")})
	require.NoError(t, err)

	e := echo.New()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/sessions/"+sess.ID+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(sess.ID)

	err = srv.addMessage(c)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusConflict, httpErr.Code)

	close(release)
	for range ch {
	}
}

func TestServer_UpdateMessage_Returns409WhileSessionStreaming(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	release := make(chan struct{})
	fake := &fakeRuntime{release: release}
	sm := newTestSessionManager(t, sess, fake)
	srv := NewWithManager(sm, "")

	msgID, err := sm.sessionStore.AddMessage(ctx, sess.ID, session.UserMessage("original"))
	require.NoError(t, err)

	ch, err := sm.RunSession(ctx, sess.ID, "agent", "root", []api.Message{{Content: "hi"}}, "")
	require.NoError(t, err)

	body, err := json.Marshal(api.UpdateMessageRequest{Message: session.UserMessage("should be rejected")})
	require.NoError(t, err)

	e := echo.New()
	msgIDStr := strconv.FormatInt(msgID, 10)
	req := httptest.NewRequestWithContext(ctx, http.MethodPatch, "/api/sessions/"+sess.ID+"/messages/"+msgIDStr, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id", "msg_id")
	c.SetParamValues(sess.ID, msgIDStr)

	err = srv.updateMessage(c)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusConflict, httpErr.Code)

	close(release)
	for range ch {
	}
}

// TestRunSession_SequentialRequestsSucceed verifies that sequential
// (non-overlapping) requests on the same session work normally.
func TestRunSession_SequentialRequestsSucceed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	fake := &fakeRuntime{}
	sm := newTestSessionManager(t, sess, fake)

	for range 3 {
		ch, err := sm.RunSession(ctx, sess.ID, "agent", "root", []api.Message{
			{Content: "hello"},
		}, "")
		require.NoError(t, err)
		for range ch {
		}
	}

	assert.Equal(t, int32(1), fake.maxConcurrent.Load())
}

// TestRunSession_DifferentSessionsConcurrently verifies that concurrent
// requests on *different* sessions are not blocked by each other.
func TestRunSession_DifferentSessionsConcurrently(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := session.NewInMemorySessionStore()
	fake1 := &fakeRuntime{}
	fake2 := &fakeRuntime{}

	sess1 := session.New()
	sess2 := session.New()
	require.NoError(t, store.AddSession(ctx, sess1))
	require.NoError(t, store.AddSession(ctx, sess2))

	sm := &SessionManager{
		runtimeSessions: concurrent.NewMap[string, *activeRuntimes](),
		deletedSessions: concurrent.NewMap[string, *activeRuntimes](),
		followUpKeys:    concurrent.NewMap[string, *idempotencyCache](),
		sessionStore:    store,
		Sources:         config.Sources{},
		runConfig:       &config.RuntimeConfig{},
		sessionReady:    make(chan struct{}),
	}

	sm.runtimeSessions.Store(sess1.ID, &activeRuntimes{
		runtime: fake1, session: sess1, titleGen: (*sessiontitle.Generator)(nil),
	})
	sm.runtimeSessions.Store(sess2.ID, &activeRuntimes{
		runtime: fake2, session: sess2, titleGen: (*sessiontitle.Generator)(nil),
	})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		ch, err := sm.RunSession(ctx, sess1.ID, "agent", "root", []api.Message{{Content: "a"}}, "")
		assert.NoError(t, err)
		for range ch {
		}
	}()

	go func() {
		defer wg.Done()
		ch, err := sm.RunSession(ctx, sess2.ID, "agent", "root", []api.Message{{Content: "b"}}, "")
		assert.NoError(t, err)
		for range ch {
		}
	}()

	wg.Wait()

	// Both sessions should have streamed (1 each).
	assert.Equal(t, int32(1), fake1.maxConcurrent.Load())
	assert.Equal(t, int32(1), fake2.maxConcurrent.Load())
}

// recordingFollowUpRuntime records calls to FollowUp so tests can assert
// whether the runtime follow-up queue was used.
type recordingFollowUpRuntime struct {
	fakeRuntime

	mu        sync.Mutex
	followUps []string
}

func (r *recordingFollowUpRuntime) FollowUp(_ context.Context, msg runtime.QueuedMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.followUps = append(r.followUps, msg.Content)
	return nil
}

func (r *recordingFollowUpRuntime) followUpContents() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.followUps...)
}

// TestFollowUpSession_RoutesToInjectorWhenRegistered verifies that an
// attached session's follow-up is delivered through the registered injector
// (which starts a real turn in the TUI App) rather than the runtime
// follow-up queue, which an idle session never drains.
func TestFollowUpSession_RoutesToInjectorWhenRegistered(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	fake := &recordingFollowUpRuntime{}
	sm := newTestSessionManager(t, sess, &fake.fakeRuntime)
	// Replace the pre-registered runtime with our recording one.
	sm.runtimeSessions.Store(sess.ID, &activeRuntimes{runtime: fake, session: sess})

	var injected []string
	sm.RegisterFollowUpInjector(sess.ID, func(_ context.Context, content string) {
		injected = append(injected, content)
	})

	streaming, duplicate, err := sm.FollowUpSession(ctx, sess.ID, []api.Message{{Content: "do this"}, {Content: "then that"}}, "")
	require.NoError(t, err)

	assert.True(t, streaming, "an injected follow-up always starts/continues a turn")
	assert.False(t, duplicate)
	assert.Equal(t, []string{"do this", "then that"}, injected)
	assert.Empty(t, fake.followUpContents(), "the runtime queue must be bypassed when an injector is registered")
}

// TestFollowUpSession_UsesRuntimeQueueWithoutInjector verifies the headless
// path (no injector): messages go to the runtime follow-up queue.
func TestFollowUpSession_UsesRuntimeQueueWithoutInjector(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	fake := &recordingFollowUpRuntime{}
	sm := newTestSessionManager(t, sess, &fake.fakeRuntime)
	sm.runtimeSessions.Store(sess.ID, &activeRuntimes{runtime: fake, session: sess})

	_, _, err := sm.FollowUpSession(ctx, sess.ID, []api.Message{{Content: "queued"}}, "")
	require.NoError(t, err)

	assert.Equal(t, []string{"queued"}, fake.followUpContents())
}

// TestFollowUpSession_UnknownSession returns ErrSessionNotRunning.
func TestFollowUpSession_UnknownSession(t *testing.T) {
	t.Parallel()

	sess := session.New()
	sm := newTestSessionManager(t, sess, &fakeRuntime{})

	_, _, err := sm.FollowUpSession(t.Context(), "does-not-exist", []api.Message{{Content: "x"}}, "")
	assert.ErrorIs(t, err, ErrSessionNotRunning)
}

func TestRecallSession_DeleteCancelsRecalledRunAndDoesNotResurrect(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	fake := &fakeRuntime{release: make(chan struct{})}
	sm := newTestSessionManager(t, sess, fake)

	recall := runtime.QueuedMessage{Content: "background job finished"}
	require.NoError(t, sm.recallSession(ctx, sess.ID, recall))
	require.Eventually(t, func() bool {
		return fake.concurrentStreams.Load() == 1
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, sm.DeleteSession(ctx, sess.ID))
	require.NoError(t, sm.WaitStopped(ctx, sess.ID, time.Second))

	_, err := sm.sessionStore.GetSession(ctx, sess.ID)
	assert.ErrorIs(t, err, session.ErrNotFound)
}

// TestFollowUpSession_IdempotencyKeyDedupes verifies that two follow-ups with
// the same Idempotency-Key are delivered only once; the second is reported as
// a duplicate.
func TestFollowUpSession_IdempotencyKeyDedupes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sess := session.New()
	fake := &recordingFollowUpRuntime{}
	sm := newTestSessionManager(t, sess, &fake.fakeRuntime)
	sm.runtimeSessions.Store(sess.ID, &activeRuntimes{runtime: fake, session: sess})

	_, dup1, err := sm.FollowUpSession(ctx, sess.ID, []api.Message{{Content: "once"}}, "key-1")
	require.NoError(t, err)
	assert.False(t, dup1)

	_, dup2, err := sm.FollowUpSession(ctx, sess.ID, []api.Message{{Content: "once"}}, "key-1")
	require.NoError(t, err)
	assert.True(t, dup2, "a repeat with the same key must be a duplicate")

	// A different key is delivered normally.
	_, dup3, err := sm.FollowUpSession(ctx, sess.ID, []api.Message{{Content: "again"}}, "key-2")
	require.NoError(t, err)
	assert.False(t, dup3)

	assert.Equal(t, []string{"once", "again"}, fake.followUpContents(),
		"the deduplicated follow-up must be delivered exactly once")
}

// TestForkSession_CopiesHistoryBeforeUserMessage exercises the happy path:
// forking at the second user message keeps the first user/assistant pair
// and drops everything from the fork point onwards.
func TestForkSession_CopiesHistoryBeforeUserMessage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := session.NewInMemorySessionStore()
	parent := session.New()
	parent.Title = "Parent Title"
	parent.Messages = []session.Item{
		session.NewMessageItem(session.UserMessage("first user")),
		session.NewMessageItem(session.NewAgentMessage("root", &chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "first answer",
		})),
		session.NewMessageItem(session.UserMessage("second user")),
		session.NewMessageItem(session.NewAgentMessage("root", &chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "second answer",
		})),
	}
	require.NoError(t, store.AddSession(ctx, parent))

	sm := NewSessionManager(ctx, config.Sources{}, store, 0, &config.RuntimeConfig{})

	// Fork BEFORE the second user message (user-message ordinal 1).
	forked, err := sm.ForkSession(ctx, parent.ID, 1)
	require.NoError(t, err)

	assert.NotEqual(t, parent.ID, forked.ID, "fork must have a fresh session ID")
	assert.Equal(t, "Parent Title (fork 1)", forked.Title)

	msgs := forked.GetAllMessages()
	require.Len(t, msgs, 2, "fork must contain only the user/assistant pair before the cut point")
	assert.Equal(t, "first user", msgs[0].Message.Content)
	assert.Equal(t, "first answer", msgs[1].Message.Content)

	// The forked session must be persisted and retrievable.
	loaded, err := store.GetSession(ctx, forked.ID)
	require.NoError(t, err)
	assert.Equal(t, forked.ID, loaded.ID)
}

// TestForkSession_ConcurrentWithLiveSessionMutation pins the data-race fix
// for issue #3590: InMemorySessionStore.GetSession returns the live, shared
// *Session pointer (not a copy), so ForkSession's index computation
// (userMessageOrdinalToItemIndex) and session.ForkSession's own copy must
// both go through locked snapshots to stay safe against a concurrent
// AddMessage on that same live session — e.g. the HTTP AddMessage handler
// racing a TUI fork action. Run with -race; before the fix, iterating
// s.Messages directly races the concurrent AddMessage goroutine below.
func TestForkSession_ConcurrentWithLiveSessionMutation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := session.NewInMemorySessionStore()
	parent := session.New()
	parent.Title = "Parent Title"
	parent.Messages = []session.Item{
		session.NewMessageItem(session.UserMessage("first user")),
		session.NewMessageItem(session.NewAgentMessage("root", &chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "first answer",
		})),
	}
	require.NoError(t, store.AddSession(ctx, parent))

	sm := NewSessionManager(ctx, config.Sources{}, store, 0, &config.RuntimeConfig{})

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			parent.AddMessage(session.UserMessage("concurrent"))
		})
		wg.Go(func() {
			if _, err := sm.ForkSession(ctx, parent.ID, 0); err != nil {
				t.Errorf("ForkSession: %v", err)
			}
		})
	}
	wg.Wait()
}

// Regression: repeated forks of the same parent must pick (fork 1),
// (fork 2), (fork 3) rather than three copies of (fork 1).
func TestForkSession_TitleIncrementsAcrossSiblings(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := session.NewInMemorySessionStore()
	parent := session.New()
	parent.Title = "Original"
	parent.Messages = []session.Item{
		session.NewMessageItem(session.UserMessage("u1")),
		session.NewMessageItem(session.NewAgentMessage("root", &chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "a1",
		})),
		session.NewMessageItem(session.UserMessage("u2")),
		session.NewMessageItem(session.NewAgentMessage("root", &chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "a2",
		})),
		session.NewMessageItem(session.UserMessage("u3")),
	}
	require.NoError(t, store.AddSession(ctx, parent))

	sm := NewSessionManager(ctx, config.Sources{}, store, 0, &config.RuntimeConfig{})

	fork1, err := sm.ForkSession(ctx, parent.ID, 0)
	require.NoError(t, err)
	assert.Equal(t, "Original (fork 1)", fork1.Title)

	fork2, err := sm.ForkSession(ctx, parent.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, "Original (fork 2)", fork2.Title)

	fork3, err := sm.ForkSession(ctx, parent.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, "Original (fork 3)", fork3.Title)

	// Forking a fork shares the counter rather than restarting at 1.
	forkOfFork, err := sm.ForkSession(ctx, fork2.ID, 0)
	require.NoError(t, err)
	assert.Equal(t, "Original (fork 4)", forkOfFork.Title)
}

// TestForkSession_OutOfRange covers the validation boundary: negative,
// past-the-end, and equal-to-count ordinals must all fail with
// ErrForkOutOfRange. The equal-to-count case is the regression guard
// for the dropped full-clone shortcut.
func TestForkSession_OutOfRange(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := session.NewInMemorySessionStore()
	parent := session.New()
	parent.Messages = []session.Item{session.NewMessageItem(session.UserMessage("hello"))}
	require.NoError(t, store.AddSession(ctx, parent))

	sm := NewSessionManager(ctx, config.Sources{}, store, 0, &config.RuntimeConfig{})

	_, err := sm.ForkSession(ctx, parent.ID, -1)
	require.ErrorIs(t, err, ErrForkOutOfRange)

	_, err = sm.ForkSession(ctx, parent.ID, 5)
	require.ErrorIs(t, err, ErrForkOutOfRange)

	// Equal to the visible user-message count: previously a silent full
	// clone, now an explicit ErrForkOutOfRange so the contract stays
	// "anchor on a real user turn".
	_, err = sm.ForkSession(ctx, parent.ID, 1)
	require.ErrorIs(t, err, ErrForkOutOfRange)
}

// TestForkSession_DeepCopyIsolatesParent verifies that mutating the
// forked session's messages does not leak back into the parent: this is
// the property that makes a fork safe to edit independently.
func TestForkSession_DeepCopyIsolatesParent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := session.NewInMemorySessionStore()
	parent := session.New()
	parent.Messages = []session.Item{
		session.NewMessageItem(session.UserMessage("original")),
		session.NewMessageItem(session.UserMessage("next")),
	}
	require.NoError(t, store.AddSession(ctx, parent))

	sm := NewSessionManager(ctx, config.Sources{}, store, 0, &config.RuntimeConfig{})

	forked, err := sm.ForkSession(ctx, parent.ID, 1)
	require.NoError(t, err)
	require.Len(t, forked.Messages, 1)

	forked.Messages[0].Message.Message.Content = "mutated"

	parentReloaded, err := store.GetSession(ctx, parent.ID)
	require.NoError(t, err)
	assert.Equal(t, "original", parentReloaded.Messages[0].Message.Message.Content,
		"mutating the fork must not affect the parent")
}

// TestUserMessageOrdinalToItemIndex covers the ordinal translation
// helper: only user-role messages count; system and assistant items
// are skipped; out-of-range ordinals are rejected.
func TestUserMessageOrdinalToItemIndex(t *testing.T) {
	t.Parallel()

	sess := session.New()
	// Items 0..3: user, system, assistant, user. Ordinal 0 → item 0,
	// ordinal 1 → item 3.
	sess.Messages = []session.Item{
		session.NewMessageItem(session.UserMessage("u1")),
		session.NewMessageItem(&session.Message{
			Message: chat.Message{Role: chat.MessageRoleSystem, Content: "sys"},
		}),
		session.NewMessageItem(session.NewAgentMessage("root", &chat.Message{
			Role:    chat.MessageRoleAssistant,
			Content: "a1",
		})),
		session.NewMessageItem(session.UserMessage("u2")),
	}

	idx, err := userMessageOrdinalToItemIndex(sess, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, idx)

	idx, err = userMessageOrdinalToItemIndex(sess, 1)
	require.NoError(t, err)
	assert.Equal(t, 3, idx, "ordinal 1 must skip past the system and assistant items")

	_, err = userMessageOrdinalToItemIndex(sess, 2)
	require.ErrorIs(t, err, ErrForkOutOfRange)

	_, err = userMessageOrdinalToItemIndex(sess, -1)
	require.ErrorIs(t, err, ErrForkOutOfRange)

	_, err = userMessageOrdinalToItemIndex(sess, 99)
	require.ErrorIs(t, err, ErrForkOutOfRange)
}

// TestAddMessage_SQLitePersistedToolResultCappedOnReload pins the read-time
// backstop for the generic API path: SessionManager.AddMessage persists a
// tool result through the store without Session.AddMessage's ingest-time
// cap, so a session reloaded from SQLite carries the unbounded payload.
// Once the runtime stamps the agent's MaxToolResultTokens onto the session
// (runtimeForSession), GetMessages — what actually reaches a provider —
// must bound the result while the persisted history stays as stored.
func TestAddMessage_SQLitePersistedToolResultCappedOnReload(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, err := session.NewSQLiteSessionStore(ctx, filepath.Join(t.TempDir(), "sessions.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	sess := session.New(session.WithUserMessage("run big-tool"))
	sess.AddMessage(session.NewAgentMessage("root", &chat.Message{
		Role: chat.MessageRoleAssistant,
		ToolCalls: []tools.ToolCall{
			{ID: "tc", Function: tools.FunctionCall{Name: "big_tool", Arguments: "{}"}},
		},
	}))
	require.NoError(t, store.AddSession(ctx, sess))

	sm := NewSessionManager(ctx, config.Sources{}, store, 0, &config.RuntimeConfig{})
	oversized := "HEAD" + strings.Repeat("x", 100_000) + "TAIL"
	require.NoError(t, sm.AddMessage(ctx, sess.ID, session.NewAgentMessage("root", &chat.Message{
		Role:       chat.MessageRoleTool,
		ToolCallID: "tc",
		Content:    oversized,
	})))

	reloaded, err := store.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, oversized, storedToolResultContent(t, reloaded, "tc"),
		"persistence must keep the raw result; only what reaches the provider is capped")

	// runtimeForSession copies the agent's cap onto the session before a run.
	agt := agent.New("root", "test instruction", agent.WithMaxToolResultTokens(100))
	reloaded.MaxToolResultTokens = agt.MaxToolResultTokens()

	var got string
	for _, msg := range reloaded.GetMessages(agt) {
		if msg.Role == chat.MessageRoleTool && msg.ToolCallID == "tc" {
			got = msg.Content
		}
	}
	require.NotEmpty(t, got, "tool result must survive sanitizeToolCalls")
	assert.LessOrEqual(t, len(got)/4, 100, "reloaded result must be bounded by the cap")
	assert.True(t, strings.HasPrefix(got, "HEAD"), "head must survive middle-out truncation")
	assert.True(t, strings.HasSuffix(got, "TAIL"), "tail must survive middle-out truncation")

	assert.Equal(t, oversized, storedToolResultContent(t, reloaded, "tc"),
		"GetMessages must not rewrite the in-memory history either")
}

func storedToolResultContent(t *testing.T, sess *session.Session, toolCallID string) string {
	t.Helper()

	for _, msg := range sess.GetAllMessages() {
		if msg.Message.Role == chat.MessageRoleTool && msg.Message.ToolCallID == toolCallID {
			return msg.Message.Content
		}
	}

	require.Failf(t, "tool result not found", "tool_call_id=%s", toolCallID)
	return ""
}

// --- #3584 review item 3: session-scoped elicitation sink for API/server runtimes ---

// TestSessionElicitationSink_MakesSessionEventSourceReplayable pins the fix
// for review item 3: before this, only pkg/app (the TUI) ever registered an
// OnElicitationRequest sink, so every API/server-created runtime had none —
// elicitationHandler's headless fast-decline path ("no sink means no UI")
// therefore fired for every background elicitation raised through the API,
// even though a remote/SSE client could otherwise answer it.
// runtimeForSession registers sessionElicitationSink on every runtime it
// builds; this exercises that exact closure (without needing a full
// runtime/team) and confirms the session gains a replayable event source it
// didn't have before.
func TestSessionElicitationSink_MakesSessionEventSourceReplayable(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sm := NewSessionManager(ctx, config.Sources{}, session.NewInMemorySessionStore(), 0, &config.RuntimeConfig{})

	require.False(t, sm.HasEventSource("sess-1"),
		"a session that was never attached and produced no out-of-band event must have no event source")

	sink := sm.sessionElicitationSink("sess-1")
	ev := runtime.ElicitationRequest("need input", "form", nil, "", "eid-1", "", "bg-child", nil, "agent")
	sink(ev)

	require.True(t, sm.HasEventSource("sess-1"),
		"the sink must lazily create a session-scoped event source on first use")

	streamCtx, cancel := context.WithCancel(ctx)
	var mu sync.Mutex
	var replayed []any
	done := make(chan struct{})
	go func() {
		defer close(done)
		ok := sm.StreamEvents(streamCtx, "sess-1", nil, func(_ uint64, event any) {
			mu.Lock()
			defer mu.Unlock()
			replayed = append(replayed, event)
		})
		assert.True(t, ok)
	}()
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(replayed) == 1
	}, 2*time.Second, time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, replayed, 1)
	assert.Same(t, ev, replayed[0], "the elicitation event must be replayable via GET .../events")
}

// TestSessionElicitationSink_ReusesEventLogAcrossCalls verifies the sink
// doesn't clobber an existing event log (e.g. one already registered via
// RegisterEventSource for an attached session) and that repeated sink
// invocations accumulate rather than overwrite.
func TestSessionElicitationSink_ReusesEventLogAcrossCalls(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sm := NewSessionManager(ctx, config.Sources{}, session.NewInMemorySessionStore(), 0, &config.RuntimeConfig{})

	sink := sm.sessionElicitationSink("sess-2")
	first := runtime.ElicitationRequest("first", "form", nil, "", "eid-1", "", "bg-child-1", nil, "agent")
	second := runtime.ElicitationRequest("second", "form", nil, "", "eid-2", "", "bg-child-2", nil, "agent")
	sink(first)
	sink(second)

	seq, ok := sm.LastEventSeq("sess-2")
	require.True(t, ok)
	assert.Equal(t, uint64(2), seq, "both sink deliveries must land in the same event log")

	streamCtx, cancel := context.WithCancel(ctx)
	var mu sync.Mutex
	var replayed []any
	done := make(chan struct{})
	go func() {
		defer close(done)
		ok := sm.StreamEvents(streamCtx, "sess-2", nil, func(_ uint64, event any) {
			mu.Lock()
			defer mu.Unlock()
			replayed = append(replayed, event)
		})
		assert.True(t, ok)
	}()
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(replayed) == 2
	}, 2*time.Second, time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, replayed, 2)
	assert.Same(t, first, replayed[0])
	assert.Same(t, second, replayed[1])
}

// TestRuntimeForSession_RegistersSessionScopedElicitationSink is an
// end-to-end check that runtimeForSession actually wires
// sessionElicitationSink onto every runtime it builds (not just that the
// sink mechanics work in isolation, per the tests above). It uses a
// harness-backed agent (harness: type: claude-code) so no model provider or
// API key is needed — runtime construction only requires *a* valid agent,
// per LocalRuntime's "has no valid model" guard.
//
// Crucially, this drives the sink through the RETURNED runtime itself (via
// EmitElicitationRequestForTesting) rather than reconstructing a fresh
// sm.sessionElicitationSink(...) closure and calling that in isolation: the
// latter would keep passing even if runtimeForSession's
// `run.OnElicitationRequest(...)` registration were deleted, since it never
// actually exercises what got wired onto `run` (#3584 re-review should-fix
// 1).
func TestRuntimeForSession_RegistersSessionScopedElicitationSink(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := []byte(`agents:
  root:
    description: Test agent
    instruction: Be helpful.
    harness:
      type: claude-code
`)
	store := session.NewInMemorySessionStore()
	sess := session.New()
	require.NoError(t, store.AddSession(ctx, sess))

	sources := config.Sources{"agent.yaml": config.NewBytesSource("agent.yaml", cfg)}
	sm := NewSessionManager(ctx, sources, store, 0, &config.RuntimeConfig{})

	require.False(t, sm.HasEventSource(sess.ID))

	run, _, err := sm.runtimeForSession(ctx, sess, "agent.yaml", "", &config.RuntimeConfig{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = run.Close() })

	lr, ok := run.(*runtime.LocalRuntime)
	require.True(t, ok, "runtimeForSession is expected to build a *runtime.LocalRuntime, got %T", run)

	// Simulate what elicitationHandler does when it raises a background
	// elicitation, but through the sink runtimeForSession actually registered
	// on this specific runtime instance, not a freshly built one.
	lr.EmitElicitationRequestForTesting(runtime.ElicitationRequest("need input", "form", nil, "", "eid-1", "", sess.ID, nil, "root"))
	require.True(t, sm.HasEventSource(sess.ID),
		"runtimeForSession must leave the session able to surface out-of-band elicitations")
}

// --- #3584 re-review blocker: lazily-created event logs must actually close on delete ---

// TestReview_DeleteSessionClosesLazyElicitationEventLog is the regression
// test for the #3584 re-review blocker: ensureEventLog handed lazily-created
// (API-only) event logs a no-op cancel function, so DeleteSession's
// unconditional pe.cancel() call did nothing to the underlying eventLog — a
// client already streaming GET /api/sessions/:id/events for such a session
// would never receive a terminal session_exited event and would never see
// its stream close, contradicting the end-of-session contract documented on
// Server.sessionEvents (and docs/features/api-server/index.md). This proves
// the lazily-created log is actually closed on deletion: session_exited is
// delivered and the blocked StreamEvents call returns.
func TestReview_DeleteSessionClosesLazyElicitationEventLog(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := session.NewInMemorySessionStore()
	sm := NewSessionManager(ctx, config.Sources{}, store, 0, &config.RuntimeConfig{})

	sess := session.New()
	require.NoError(t, store.AddSession(ctx, sess))

	// Lazily create the session's event log the way runtimeForSession's
	// sessionElicitationSink does for a background job's elicitation — no
	// runtime/RegisterEventSource pump is ever registered for this session.
	sm.sessionElicitationSink(sess.ID)(runtime.ElicitationRequest("need input", "form", nil, "", "eid-1", "", sess.ID, nil, "root"))
	require.True(t, sm.HasEventSource(sess.ID))

	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	var mu sync.Mutex
	var received []any
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		sm.StreamEvents(streamCtx, sess.ID, nil, func(_ uint64, event any) {
			mu.Lock()
			defer mu.Unlock()
			received = append(received, event)
		})
	}()

	// Wait for the replay of the elicitation event so the stream is known to
	// be actively connected (not merely about to start) before we delete.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) == 1
	}, 2*time.Second, time.Millisecond)

	require.NoError(t, sm.DeleteSession(ctx, sess.ID))

	select {
	case <-streamDone:
	case <-time.After(2 * time.Second):
		t.Fatal("StreamEvents must return once the session is deleted; a no-op cancel on the lazily-created event log leaves connected /events streams blocked forever")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, received, 2, "expected the elicitation event followed by a terminal session_exited event")
	exited, ok := received[1].(sessionExitedEvent)
	require.True(t, ok, "expected sessionExitedEvent, got %T", received[1])
	assert.Equal(t, "session_exited", exited.Type)
}

// TestReview_BatchDeleteSessionsClosesLazyElicitationEventLog is the batch
// variant of the above: BatchDeleteSessions goes through the same
// pe.cancel() call per session, so it shares the exact same bug and fix.
func TestReview_BatchDeleteSessionsClosesLazyElicitationEventLog(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := session.NewInMemorySessionStore()
	sm := NewSessionManager(ctx, config.Sources{}, store, 0, &config.RuntimeConfig{})

	sess := session.New()
	require.NoError(t, store.AddSession(ctx, sess))

	sm.sessionElicitationSink(sess.ID)(runtime.ElicitationRequest("need input", "form", nil, "", "eid-1", "", sess.ID, nil, "root"))
	require.True(t, sm.HasEventSource(sess.ID))

	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	var mu sync.Mutex
	var received []any
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		sm.StreamEvents(streamCtx, sess.ID, nil, func(_ uint64, event any) {
			mu.Lock()
			defer mu.Unlock()
			received = append(received, event)
		})
	}()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) == 1
	}, 2*time.Second, time.Millisecond)

	deleted, failed := sm.BatchDeleteSessions(ctx, []string{sess.ID})
	assert.Equal(t, 1, deleted)
	assert.Empty(t, failed)

	select {
	case <-streamDone:
	case <-time.After(2 * time.Second):
		t.Fatal("StreamEvents must return once the session is batch-deleted; a no-op cancel on the lazily-created event log leaves connected /events streams blocked forever")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, received, 2, "expected the elicitation event followed by a terminal session_exited event")
	exited, ok := received[1].(sessionExitedEvent)
	require.True(t, ok, "expected sessionExitedEvent, got %T", received[1])
	assert.Equal(t, "session_exited", exited.Type)
}
