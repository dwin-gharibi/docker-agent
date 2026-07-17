package ui

import tuiimage "github.com/docker/docker-agent/pkg/tui/image"

// InlineImage is a PNG image prepared for inline kitty-protocol rendering.
type InlineImage = tuiimage.Inline

// RenderInlineImage renders an inline image label and kitty image sequence.
func RenderInlineImage(img InlineImage, width int) []string {
	return tuiimage.Render(img, width)
}

// KittyImageSequence encodes PNG data as a kitty graphics escape sequence.
func KittyImageSequence(pngData []byte, cols, rows int) string {
	return tuiimage.KittySequence(pngData, cols, rows)
}
