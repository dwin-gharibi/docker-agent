package base

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/config/latest"
)

func TestTrackUsageEnabled(t *testing.T) {
	t.Parallel()

	boolPtr := func(b bool) *bool { return &b }

	t.Run("defaults to on when unset", func(t *testing.T) {
		t.Parallel()
		c := &Config{ModelConfig: latest.ModelConfig{}}
		assert.True(t, c.TrackUsageEnabled())
	})

	t.Run("explicit true", func(t *testing.T) {
		t.Parallel()
		c := &Config{ModelConfig: latest.ModelConfig{TrackUsage: boolPtr(true)}}
		assert.True(t, c.TrackUsageEnabled())
	})

	t.Run("explicit false disables", func(t *testing.T) {
		t.Parallel()
		c := &Config{ModelConfig: latest.ModelConfig{TrackUsage: boolPtr(false)}}
		assert.False(t, c.TrackUsageEnabled())
	})
}
