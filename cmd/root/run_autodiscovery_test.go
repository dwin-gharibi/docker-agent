package root

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRunAgentFileName(t *testing.T) {
	t.Run("explicit argument wins", func(t *testing.T) {
		t.Chdir(t.TempDir())
		require.NoError(t, os.WriteFile("docker-agent.yaml", []byte("agents: {}\n"), 0o644))

		got := (&runExecFlags{}).resolveRunAgentFileName([]string{"custom.yaml"})

		assert.Equal(t, "custom.yaml", got)
	})

	t.Run("discovers docker-agent yaml before yml", func(t *testing.T) {
		t.Chdir(t.TempDir())
		require.NoError(t, os.WriteFile("docker-agent.yaml", []byte("agents: {}\n"), 0o644))
		require.NoError(t, os.WriteFile("docker-agent.yml", []byte("agents: {}\n"), 0o644))

		got := (&runExecFlags{}).resolveRunAgentFileName(nil)

		assert.Equal(t, "docker-agent.yaml", got)
	})

	t.Run("discovers docker-agent yml", func(t *testing.T) {
		t.Chdir(t.TempDir())
		require.NoError(t, os.WriteFile("docker-agent.yml", []byte("agents: {}\n"), 0o644))

		got := (&runExecFlags{}).resolveRunAgentFileName(nil)

		assert.Equal(t, "docker-agent.yml", got)
	})

	t.Run("ignores directories", func(t *testing.T) {
		t.Chdir(t.TempDir())
		require.NoError(t, os.Mkdir("docker-agent.yaml", 0o755))
		require.NoError(t, os.WriteFile("docker-agent.yml", []byte("agents: {}\n"), 0o644))

		got := (&runExecFlags{}).resolveRunAgentFileName(nil)

		assert.Equal(t, "docker-agent.yml", got)
	})

	t.Run("falls back to built-in default", func(t *testing.T) {
		t.Chdir(t.TempDir())

		got := (&runExecFlags{}).resolveRunAgentFileName(nil)

		assert.Empty(t, got)
	})

	t.Run("remote run keeps server-side default", func(t *testing.T) {
		t.Chdir(t.TempDir())
		require.NoError(t, os.WriteFile("docker-agent.yaml", []byte("agents: {}\n"), 0o644))

		got := (&runExecFlags{remoteAddress: "http://127.0.0.1:8080"}).resolveRunAgentFileName(nil)

		assert.Empty(t, got)
	})
}
