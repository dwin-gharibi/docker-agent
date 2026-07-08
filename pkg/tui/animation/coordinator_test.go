package animation

import (
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getActiveCount(c *Coordinator) int32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active
}

func runCmdWithTimeout(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	require.NotNil(t, cmd)

	done := make(chan tea.Msg, 1)
	go func() {
		done <- cmd()
	}()

	timeout := time.NewTimer(250 * time.Millisecond)
	defer timeout.Stop()

	select {
	case msg := <-done:
		return msg
	case <-timeout.C:
		t.Fatal("timed out waiting for tick command")
	}

	return nil
}

func runTickCmd(t *testing.T, cmd tea.Cmd) TickMsg {
	t.Helper()

	msg := runCmdWithTimeout(t, cmd)
	tickMsg, ok := msg.(TickMsg)
	require.True(t, ok)

	return tickMsg
}

func TestGlobalCoordinatorLifecycle(t *testing.T) {
	t.Parallel()
	c := &Coordinator{}

	// No active animations = no tick
	require.Nil(t, c.StartTick())

	// First registration starts tick
	firstTick := c.StartTickIfFirst()
	tickMsg := runTickCmd(t, firstTick)
	assert.Equal(t, 1, tickMsg.Frame)

	// Subsequent tick continues
	nextTick := c.StartTick()
	tickMsg = runTickCmd(t, nextTick)
	assert.Equal(t, 2, tickMsg.Frame)

	// Second StartTickIfFirst registers but doesn't return tick (not first)
	cmd := c.StartTickIfFirst()
	require.Nil(t, cmd)
	assert.Equal(t, int32(2), getActiveCount(c))

	// Unregister one, still active
	c.Unregister()
	require.True(t, c.HasActive())
	require.NotNil(t, c.StartTick())

	// Unregister last one
	c.Unregister()
	require.False(t, c.HasActive())
	require.Nil(t, c.StartTick())
}

func TestUnregisterNeverGoesNegative(t *testing.T) {
	t.Parallel()
	c := &Coordinator{}

	// Multiple unregisters when already at 0
	c.Unregister()
	c.Unregister()
	c.Unregister()

	assert.Equal(t, int32(0), getActiveCount(c))
	require.False(t, c.HasActive())
}

func TestConcurrentRegisterUnregister(t *testing.T) {
	t.Parallel()
	c := &Coordinator{}

	const goroutines = 100
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Half goroutines do register
	for range goroutines {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				c.Register()
			}
		}()
	}

	// Half goroutines do unregister
	for range goroutines {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				c.Unregister()
			}
		}()
	}

	wg.Wait()

	// Should have exactly goroutines * opsPerGoroutine registers
	// minus whatever unregisters succeeded (capped at 0)
	// Final count should be >= 0
	count := getActiveCount(c)
	assert.GreaterOrEqual(t, count, int32(0), "active count should never be negative")
}

func TestConcurrentStartTickIfFirst(t *testing.T) {
	t.Parallel()
	c := &Coordinator{}

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	cmds := make(chan tea.Cmd, goroutines)

	// Many goroutines race to be "first"
	for range goroutines {
		go func() {
			defer wg.Done()
			cmd := c.StartTickIfFirst()
			cmds <- cmd
		}()
	}

	wg.Wait()
	close(cmds)

	// Count non-nil commands (ticks started)
	ticksStarted := 0
	for cmd := range cmds {
		if cmd != nil {
			ticksStarted++
		}
	}

	// Exactly one should have started the tick
	assert.Equal(t, 1, ticksStarted, "exactly one goroutine should start the tick")
	// All should have registered
	assert.Equal(t, int32(goroutines), getActiveCount(c))
}
