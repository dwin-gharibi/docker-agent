package leantui

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/leantui/ui"
	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/tui/service"
	tuitypes "github.com/docker/docker-agent/pkg/tui/types"
)

// bareModel builds a model with just the pieces buildLines needs, so the
// rendering pipeline can be exercised without a real App or terminal.
func bareModel(height int) *model {
	const width = 80

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	return &model{
		width:        width,
		height:       height,
		r:            ui.NewRenderer(w, width, height),
		editor:       ui.NewEditor("type here"),
		ac:           ui.NewAutocomplete(),
		transcript:   newTranscript(),
		status:       ui.StatusModel{WorkingDir: "/tmp/project"},
		sessionState: service.NewSessionState(nil),
		usage:        ui.NewUsageTracker(),
	}
}

func TestStreamingGrowthScrollsAndRendersMarkdown(t *testing.T) {
	t.Parallel()
	m := bareModel(10)
	m.busy = true
	m.render() // initial frame

	m.transcript.pending = &pendingBlock{kind: blockAssistant}
	for i := range 40 {
		m.transcript.pending.text.WriteString("Paragraph " + strconv.Itoa(i) + " with some streamed text.\n\n")
		lines, cl, cc := m.buildLines()
		require.NotPanics(t, func() { m.r.Frame(lines, cl, cc) })
	}

	// Content far exceeds the 10-row viewport, so it must have scrolled.
	assert.Positive(t, m.r.ViewportTop())

	// Finalizing the stream turns it into a cached block; the visible output is
	// unchanged because it was already rendered as markdown live.
	m.transcript.flushPending()
	assert.Len(t, m.transcript.blocks, 1)
	require.NotPanics(t, func() {
		lines, cl, cc := m.buildLines()
		m.r.Frame(lines, cl, cc)
	})
}

func TestBuildLinesPlacesCursorOnInput(t *testing.T) {
	t.Parallel()
	m := bareModel(24)
	m.editor.SetText("hello")

	lines, cursorLine, cursorCol := m.buildLines()
	require.NotEmpty(t, lines)
	// The cursor line must point at the input row and the column past the prompt.
	assert.Contains(t, lines[cursorLine], "hello")
	assert.Equal(t, ui.PromptWidth+5, cursorCol)
}

func TestConversationLinesShowsSpinnerWhenBusy(t *testing.T) {
	t.Parallel()
	m := bareModel(24)
	m.busy = true
	lines := m.transcript.lines(80, m.spinnerFrame, m.busy, m.sessionState, nil)
	assert.Contains(t, strings.Join(lines, ""), "Working")
}

func TestToolConfirmationReplacesRunningTool(t *testing.T) {
	t.Parallel()
	m := bareModel(24)
	tv := shellToolView(tuitypes.ToolStatusRunning)
	m.transcript.upsertTool("root", tv.Message().ToolCall, tv.Message().ToolDefinition, tuitypes.ToolStatusRunning)
	require.Equal(t, 1, m.transcript.toolz.Len())

	event := runtime.ToolCallConfirmation(tv.Message().ToolCall, tv.Message().ToolDefinition, "root", nil)
	m.handleEvent(t.Context(), event)

	assert.Zero(t, m.transcript.toolz.Len())
	assert.Zero(t, m.transcript.toolz.ByIDLen())
	require.NotNil(t, m.confirm)
}

func TestBuildLinesConfirmCursorSitsOnOptions(t *testing.T) {
	t.Parallel()
	m := bareModel(24)
	m.confirm = &confirmState{
		tool:     "shell",
		toolView: *shellToolView(tuitypes.ToolStatusConfirmation),
	}

	lines, cursorLine, cursorCol := m.buildLines()
	require.NotEmpty(t, lines)
	require.GreaterOrEqual(t, cursorLine, 0)
	require.Less(t, cursorLine, len(lines))
	assert.Contains(t, lines[cursorLine], "[y] yes")
	assert.Positive(t, cursorCol)
}
