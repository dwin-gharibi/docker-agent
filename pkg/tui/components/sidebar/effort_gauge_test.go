package sidebar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/tui/components/toolcommon"
	"github.com/docker/docker-agent/pkg/tui/styles"
)

// gaugePattern builds the expected ANSI-stripped gauge string for n filled cells
// of the shared six-cell gauge.
func gaugePattern(filled int) string {
	return strings.Repeat(styles.GaugeFilled, filled) +
		strings.Repeat(styles.GaugeEmpty, toolcommon.EffortGaugeCells-filled)
}

// TestThinkingBadgeLevelUsesGauge verifies the level case of thinkingBadge
// renders the shared six-cell gauge (no ✻ glyph) while the glyph-only
// degradation step returns a single ramp-colored filled cell.
func TestThinkingBadgeLevelUsesGauge(t *testing.T) {
	t.Parallel()

	badge, compact := thinkingBadge("high")
	assert.Equal(t, gaugePattern(4), ansi.Strip(badge), "high renders a 4/6-cell gauge")
	assert.NotContains(t, ansi.Strip(badge), styles.ThinkingGlyph, "gauge carries no ✻ glyph")
	assert.Equal(t, styles.GaugeFilled, ansi.Strip(compact), "glyph-only step is a single filled cell")
}

// TestThinkingBadgeUnknownLevelFallsBackToText verifies an unparseable level
// label keeps a plain text badge (no glyph) so unknown/future labels still
// render.
func TestThinkingBadgeUnknownLevelFallsBackToText(t *testing.T) {
	t.Parallel()

	badge, compact := thinkingBadge("on")
	assert.Equal(t, "on", ansi.Strip(badge), "unknown label keeps a plain text badge")
	assert.NotContains(t, ansi.Strip(badge), styles.ThinkingGlyph, "no ✻ glyph")
	assert.Equal(t, "on", ansi.Strip(compact))
}

// TestThinkingBadgeVocabulary verifies the full no-✻ badge vocabulary: none
// renders nothing, off is an empty gauge, adaptive is "auto", and a token budget
// keeps its token glyph.
func TestThinkingBadgeVocabulary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		label   string
		badge   string
		compact string
	}{
		{"", "", ""},
		{"off", strings.Repeat(styles.GaugeEmpty, toolcommon.EffortGaugeCells), styles.GaugeEmpty},
		{"adaptive", "auto", "auto"},
		{"8192", styles.TokenGlyph + " 8.2K", styles.TokenGlyph},
	}
	for _, c := range cases {
		badge, compact := thinkingBadge(c.label)
		assert.Equalf(t, c.badge, ansi.Strip(badge), "badge for %q", c.label)
		assert.Equalf(t, c.compact, ansi.Strip(compact), "compact for %q", c.label)
		assert.NotContainsf(t, ansi.Strip(badge), styles.ThinkingGlyph, "badge for %q must carry no ✻", c.label)
	}
}

// TestCardThinkingLineShowsGaugeAndValue verifies the focus card thinking line
// is "thinking <gauge> <word>" (no ✻): both the gauge and the descriptive word.
func TestCardThinkingLineShowsGaugeAndValue(t *testing.T) {
	t.Parallel()

	got := ansi.Strip(cardThinkingLine("high"))
	assert.Equal(t, "thinking "+gaugePattern(4)+" high", got)
	assert.NotContains(t, got, styles.ThinkingGlyph, "card line carries no ✻ glyph")

	// off shows a dim empty gauge plus the word "off".
	gotOff := ansi.Strip(cardThinkingLine("off"))
	assert.Equal(t, "thinking "+strings.Repeat(styles.GaugeEmpty, toolcommon.EffortGaugeCells)+" off", gotOff)

	// Empty label omits the line entirely.
	assert.Empty(t, ansi.Strip(cardThinkingLine("")))
}

// TestRowGaugeColumnAlignment verifies a roster of effort-level agents renders
// fixed-width six-cell gauges on line 1 that all end in the same right-aligned
// column.
func TestRowGaugeColumnAlignment(t *testing.T) {
	t.Parallel()

	m := newAgentPanelSidebar(t, "root", 40,
		runtime.AgentDetails{Name: "root", Provider: "anthropic", Model: "opus", Thinking: "high"},
		runtime.AgentDetails{Name: "alpha", Provider: "openai", Model: "gpt-5.4-mini", Thinking: "minimal"},
		runtime.AgentDetails{Name: "beta", Provider: "openai", Model: "gpt-5.4", Thinking: "medium"},
		runtime.AgentDetails{Name: "gamma", Provider: "openai", Model: "gpt-4o", Thinking: "max"},
	)

	lines := renderAgentPanel(m)

	wantGauge := map[string]string{
		"alpha": gaugePattern(1),
		"beta":  gaugePattern(3),
		"gamma": gaugePattern(6),
	}
	seen := 0
	end := -1
	for _, l := range lines {
		for name, gauge := range wantGauge {
			// The name sits on line 1; the model (line 2) never contains it.
			if !strings.Contains(l, name) {
				continue
			}
			seen++
			trimmed := strings.TrimRight(l, " ")
			assert.Truef(t, strings.HasSuffix(trimmed, gauge), "row %q should end with gauge %q", trimmed, gauge)
			w := len([]rune(trimmed))
			if end == -1 {
				end = w
			} else {
				assert.Equal(t, end, w, "fixed-width gauges must end in a single column")
			}
		}
	}
	require.Equal(t, len(wantGauge), seen, "every effort-level row should render")
}
