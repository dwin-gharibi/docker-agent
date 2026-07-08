package tab

import (
	"charm.land/lipgloss/v2"

	"github.com/docker/docker-agent/pkg/tui/styles"
)

// Render renders a titled section flush: the blank space that separates
// consecutive sections is owned by the caller (see sidebar section spacing).
func Render(title, content string, width int) string {
	styleTitle := styles.TabTitleStyle
	styleBody := styles.TabStyle

	return lipgloss.JoinVertical(lipgloss.Top,
		lipgloss.PlaceHorizontal(width, lipgloss.Left,
			styleTitle.PaddingRight(1).Render(title),
			lipgloss.WithWhitespaceChars("─"),
			lipgloss.WithWhitespaceStyle(styleTitle),
		),
		styles.RenderComposite(styleBody.Width(width).PaddingBottom(0), content),
	)
}
