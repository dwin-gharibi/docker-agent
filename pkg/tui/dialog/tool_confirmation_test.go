package dialog

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tui/service"
)

// newConfirmationEvent builds a tool-call confirmation event carrying the
// supplied metadata for use in the dialog tests.
func newConfirmationEvent(metadata map[string]string) *runtime.ToolCallConfirmationEvent {
	return &runtime.ToolCallConfirmationEvent{
		Type:           "tool_call_confirmation",
		ToolCall:       tools.ToolCall{ID: "x", Function: tools.FunctionCall{Name: "shell", Arguments: "{}"}},
		ToolDefinition: tools.Tool{Name: "shell"},
		Metadata:       metadata,
	}
}

func TestToolConfirmationDialog_RendersMetadata(t *testing.T) {
	t.Parallel()

	dialog := NewToolConfirmationDialog(
		newConfirmationEvent(map[string]string{"danger": "high", "reason": "policy-x"}),
		&service.SessionState{},
	)
	_, _ = dialog.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	view := ansi.Strip(dialog.View())
	assert.Contains(t, view, "Metadata")
	assert.Contains(t, view, "danger: high")
	assert.Contains(t, view, "reason: policy-x")
}

func TestToolConfirmationDialog_NoMetadataSection_WhenEmpty(t *testing.T) {
	t.Parallel()

	dialog := NewToolConfirmationDialog(newConfirmationEvent(nil), &service.SessionState{})
	_, _ = dialog.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	view := ansi.Strip(dialog.View())
	assert.NotContains(t, view, "Metadata")
}

func TestToolConfirmationDialog_MetadataKeysSorted(t *testing.T) {
	t.Parallel()

	dialog := NewToolConfirmationDialog(
		newConfirmationEvent(map[string]string{"zebra": "1", "apple": "2", "mango": "3"}),
		&service.SessionState{},
	)
	_, _ = dialog.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	view := ansi.Strip(dialog.View())
	apple := strings.Index(view, "apple:")
	mango := strings.Index(view, "mango:")
	zebra := strings.Index(view, "zebra:")
	require.NotEqual(t, -1, apple)
	require.NotEqual(t, -1, mango)
	require.NotEqual(t, -1, zebra)
	assert.Less(t, apple, mango, "keys must render in sorted order")
	assert.Less(t, mango, zebra, "keys must render in sorted order")
}

// TestToolConfirmationDialog_RendersSafetyWarning pins the destructive-
// command UX: when the confirmation event carries the safer_shell
// builtin's `blast_radius` metadata, the dialog composes a polished
// warning block instead of rendering raw key/value pairs. The
// convention keys (blast_radius, category, reason) are suppressed
// from the plain Metadata section.
func TestToolConfirmationDialog_RendersSafetyWarning(t *testing.T) {
	t.Parallel()

	dialog := NewToolConfirmationDialog(
		newConfirmationEvent(map[string]string{
			"blast_radius": "high",
			"category":     "fs-delete",
			"reason":       "Command matches destructive operation: rm -rf <path>",
		}),
		&service.SessionState{},
	)
	_, _ = dialog.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	view := ansi.Strip(dialog.View())
	assert.Contains(t, view, "Destructive command", "warning block must name what it's warning about")
	assert.Contains(t, view, "high blast radius", "warning block must name the severity in prose")
	assert.Contains(t, view, "Command matches destructive operation: rm -rf <path>",
		"the matched-pattern reason must surface as supporting context")
	assert.NotContains(t, view, "blast_radius:",
		"raw blast_radius key must not appear; the warning block replaces it")
	assert.NotContains(t, view, "category: fs-delete",
		"raw category key must not appear when blast_radius is in play")
	assert.NotContains(t, view, "Metadata",
		"the plain Metadata section must not render when only convention keys are present")
}

// TestToolConfirmationDialog_RendersSafetyWarningPlusExtraMetadata
// covers the case where a permission_request hook contributes its own
// metadata alongside safer_shell's verdict. The warning block uses
// the safer_shell convention keys, and the extra keys still render
// as plain pairs in the Metadata section.
func TestToolConfirmationDialog_RendersSafetyWarningPlusExtraMetadata(t *testing.T) {
	t.Parallel()

	dialog := NewToolConfirmationDialog(
		newConfirmationEvent(map[string]string{
			"blast_radius": "medium",
			"reason":       "rm without recursion flag",
			"team_policy":  "review-required",
			"ticket":       "SEC-1234",
		}),
		&service.SessionState{},
	)
	_, _ = dialog.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	view := ansi.Strip(dialog.View())
	assert.Contains(t, view, "medium blast radius", "warning block consumes blast_radius")
	assert.Contains(t, view, "rm without recursion flag", "warning block consumes reason")
	assert.Contains(t, view, "team_policy: review-required",
		"non-convention keys still render as plain pairs")
	assert.Contains(t, view, "ticket: SEC-1234")
}

// TestToolConfirmationDialog_ReasonOutsideSafetyVerdictRendersPlain
// pins the orthogonality: the `reason` key is generic and can be
// used by permission_request hooks for unrelated purposes. When
// blast_radius is NOT present, reason renders as a plain pair so
// existing permission_request consumers aren't affected.
func TestToolConfirmationDialog_ReasonOutsideSafetyVerdictRendersPlain(t *testing.T) {
	t.Parallel()

	dialog := NewToolConfirmationDialog(
		newConfirmationEvent(map[string]string{"reason": "policy-x"}),
		&service.SessionState{},
	)
	_, _ = dialog.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	view := ansi.Strip(dialog.View())
	assert.Contains(t, view, "Metadata")
	assert.Contains(t, view, "reason: policy-x",
		"without blast_radius, reason is just a regular metadata key")
	assert.NotContains(t, view, "Destructive command",
		"no warning block without a blast_radius classification")
}
