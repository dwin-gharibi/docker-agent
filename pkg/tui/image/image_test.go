package image

import (
	"container/list"
	"testing"

	"github.com/stretchr/testify/assert"
)

func resetInlineRegistry() {
	inlineRegistry.Lock()
	defer inlineRegistry.Unlock()
	inlineRegistry.entries = make(map[uint32]*list.Element)
	inlineRegistry.order.Init()
}

func TestInlineRegistryIsBounded(t *testing.T) {
	resetInlineRegistry()
	defer resetInlineRegistry()

	for id := uint32(1); id <= maxInlineRegistryEntries+1; id++ {
		registerImage(id, Inline{PNGData: []byte{byte(id)}})
	}
	_, oldestPresent := registeredImage(1)
	newest, newestPresent := registeredImage(maxInlineRegistryEntries + 1)
	assert.False(t, oldestPresent)
	assert.True(t, newestPresent)
	assert.Equal(t, []byte{byte(maxInlineRegistryEntries + 1)}, newest.PNGData)
}

func TestInlineRegistryResolvesIDCollisions(t *testing.T) {
	resetInlineRegistry()
	defer resetInlineRegistry()

	firstID := registerImage(42, Inline{PNGData: []byte("first")})
	secondID := registerImage(42, Inline{PNGData: []byte("second")})
	assert.Equal(t, uint32(42), firstID)
	assert.Equal(t, uint32(43), secondID)

	first, ok := registeredImage(firstID)
	assert.True(t, ok)
	assert.Equal(t, []byte("first"), first.PNGData)
	second, ok := registeredImage(secondID)
	assert.True(t, ok)
	assert.Equal(t, []byte("second"), second.PNGData)
}

func TestRenderMarkersDisabledDoesNotReserveRows(t *testing.T) {
	SetRenderingEnabled(false)
	defer SetRenderingEnabled(true)

	markers := RenderMarkers(Inline{PNGData: []byte("png"), Width: 10, Height: 10}, 40)
	assert.Nil(t, markers)
}

func TestCellSizePreservesAspectRatioWhenHeightIsCapped(t *testing.T) {
	t.Parallel()

	cols, rows := cellSize(Inline{Width: 1000, Height: 1000}, 100)
	assert.Equal(t, 40, cols)
	assert.Equal(t, 20, rows)
}

func TestCellSizeUsesAvailableWidthForWideImage(t *testing.T) {
	t.Parallel()

	cols, rows := cellSize(Inline{Width: 1600, Height: 900}, 100)
	assert.Equal(t, 60, cols)
	assert.Equal(t, 17, rows)
}
