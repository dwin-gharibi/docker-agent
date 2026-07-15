package service

import (
	"github.com/docker/docker-agent/pkg/tui/types"
)

// EmbeddedSessionState extends StaticSessionState with the mutable state the
// message-list component maintains: the previous message (for grouping
// consecutive messages from the same sender) and the tool-results visibility
// toggle. Use it to host the message list outside the full TUI application.
type EmbeddedSessionState struct {
	StaticSessionState

	previousMessage *types.Message
	hideToolResults bool
}

func (s *EmbeddedSessionState) PreviousMessage() *types.Message       { return s.previousMessage }
func (s *EmbeddedSessionState) SetPreviousMessage(msg *types.Message) { s.previousMessage = msg }
func (s *EmbeddedSessionState) HideToolResults() bool                 { return s.hideToolResults }
func (s *EmbeddedSessionState) ToggleHideToolResults()                { s.hideToolResults = !s.hideToolResults }
