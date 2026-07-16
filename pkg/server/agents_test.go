package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/config/latest"
)

// TestAgentsAPIEntry_ZeroAgents pins the docker/docker-agent#3588 handler
// guard directly: agentsAPIEntry must report ok=false (not panic) for a
// config with no agents, exercising the len(cfg.Agents)==0 check regardless
// of whether validateConfig also rejects such a config at load time
// (defense in depth).
func TestAgentsAPIEntry_ZeroAgents(t *testing.T) {
	t.Parallel()

	cfg := &latest.Config{}

	require.NotPanics(t, func() {
		_, ok := agentsAPIEntry("empty", cfg)
		assert.False(t, ok)
	})
}

func TestAgentsAPIEntry_SingleAndMultiAgent(t *testing.T) {
	t.Parallel()

	single := &latest.Config{Agents: latest.Agents{{Name: "root", Description: "solo"}}}
	agent, ok := agentsAPIEntry("single", single)
	require.True(t, ok)
	assert.False(t, agent.Multi)
	assert.Equal(t, "solo", agent.Description)

	multi := &latest.Config{Agents: latest.Agents{{Name: "root", Description: "lead"}, {Name: "helper"}}}
	agent, ok = agentsAPIEntry("multi", multi)
	require.True(t, ok)
	assert.True(t, agent.Multi)
	assert.Equal(t, "lead", agent.Description)
}
