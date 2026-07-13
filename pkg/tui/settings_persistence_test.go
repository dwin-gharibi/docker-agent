package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/paths"
	"github.com/docker/docker-agent/pkg/tui/messages"
	"github.com/docker/docker-agent/pkg/userconfig"
)

// setupSettingsConfigTest isolates the user config in a temp dir. Tests using
// it must not be parallel: the config dir override is process-global.
func setupSettingsConfigTest(t *testing.T) {
	t.Helper()
	paths.SetConfigDir(t.TempDir())
	t.Cleanup(func() { paths.SetConfigDir("") })
}

func TestLayoutSettingsFromConfig(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		messages.LayoutSettings{SidebarPosition: messages.SidebarRight, SectionSpacing: messages.SpacingNormal},
		layoutSettingsFromConfig(userconfig.LayoutSettings{}),
		"empty config falls back to the default position and spacing")

	assert.Equal(t,
		messages.LayoutSettings{SidebarPosition: messages.SidebarRight, SectionSpacing: messages.SpacingNormal},
		layoutSettingsFromConfig(userconfig.LayoutSettings{SidebarPosition: "bogus", SectionSpacing: "bogus"}),
		"unknown values fall back to the defaults")

	got := layoutSettingsFromConfig(userconfig.LayoutSettings{
		SidebarPosition: "bottom",
		SectionSpacing:  "compact",
		HideSessionPath: true,
		HideUsage:       true,
		HideAgents:      true,
		HideTools:       true,
		HideTodos:       true,
	})
	assert.Equal(t, messages.LayoutSettings{
		SidebarPosition: messages.SidebarBottom,
		SectionSpacing:  messages.SpacingCompact,
		HideSessionPath: true,
		HideUsage:       true,
		HideAgents:      true,
		HideTools:       true,
		HideTodos:       true,
	}, got)
}

func TestSaveSettingsToUserConfig_RoundTrip(t *testing.T) {
	setupSettingsConfigTest(t)

	saved := messages.LayoutSettings{
		SidebarPosition: messages.SidebarLeft,
		SectionSpacing:  messages.SpacingRelaxed,
		HideSessionPath: true,
		HideTools:       true,
	}
	require.NoError(t, saveSettingsToUserConfig(saved, messages.SendModeQueue))

	assert.Equal(t, saved, layoutSettingsFromConfig(userconfig.Get().GetLayout()))
	assert.Equal(t, messages.SendModeQueue, messages.ParseSendMode(userconfig.Get().GetBusySendMode()))
}

func TestSaveSettingsToUserConfig_DefaultsClearEntry(t *testing.T) {
	setupSettingsConfigTest(t)

	require.NoError(t, saveSettingsToUserConfig(messages.LayoutSettings{
		SidebarPosition: messages.SidebarTop,
	}, messages.SendModeQueue))
	require.NoError(t, saveSettingsToUserConfig(messages.LayoutSettings{
		SidebarPosition: messages.SidebarRight,
		SectionSpacing:  messages.SpacingNormal,
	}, messages.SendModeSteer))

	cfg, err := userconfig.Load()
	require.NoError(t, err)
	assert.Nil(t, cfg.GetSettings().Layout, "default layout clears the config entry")
	assert.Empty(t, cfg.GetSettings().GetBusySendMode(), "the default send mode is not written out")
}

func TestSaveSettingsToUserConfig_OmitsDefaultPosition(t *testing.T) {
	setupSettingsConfigTest(t)

	require.NoError(t, saveSettingsToUserConfig(messages.LayoutSettings{
		SidebarPosition: messages.SidebarRight,
		SectionSpacing:  messages.SpacingNormal,
		HideUsage:       true,
	}, messages.SendModeSteer))

	cfg, err := userconfig.Load()
	require.NoError(t, err)
	layout := cfg.GetSettings().Layout
	require.NotNil(t, layout)
	assert.Empty(t, layout.SidebarPosition, "the default position is not written out")
	assert.Empty(t, layout.SectionSpacing, "the default spacing is not written out")
	assert.True(t, layout.HideUsage)
}

func TestSaveSettingsToUserConfig_HideSessionPathKeepsEntry(t *testing.T) {
	setupSettingsConfigTest(t)

	saved := messages.LayoutSettings{
		SidebarPosition: messages.SidebarRight,
		SectionSpacing:  messages.SpacingNormal,
		HideSessionPath: true,
	}
	require.NoError(t, saveSettingsToUserConfig(saved, messages.SendModeSteer))

	cfg, err := userconfig.Load()
	require.NoError(t, err)
	layout := cfg.GetSettings().Layout
	require.NotNil(t, layout, "hide_session_path alone must keep the layout entry")
	assert.True(t, layout.HideSessionPath)
	assert.Equal(t, saved, layoutSettingsFromConfig(userconfig.Get().GetLayout()))
}
