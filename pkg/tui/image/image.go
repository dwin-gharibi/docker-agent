// Package image prepares and renders images for terminal display.
package image

import (
	"bytes"
	"container/list"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	stdimage "image"
	_ "image/gif"
	"image/png"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tui/styles"
)

const (
	kittyMaxChunkSize        = 4096
	maxImageCols             = 60
	maxImageRows             = 20
	maxInlineRegistryEntries = 128
	markerPrefix             = "\x1b_cagent-image;"
)

type registryEntry struct {
	id    uint32
	image Inline
}

var (
	inlineRegistry = struct {
		sync.Mutex

		entries map[uint32]*list.Element
		order   list.List
	}{entries: make(map[uint32]*list.Element)}
	renderingDisabled atomic.Bool
)

// SetRenderingEnabled controls whether the full-screen TUI reserves image rows.
func SetRenderingEnabled(enabled bool) {
	renderingDisabled.Store(!enabled)
}

// Inline is a PNG image prepared for inline kitty-protocol rendering.
type Inline struct {
	Name    string
	MIME    string
	PNGData []byte
	Width   int
	Height  int
}

// FromToolResult extracts displayable images from a tool result.
func FromToolResult(result *tools.ToolCallResult) []Inline {
	if result == nil {
		return nil
	}

	images := make([]Inline, 0, len(result.Images)+len(result.Documents))
	for i, img := range result.Images {
		name := fmt.Sprintf("image-%d", i+1)
		if inline, ok := FromBase64(name, img.MimeType, img.Data); ok {
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
		if inline, ok := FromBase64(name, doc.MimeType, doc.Data); ok {
			images = append(images, inline)
		}
	}
	return images
}

// FromBase64 decodes and normalizes one base64-encoded image.
func FromBase64(name, mimeType, encoded string) (Inline, bool) {
	if strings.TrimSpace(encoded) == "" {
		return Inline{}, false
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Inline{}, false
	}
	if mimeType == "" {
		mimeType = chat.DetectMimeTypeByContent(data)
	}
	if !chat.IsImageMimeType(mimeType) {
		return Inline{}, false
	}
	if resized, resizeErr := chat.ResizeImage(data, mimeType); resizeErr == nil {
		data = resized.Data
		mimeType = resized.MimeType
	}

	img, _, err := stdimage.Decode(bytes.NewReader(data))
	if err != nil {
		return Inline{}, false
	}
	bounds := img.Bounds()

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return Inline{}, false
	}

	return Inline{
		Name:    name,
		MIME:    mimeType,
		PNGData: pngBuf.Bytes(),
		Width:   bounds.Dx(),
		Height:  bounds.Dy(),
	}, true
}

// Render returns a label, kitty image sequence, and the rows reserved by it.
func Render(img Inline, width int) []string {
	if len(img.PNGData) == 0 || img.Width <= 0 || img.Height <= 0 || width <= 0 {
		return nil
	}

	cols, rows := cellSize(img, width)

	label := "image"
	if img.Name != "" {
		label = img.Name
	}
	if img.MIME != "" {
		label += " (" + img.MIME + ")"
	}

	out := []string{"  " + styles.MutedStyle.Render("🖼 "+label)}
	out = append(out, "  "+KittySequence(img.PNGData, cols, rows))
	for range rows - 1 {
		out = append(out, "")
	}
	return out
}

// RenderMarkers renders a compact marker on every occupied row. The normal
// TUI resolves these after viewport clipping, which lets it crop images that
// are partially scrolled off-screen without copying PNG data into every row.
func RenderMarkers(img Inline, width int) []string {
	if renderingDisabled.Load() || len(img.PNGData) == 0 || img.Width <= 0 || img.Height <= 0 || width <= 0 {
		return nil
	}
	cols, rows := cellSize(img, width)
	id := registerImage(imageID(img.PNGData), img)

	out := make([]string, 0, rows)
	for row := range rows {
		out = append(out, fmt.Sprintf("  %s%d;%d;%d;%d\x1b\\", markerPrefix, id, cols, rows, row))
	}
	return out
}

func cellSize(img Inline, width int) (cols, rows int) {
	cols = min(max(width-4, 1), maxImageCols)
	rows = max(1, (img.Height*cols+2*img.Width-1)/(2*img.Width))
	if rows <= maxImageRows {
		return cols, rows
	}

	// Shrink both dimensions together. Capping rows alone stretches tall and
	// square images horizontally because terminal cells are roughly twice as
	// tall as they are wide.
	cols = min(cols, max(1, maxImageRows*2*img.Width/img.Height))
	rows = min(maxImageRows, max(1, (img.Height*cols+2*img.Width-1)/(2*img.Width)))
	return cols, rows
}

func imageID(data []byte) uint32 {
	h := fnv.New32a()
	_, _ = h.Write(data)
	if id := h.Sum32(); id != 0 {
		return id
	}
	return 1
}

func registerImage(id uint32, img Inline) uint32 {
	inlineRegistry.Lock()
	defer inlineRegistry.Unlock()
	for {
		if elem, ok := inlineRegistry.entries[id]; ok {
			entry := elem.Value.(*registryEntry)
			if bytes.Equal(entry.image.PNGData, img.PNGData) {
				entry.image = img
				inlineRegistry.order.MoveToFront(elem)
				return id
			}
			id++
			if id == 0 {
				id = 1
			}
			continue
		}

		elem := inlineRegistry.order.PushFront(&registryEntry{id: id, image: img})
		inlineRegistry.entries[id] = elem
		if inlineRegistry.order.Len() > maxInlineRegistryEntries {
			oldest := inlineRegistry.order.Back()
			inlineRegistry.order.Remove(oldest)
			delete(inlineRegistry.entries, oldest.Value.(*registryEntry).id)
		}
		return id
	}
}

func registeredImage(id uint32) (Inline, bool) {
	inlineRegistry.Lock()
	defer inlineRegistry.Unlock()
	elem, ok := inlineRegistry.entries[id]
	if !ok {
		return Inline{}, false
	}
	inlineRegistry.order.MoveToFront(elem)
	return elem.Value.(*registryEntry).image, true
}

// KittySequence encodes PNG data as a kitty graphics escape sequence.
func KittySequence(pngData []byte, cols, rows int) string {
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
