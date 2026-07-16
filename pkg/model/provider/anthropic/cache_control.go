package anthropic

import (
	"github.com/anthropics/anthropic-sdk-go"

	"github.com/docker/docker-agent/pkg/tools"
)

// messageCacheBreakpoints returns how many message-level cache_control
// breakpoints a request may use. Anthropic allows at most 4 per request;
// deferred tools consume one on the tool list, leaving one fewer for
// messages.
func messageCacheBreakpoints(requestTools []tools.Tool) int {
	if containsDeferredTool(requestTools) {
		return 1
	}
	return 2
}

// applyMessageCacheControl adds ephemeral cache control to the last content block
// of the last `breakpoints` messages for prompt caching.
func applyMessageCacheControl(messages []anthropic.MessageParam, breakpoints int) {
	for i := len(messages) - 1; i >= 0 && i >= len(messages)-breakpoints; i-- {
		content := messages[i].Content
		if len(content) == 0 {
			continue
		}
		// nil for block kinds without cache control (e.g. thinking blocks).
		if cc := content[len(content)-1].GetCacheControl(); cc != nil {
			*cc = anthropic.NewCacheControlEphemeralParam()
		}
	}
}

// applyBetaMessageCacheControl adds ephemeral cache control to the last content block
// of the last `breakpoints` messages for prompt caching.
func applyBetaMessageCacheControl(messages []anthropic.BetaMessageParam, breakpoints int) {
	for i := len(messages) - 1; i >= 0 && i >= len(messages)-breakpoints; i-- {
		content := messages[i].Content
		if len(content) == 0 {
			continue
		}
		// nil for block kinds without cache control (e.g. thinking blocks).
		if cc := content[len(content)-1].GetCacheControl(); cc != nil {
			*cc = anthropic.NewBetaCacheControlEphemeralParam()
		}
	}
}
