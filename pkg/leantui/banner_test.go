package leantui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/leantui/ui"
)

func TestCommitWelcomePadsBanner(t *testing.T) {
	t.Parallel()
	m := &model{transcript: ui.NewTranscript()}
	m.commitWelcome()

	require.Equal(t, 1, m.transcript.BlockCount())
	lines := m.transcript.BlockLines(0, 80)
	require.GreaterOrEqual(t, len(lines), bannerTopPadding+len(bannerLines))

	for i := range bannerTopPadding {
		assert.Empty(t, ansi.Strip(lines[i]))
	}

	leftPad := strings.Repeat(" ", bannerLeftPadding)
	firstBannerLine := ansi.Strip(lines[bannerTopPadding])
	assert.True(t, strings.HasPrefix(firstBannerLine, leftPad))
	assert.Equal(t, leftPad+bannerLines[0], firstBannerLine)

	helpLine := ansi.Strip(lines[len(lines)-1])
	assert.True(t, strings.HasPrefix(helpLine, leftPad))
}
