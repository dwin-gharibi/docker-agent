package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/paths"
	"github.com/docker/docker-agent/pkg/tui/messages"
	"github.com/docker/docker-agent/pkg/userconfig"
)

// setupLayoutConfigTest isolates the user config in a temp dir. Tests using
// it must not be parallel: the config dir override is process-global.
func setupLayoutConfigTest(t *testing.T) {
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
		HideUsage:       true,
		HideAgents:      true,
		HideTools:       true,
		HideTodos:       true,
	})
	assert.Equal(t, messages.LayoutSettings{
		SidebarPosition: messages.SidebarBottom,
		SectionSpacing:  messages.SpacingCompact,
		HideUsage:       true,
		HideAgents:      true,
		HideTools:       true,
		HideTodos:       true,
	}, got)
}

func TestSaveLayoutToUserConfig_RoundTrip(t *testing.T) {
	setupLayoutConfigTest(t)

	saved := messages.LayoutSettings{
		SidebarPosition: messages.SidebarLeft,
		SectionSpacing:  messages.SpacingRelaxed,
		HideTools:       true,
	}
	require.NoError(t, saveLayoutToUserConfig(saved))

	assert.Equal(t, saved, layoutSettingsFromConfig(userconfig.Get().GetLayout()))
}

func TestSaveLayoutToUserConfig_DefaultsClearEntry(t *testing.T) {
	setupLayoutConfigTest(t)

	require.NoError(t, saveLayoutToUserConfig(messages.LayoutSettings{
		SidebarPosition: messages.SidebarTop,
	}))
	require.NoError(t, saveLayoutToUserConfig(messages.LayoutSettings{
		SidebarPosition: messages.SidebarRight,
		SectionSpacing:  messages.SpacingNormal,
	}))

	cfg, err := userconfig.Load()
	require.NoError(t, err)
	assert.Nil(t, cfg.GetSettings().Layout, "default layout clears the config entry")
}

func TestSaveLayoutToUserConfig_OmitsDefaultPosition(t *testing.T) {
	setupLayoutConfigTest(t)

	require.NoError(t, saveLayoutToUserConfig(messages.LayoutSettings{
		SidebarPosition: messages.SidebarRight,
		SectionSpacing:  messages.SpacingNormal,
		HideUsage:       true,
	}))

	cfg, err := userconfig.Load()
	require.NoError(t, err)
	layout := cfg.GetSettings().Layout
	require.NotNil(t, layout)
	assert.Empty(t, layout.SidebarPosition, "the default position is not written out")
	assert.Empty(t, layout.SectionSpacing, "the default spacing is not written out")
	assert.True(t, layout.HideUsage)
}
