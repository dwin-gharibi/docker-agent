package leantui

import (
	"github.com/docker/docker-agent/pkg/leantui/ui"
	"github.com/docker/docker-agent/pkg/tools"
	tuiimage "github.com/docker/docker-agent/pkg/tui/image"
)

func inlineImagesFromToolResult(result *tools.ToolCallResult) []ui.InlineImage {
	return tuiimage.FromToolResult(result)
}

func inlineImageFromBase64(name, mimeType, encoded string) (ui.InlineImage, bool) {
	return tuiimage.FromBase64(name, mimeType, encoded)
}
