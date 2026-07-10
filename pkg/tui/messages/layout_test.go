package messages

import "testing"

func TestParseSectionSpacing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw  string
		want SectionSpacing
	}{
		{"", SpacingNormal},
		{"normal", SpacingNormal},
		{"compact", SpacingCompact},
		{"relaxed", SpacingRelaxed},
		{"bogus", SpacingNormal},
	}
	for _, tt := range tests {
		if got := ParseSectionSpacing(tt.raw); got != tt.want {
			t.Errorf("ParseSectionSpacing(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestSectionSpacingBlankLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		spacing SectionSpacing
		want    int
	}{
		{SpacingCompact, 1},
		{SpacingNormal, 2},
		{SpacingRelaxed, 3},
		{SectionSpacing(""), 2},
		{SectionSpacing("bogus"), 2},
	}
	for _, tt := range tests {
		if got := tt.spacing.BlankLines(); got != tt.want {
			t.Errorf("SectionSpacing(%q).BlankLines() = %d, want %d", tt.spacing, got, tt.want)
		}
	}
}
