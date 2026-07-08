package ui

import (
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	kittyMaxChunkSize = 4096
	kittyMaxImageCols = 80
	kittyMaxImageRows = 30
)

// InlineImage is a PNG image prepared for inline kitty-protocol rendering.
type InlineImage struct {
	Name    string
	MIME    string
	PNGData []byte
	Width   int
	Height  int
}

// RenderInlineImage renders an inline image label and kitty image sequence.
func RenderInlineImage(img InlineImage, width int) []string {
	if len(img.PNGData) == 0 || img.Width <= 0 || img.Height <= 0 {
		return nil
	}

	cols := min(max(width-4, 1), kittyMaxImageCols)
	rows := max(1, (img.Height*cols+img.Width-1)/img.Width/2)
	rows = min(rows, kittyMaxImageRows)

	label := "image"
	if img.Name != "" {
		label = img.Name
	}
	if img.MIME != "" {
		label += " (" + img.MIME + ")"
	}

	out := []string{"  " + StMuted().Render("🖼 "+label)}
	out = append(out, "  "+KittyImageSequence(img.PNGData, cols, rows))
	for range rows - 1 {
		out = append(out, "")
	}
	return out
}

// KittyImageSequence encodes PNG data as a kitty graphics escape sequence.
func KittyImageSequence(pngData []byte, cols, rows int) string {
	encoded := base64.StdEncoding.EncodeToString(pngData)
	var b strings.Builder
	for offset := 0; offset < len(encoded); offset += kittyMaxChunkSize {
		end := min(offset+kittyMaxChunkSize, len(encoded))
		more := 0
		if end < len(encoded) {
			more = 1
		}
		if offset == 0 {
			fmt.Fprintf(&b, "\x1b_Ga=T,t=d,f=100,q=2,C=1,c=%d,r=%d,m=%d;%s\x1b\\", cols, rows, more, encoded[offset:end])
		} else {
			fmt.Fprintf(&b, "\x1b_Gm=%d;%s\x1b\\", more, encoded[offset:end])
		}
	}
	return b.String()
}
