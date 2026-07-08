package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/docker-agent/pkg/session"
)

func TestSessionUsageCarriesCompactionThreshold(t *testing.T) {
	t.Parallel()

	sess := session.New(session.WithUserMessage("hi"))
	sess.InputTokens = 600
	sess.OutputTokens = 400

	u := SessionUsage(sess, 100_000)
	assert.Zero(t, u.CompactionThreshold, "threshold is 0 (unknown) when omitted")

	u = SessionUsage(sess, 100_000, 0.75)
	assert.InDelta(t, 0.75, u.CompactionThreshold, 0.0001)
	assert.Equal(t, int64(100_000), u.ContextLimit)
	assert.Equal(t, int64(1_000), u.ContextLength)
}
