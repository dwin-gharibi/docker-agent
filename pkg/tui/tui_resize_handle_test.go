package tui

import (
	"testing"
)

// TestRenderResizeHandle_TinyWidths guards against a panic (negative
// strings.Repeat count) when the terminal reports degenerate sizes such as
// 0x0 or 1x1: the padded inner width goes negative for widths smaller than
// the app's horizontal padding.
func TestRenderResizeHandle_TinyWidths(t *testing.T) {
	t.Parallel()

	m, _ := newTestModel(t)

	for _, width := range []int{-1, 0, 1, appPaddingHorizontal, appPaddingHorizontal + 1, 80} {
		out := m.renderResizeHandle(width)
		if width <= appPaddingHorizontal && out != "" {
			t.Errorf("width %d: expected empty handle, got %q", width, out)
		}
		if width > appPaddingHorizontal && out == "" {
			t.Errorf("width %d: expected a rendered handle", width)
		}
	}
}
