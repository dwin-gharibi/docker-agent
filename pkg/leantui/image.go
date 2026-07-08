package leantui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	"image/png"
	"strings"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/leantui/ui"
	"github.com/docker/docker-agent/pkg/tools"
)

func inlineImagesFromToolResult(result *tools.ToolCallResult) []ui.InlineImage {
	if result == nil {
		return nil
	}

	images := make([]ui.InlineImage, 0, len(result.Images)+len(result.Documents))
	for i, img := range result.Images {
		name := fmt.Sprintf("image-%d", i+1)
		if inline, ok := inlineImageFromBase64(name, img.MimeType, img.Data); ok {
			images = append(images, inline)
		}
	}
	for _, doc := range result.Documents {
		if !chat.IsImageMimeType(doc.MimeType) || doc.Data == "" {
			continue
		}
		name := doc.Name
		if name == "" {
			name = "image"
		}
		if inline, ok := inlineImageFromBase64(name, doc.MimeType, doc.Data); ok {
			images = append(images, inline)
		}
	}
	return images
}

func inlineImageFromBase64(name, mimeType, b64 string) (ui.InlineImage, bool) {
	if strings.TrimSpace(b64) == "" {
		return ui.InlineImage{}, false
	}

	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return ui.InlineImage{}, false
	}
	if mimeType == "" {
		mimeType = chat.DetectMimeTypeByContent(data)
	}
	if !chat.IsImageMimeType(mimeType) {
		return ui.InlineImage{}, false
	}
	if resized, err := chat.ResizeImage(data, mimeType); err == nil {
		data = resized.Data
		mimeType = resized.MimeType
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ui.InlineImage{}, false
	}
	bounds := img.Bounds()

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return ui.InlineImage{}, false
	}

	return ui.InlineImage{
		Name:    name,
		MIME:    mimeType,
		PNGData: pngBuf.Bytes(),
		Width:   bounds.Dx(),
		Height:  bounds.Dy(),
	}, true
}
