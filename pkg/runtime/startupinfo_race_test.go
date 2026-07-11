package runtime

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/team"
)

// TestStartupInfoEmittedConcurrentStartAndReEmit pins the fix for
// startupInfoEmitted: it used to be a plain bool checked and set with two
// separate statements ("if r.startupInfoEmitted { return }; r.startupInfoEmitted
// = true"). App.Start emits startup info from a spawned goroutine while
// ResetStartupInfo + EmitStartupInfo (the reEmitStartupInfo path, e.g. on
// /new session) reset and re-emit from another goroutine — the
// check-then-set was racy and could let both goroutines through, or drop
// both. The flag must be an atomic.Bool driven by CompareAndSwap so exactly
// one EmitStartupInfo call wins each round.
func TestStartupInfoEmittedConcurrentStartAndReEmit(t *testing.T) {
	t.Parallel()

	prov := &mockProvider{id: "test/mock-model"}
	root := agent.New("root", "test", agent.WithModel(prov))
	tm := team.New(team.WithAgents(root))

	rt, err := NewLocalRuntime(t.Context(), tm, WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	sess := session.New()

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			events := make(chan Event, 16)
			rt.EmitStartupInfo(t.Context(), sess, NewChannelSink(events))
			close(events)
			for range events {
			}
		})
		wg.Go(func() {
			rt.ResetStartupInfo()
		})
	}
	wg.Wait()
}

// TestStartupInfoEmittedSingleEmissionWithoutReset pins the "avoid
// unnecessary duplication" contract: concurrent EmitStartupInfo calls with
// no ResetStartupInfo interleaved must result in exactly one goroutine
// actually emitting events.
func TestStartupInfoEmittedSingleEmissionWithoutReset(t *testing.T) {
	t.Parallel()

	prov := &mockProvider{id: "test/mock-model"}
	root := agent.New("root", "test", agent.WithModel(prov))
	tm := team.New(team.WithAgents(root))

	rt, err := NewLocalRuntime(t.Context(), tm, WithModelStore(mockModelStore{}))
	require.NoError(t, err)

	sess := session.New()

	var mu sync.Mutex
	var emissions int
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			events := make(chan Event, 16)
			rt.EmitStartupInfo(t.Context(), sess, NewChannelSink(events))
			close(events)
			n := 0
			for range events {
				n++
			}
			if n > 0 {
				mu.Lock()
				emissions++
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	if emissions != 1 {
		t.Errorf("expected exactly 1 emission across concurrent calls, got %d", emissions)
	}
}
