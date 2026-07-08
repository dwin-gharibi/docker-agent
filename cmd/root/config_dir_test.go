package root

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveConfigDir_Precedence(t *testing.T) {
	// Not parallel: t.Setenv mutates process-wide env vars. Both vars are
	// explicitly neutralized first so an env leak from the developer's
	// shell cannot skew the assertions.
	t.Setenv(envConfigDir, "")
	t.Setenv(cagentEnvConfigDir, "")
	assert.Empty(t, resolveConfigDir(""))

	t.Setenv(cagentEnvConfigDir, "/from-cagent-env")
	assert.Equal(t, "/from-cagent-env", resolveConfigDir(""))

	t.Setenv(envConfigDir, "/from-docker-agent-env")
	assert.Equal(t, "/from-docker-agent-env", resolveConfigDir(""))

	assert.Equal(t, "/from-flag", resolveConfigDir("/from-flag"))
}
