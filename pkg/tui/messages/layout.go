package messages

// SidebarPosition identifies where the session info sidebar is placed
// relative to the chat area.
type SidebarPosition string

const (
	// SidebarRight is the default vertical sidebar on the right side.
	SidebarRight SidebarPosition = "right"
	// SidebarLeft is a vertical sidebar on the left side.
	SidebarLeft SidebarPosition = "left"
	// SidebarTop is a compact horizontal band above the chat.
	SidebarTop SidebarPosition = "top"
	// SidebarBottom is a compact horizontal band below the chat.
	SidebarBottom SidebarPosition = "bottom"
)

// ParseSidebarPosition normalizes a raw position string, falling back to
// SidebarRight for empty or unknown values so persisted configs can never
// break the layout.
func ParseSidebarPosition(raw string) SidebarPosition {
	switch SidebarPosition(raw) {
	case SidebarLeft, SidebarTop, SidebarBottom:
		return SidebarPosition(raw)
	default:
		return SidebarRight
	}
}

// SectionSpacing identifies how much blank space separates the sidebar
// sections (blocks) in the vertical sidebar.
type SectionSpacing string

const (
	// SpacingNormal is the default spacing between sidebar sections.
	SpacingNormal SectionSpacing = "normal"
	// SpacingCompact tightens the sidebar by using less space between sections.
	SpacingCompact SectionSpacing = "compact"
	// SpacingRelaxed adds extra breathing room between sections.
	SpacingRelaxed SectionSpacing = "relaxed"
)

// ParseSectionSpacing normalizes a raw spacing string, falling back to
// SpacingNormal for empty or unknown values so persisted configs can never
// break the layout.
func ParseSectionSpacing(raw string) SectionSpacing {
	switch SectionSpacing(raw) {
	case SpacingCompact, SpacingRelaxed:
		return SectionSpacing(raw)
	default:
		return SpacingNormal
	}
}

// BlankLines returns the number of blank lines rendered between sidebar
// sections for this spacing.
func (s SectionSpacing) BlankLines() int {
	switch ParseSectionSpacing(string(s)) {
	case SpacingCompact:
		return 1
	case SpacingRelaxed:
		return 3
	default:
		return 2
	}
}

// LayoutSettings describes the user-customizable TUI layout: where the
// sidebar sits, which of its optional sections are rendered, and how much
// space separates them. The zero value is the default layout (sidebar on
// the right, everything visible, normal spacing).
type LayoutSettings struct {
	SidebarPosition SidebarPosition
	SectionSpacing  SectionSpacing
	HideUsage       bool
	HideAgents      bool
	HideTools       bool
	HideTodos       bool
}

// Layout customization messages.
type (
	// OpenCustomizeDialogMsg opens the layout customization dialog.
	OpenCustomizeDialogMsg struct{}

	// PreviewLayoutMsg applies layout settings live without persisting them.
	PreviewLayoutMsg struct {
		Layout LayoutSettings
	}

	// ApplyLayoutMsg applies layout settings and persists them to the user config.
	ApplyLayoutMsg struct {
		Layout LayoutSettings
	}

	// CancelLayoutPreviewMsg restores the layout that was active before a preview.
	CancelLayoutPreviewMsg struct {
		Original LayoutSettings
	}
)
