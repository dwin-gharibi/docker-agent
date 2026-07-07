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

// LayoutSettings describes the user-customizable TUI layout: where the
// sidebar sits and which of its optional sections are rendered. The zero
// value is the default layout (sidebar on the right, everything visible).
type LayoutSettings struct {
	SidebarPosition SidebarPosition
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
