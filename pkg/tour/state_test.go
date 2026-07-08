package tour

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/paths"
)

func isolateConfigDir(t *testing.T) {
	t.Helper()
	paths.SetConfigDir(filepath.Join(t.TempDir(), "config"))
	t.Cleanup(func() { paths.SetConfigDir("") })
}

func TestReadStatus_DefaultsToUnanswered(t *testing.T) {
	isolateConfigDir(t)

	assert.Equal(t, StatusUnanswered, ReadStatus())
}

func TestMarkDone(t *testing.T) {
	isolateConfigDir(t)

	require.NoError(t, MarkDone())

	assert.Equal(t, StatusDone, ReadStatus())
	assert.False(t, ShouldOffer(func(string) string { return "" }))
}

func TestMarkNever(t *testing.T) {
	isolateConfigDir(t)

	require.NoError(t, MarkNever())

	assert.Equal(t, StatusNever, ReadStatus())
	assert.False(t, ShouldOffer(func(string) string { return "" }))
}

func TestShouldOffer_Unanswered(t *testing.T) {
	isolateConfigDir(t)

	assert.True(t, ShouldOffer(func(string) string { return "" }))
}

func TestShouldOffer_DisabledByEnv(t *testing.T) {
	isolateConfigDir(t)

	for _, name := range []string{"DOCKER_AGENT_NO_TOUR", "CAGENT_NO_TOUR"} {
		env := func(key string) string {
			if key == name {
				return "1"
			}
			return ""
		}
		assert.False(t, ShouldOffer(env), name)
		assert.True(t, DisabledByEnv(env), name)
	}
}

func TestReadStatus_UnknownContent(t *testing.T) {
	isolateConfigDir(t)

	require.NoError(t, writeStatus(Status("bogus")))

	assert.Equal(t, StatusUnanswered, ReadStatus())
}
