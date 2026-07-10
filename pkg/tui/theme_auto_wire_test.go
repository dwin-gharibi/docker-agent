package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tui/styles"
)

// TestColorSchemeWireFormat drives the auto theme from the raw DEC mode 2031
// report bytes a terminal emits (CSI ? 997 ; 1|2 n), going through the same
// ultraviolet decoder bubbletea's input loop uses. This pins the full chain:
// wire bytes -> decoder event -> appModel.Update -> theme switch.
func TestColorSchemeWireFormat(t *testing.T) {
	setupAutoThemeTest(t)
	m, _ := newTestModel(t)

	styles.SetAutoThemeEnabled(true)
	styles.SetTerminalDark(true)
	styles.ApplyTheme(styles.DefaultTheme())

	decode := func(seq string) uv.Event {
		t.Helper()
		var decoder uv.EventDecoder
		n, event := decoder.Decode([]byte(seq))
		require.Equal(t, len(seq), n, "decoder must consume the full sequence %q", seq)
		require.NotNil(t, event)
		return event
	}

	lightEvent := decode("\x1b[?997;2n")
	require.IsType(t, uv.LightColorSchemeEvent{}, lightEvent)
	_, _ = m.Update(lightEvent)
	assert.Equal(t, styles.DefaultLightThemeRef, styles.CurrentTheme().Ref)

	darkEvent := decode("\x1b[?997;1n")
	require.IsType(t, uv.DarkColorSchemeEvent{}, darkEvent)
	_, _ = m.Update(darkEvent)
	assert.Equal(t, styles.DefaultThemeRef, styles.CurrentTheme().Ref)
}
