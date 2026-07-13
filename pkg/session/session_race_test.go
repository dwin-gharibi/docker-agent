package session

import (
	"sync"
	"testing"

	"github.com/docker/docker-agent/pkg/chat"
)

func TestAddMessageUsageRecordConcurrent(t *testing.T) {
	t.Parallel()

	s := New()
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			s.AddMessageUsageRecord("agent", "model", 0.1, &chat.Usage{InputTokens: 10, OutputTokens: 5})
		})
	}
	wg.Wait()
	if got := len(s.MessageUsageHistory); got != 100 {
		t.Errorf("expected 100 records, got %d", got)
	}
}

// TestCompactionInputConcurrent pins the data-race fix for the
// compactor: CompactionInput must read s.Messages under s.mu (via
// snapshotItems) so it stays safe against concurrent AddMessage and
// ApplyCompaction calls. Run with -race; without the lock the slice
// header read aliases the live backing array and the race detector
// flags the AddMessage append.
func TestCompactionInputConcurrent(t *testing.T) {
	t.Parallel()

	s := New()
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			s.AddMessage(&Message{Message: chat.Message{Role: chat.MessageRoleUser, Content: "u"}})
		})
		wg.Go(func() {
			_, _, _ = s.CompactionInput()
		})
	}
	// One concurrent ApplyCompaction-shaped write to exercise the same
	// lock from a writer that also bumps the cumulative token counts.
	wg.Go(func() {
		s.ApplyCompaction(0, 0, Item{Summary: "snap"})
	})
	wg.Wait()
}

// TestAddMessageReturnsAppendedIndex pins AddMessage's return-value
// contract: the index of the item it just appended. Hot paths (e.g.
// pkg/runtime/loop.go's UserMessageEvent emission) rely on this instead of
// a separate len(sess.Messages)-1 read, which would race with a concurrent
// AddMessage/ApplyCompaction and could observe a later, larger length.
func TestAddMessageReturnsAppendedIndex(t *testing.T) {
	t.Parallel()

	s := New()
	if got := s.AddMessage(UserMessage("a")); got != 0 {
		t.Errorf("expected index 0, got %d", got)
	}
	if got := s.AddMessage(UserMessage("b")); got != 1 {
		t.Errorf("expected index 1, got %d", got)
	}
}

// TestAddMessageConcurrentReturnsUniqueIndices pins the atomicity of
// AddMessage's returned index under concurrent callers: every call must
// observe the position of its own append (under s.mu), never a stale or
// duplicate one, which is what makes the returned value safe to stamp an
// event's SessionPosition with instead of reading len(sess.Messages)-1
// after the fact.
func TestAddMessageConcurrentReturnsUniqueIndices(t *testing.T) {
	t.Parallel()

	s := New()
	const n = 200
	indices := make(chan int, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			indices <- s.AddMessage(&Message{Message: chat.Message{Role: chat.MessageRoleUser, Content: "u"}})
		})
	}
	wg.Wait()
	close(indices)

	seen := make(map[int]bool, n)
	for idx := range indices {
		if seen[idx] {
			t.Fatalf("duplicate index %d returned by concurrent AddMessage calls", idx)
		}
		seen[idx] = true
	}
	if len(seen) != n {
		t.Errorf("expected %d unique indices, got %d", n, len(seen))
	}
}

// TestBranchSessionConcurrent pins the data-race fix for branch/fork:
// branchSessionWithTitle (BranchSession/ForkSession) must read
// parent.Messages under parent.mu (via snapshotItems), mirroring Clone,
// so it stays safe against a concurrent AddMessage on the same live
// session — e.g. the HTTP AddMessage path racing a TUI branch/fork action.
// Run with -race; without the lock the branchAtPosition bounds check and
// the parent.Messages[i] reads alias the live backing array and the race
// detector flags the AddMessage append.
func TestBranchSessionConcurrent(t *testing.T) {
	t.Parallel()

	parent := New(WithUserMessage("seed"))
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			parent.AddMessage(&Message{Message: chat.Message{Role: chat.MessageRoleUser, Content: "u"}})
		})
		wg.Go(func() {
			if _, err := BranchSession(parent, 1); err != nil {
				t.Errorf("BranchSession: %v", err)
			}
		})
		wg.Go(func() {
			if _, err := ForkSession(parent, 1); err != nil {
				t.Errorf("ForkSession: %v", err)
			}
		})
	}
	wg.Wait()
}

// TestForkSessionConcurrentWithSubSessionMutation pins the same fix for
// cloneSubSession: forking a session that contains a sub-session (e.g. a
// background agent task) must snapshot the sub-session's own Messages
// under its own mu, since that sub-session can still be actively appended
// to while the top-level session is being forked.
func TestForkSessionConcurrentWithSubSessionMutation(t *testing.T) {
	t.Parallel()

	sub := New(WithUserMessage("sub-seed"))
	parent := New(WithUserMessage("seed"))
	parent.AddSubSession(sub)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			sub.AddMessage(&Message{Message: chat.Message{Role: chat.MessageRoleUser, Content: "u"}})
		})
		wg.Go(func() {
			if _, err := ForkSession(parent, 2); err != nil {
				t.Errorf("ForkSession: %v", err)
			}
		})
	}
	wg.Wait()
}
