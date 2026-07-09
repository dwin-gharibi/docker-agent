package messages

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/docker/docker-agent/pkg/tui/components/markdown"
	"github.com/docker/docker-agent/pkg/tui/styles"
	"github.com/docker/docker-agent/pkg/tui/types"
)

// copiedFlashDuration is how long a clicked copy label reads "copied" before
// reverting.
const copiedFlashDuration = 1500 * time.Millisecond

// copiedFlash tracks the transient "copied" confirmation shown in place of a
// copy label after it is clicked. The label is addressed by message index and
// line within that message's view so it stays put when content above shifts.
type copiedFlash struct {
	msgIdx    int
	localLine int
	codeBlock bool
	seq       int
}

// copiedFlashExpiredMsg reverts the "copied" confirmation once the flash
// duration elapsed. Seq guards against reverting a newer flash.
type copiedFlashExpiredMsg struct {
	Seq int
}

// flashCopiedLabel swaps the clicked copy label for a transient "copied"
// confirmation and schedules its revert.
func (m *model) flashCopiedLabel(msgIdx, localLine int, codeBlock bool) tea.Cmd {
	m.copiedFlashSeq++
	seq := m.copiedFlashSeq
	m.copiedFlash = &copiedFlash{msgIdx: msgIdx, localLine: localLine, codeBlock: codeBlock, seq: seq}
	return tea.Tick(copiedFlashDuration, func(time.Time) tea.Msg {
		return copiedFlashExpiredMsg{Seq: seq}
	})
}

// handleCopiedFlashExpired clears the flash unless a newer one replaced it.
func (m *model) handleCopiedFlashExpired(msg copiedFlashExpiredMsg) {
	if m.copiedFlash != nil && m.copiedFlash.seq == msg.Seq {
		m.copiedFlash = nil
	}
}

// applyCopiedFlash replaces the flashed copy label with the same-width
// "copied" confirmation on the visible line it lives on. It is a view-time
// overlay: rendered caches are left untouched and the swap disappears on its
// own once the flash state is cleared.
func (m *model) applyCopiedFlash(lines []string, viewportStartLine int) []string {
	f := m.copiedFlash
	if f == nil || f.msgIdx < 0 || f.msgIdx >= len(m.lineOffsets) {
		return lines
	}
	idx := m.lineOffsets[f.msgIdx] + f.localLine - viewportStartLine
	if idx < 0 || idx >= len(lines) {
		return lines
	}

	label := types.MessageCopyLabel
	if f.codeBlock {
		label = markdown.CodeBlockCopyIcon
	}

	line := lines[idx]
	plain := ansi.Strip(line)
	before, _, ok := strings.Cut(plain, label)
	if !ok {
		return lines
	}
	start := ansi.StringWidth(before)
	end := start + ansi.StringWidth(label)

	style := styles.SuccessStyle.Bold(true)
	if f.codeBlock {
		// Keep the code block's background band intact behind the swap.
		if bg := styles.MarkdownStyle().CodeBlock.BackgroundColor; bg != nil {
			style = style.Background(lipgloss.Color(*bg))
		}
	}

	result := make([]string, len(lines))
	copy(result, lines)
	result[idx] = ansi.Cut(line, 0, start) +
		style.Render(types.CopiedFeedbackLabel) +
		ansi.Cut(line, end, ansi.StringWidth(plain))
	return result
}
