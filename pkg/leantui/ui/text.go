package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// DisplayWidth returns the rendered cell width of s, ignoring ANSI escape
// sequences.
func DisplayWidth(s string) int {
	return ansi.StringWidth(s)
}

func RuneWidth(r rune) int {
	if r == '\t' {
		return 1
	}
	w := runewidth.RuneWidth(r)
	if w < 0 {
		return 0
	}
	return w
}

// Truncate shortens s to at most w cells, appending an ellipsis when it had to
// cut anything.
func Truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if DisplayWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// PadRight pads s with spaces up to w cells. It never Truncates.
func PadRight(s string, w int) string {
	gap := w - DisplayWidth(s)
	if gap <= 0 {
		return s
	}
	return s + strings.Repeat(" ", gap)
}

// WrapANSI hard-wraps s to width w, keeping ANSI styling intact and returning
// one string per physical row. Existing newlines in s start new rows.
func WrapANSI(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	s = strings.ReplaceAll(s, "\t", "    ")
	wrapped := ansi.Hardwrap(s, w, false)
	return strings.Split(wrapped, "\n")
}
