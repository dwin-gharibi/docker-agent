package service

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/runtime"
)

// TestSessionState_AgentUsage covers the per-agent usage snapshot store: set,
// last-write-wins replacement, the empty-name guard, and the miss case.
func TestSessionState_AgentUsage(t *testing.T) {
	t.Parallel()

	s := NewSessionState(nil)

	_, ok := s.AgentUsage("root")
	assert.False(t, ok, "no snapshot before any usage is recorded")

	s.SetAgentUsage("root", runtime.Usage{ContextLength: 1000, ContextLimit: 10000})
	usage, ok := s.AgentUsage("root")
	assert.True(t, ok)
	assert.Equal(t, int64(1000), usage.ContextLength)

	s.SetAgentUsage("root", runtime.Usage{ContextLength: 2000, ContextLimit: 10000})
	usage, ok = s.AgentUsage("root")
	assert.True(t, ok)
	assert.Equal(t, int64(2000), usage.ContextLength, "latest snapshot wins")

	s.SetAgentUsage("", runtime.Usage{ContextLength: 3000})
	_, ok = s.AgentUsage("")
	assert.False(t, ok, "snapshots without an agent name are dropped")
}
