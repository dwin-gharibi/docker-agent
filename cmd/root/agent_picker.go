package root

import (
	"context"
	"errors"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/tui/components/toolcommon"
	"github.com/docker/docker-agent/pkg/tui/styles"
)

// defaultAgentPickerRefs is the list of agent refs offered by the picker when
// the user doesn't pass --agent-picker with an explicit list.
var defaultAgentPickerRefs = []string{"default", "coder"}

// errAgentPickerCancelled is returned when the user aborts the picker
// (Esc / Ctrl-C) without choosing an agent.
var errAgentPickerCancelled = errors.New("agent selection cancelled")

// agentChoice is a single entry in the agent picker.
type agentChoice struct {
	ref         string // agent reference as passed on the command line
	description string // one-line description loaded from the agent config
	err         error  // non-nil when the config could not be loaded
}

// loadAgentChoices resolves and loads metadata for each ref so the picker can
// show a name and description. A ref that fails to load is still listed (with
// the error surfaced) so the user can see what went wrong instead of it
// silently disappearing.
func loadAgentChoices(ctx context.Context, refs []string, env environment.Provider) []agentChoice {
	choices := make([]agentChoice, 0, len(refs))
	for _, ref := range refs {
		choice := agentChoice{ref: ref}

		source, err := config.Resolve(ref, env)
		if err != nil {
			choice.err = err
			choices = append(choices, choice)
			continue
		}

		cfg, err := config.Load(ctx, source)
		if err != nil {
			choice.err = err
			choices = append(choices, choice)
			continue
		}

		if len(cfg.Agents) > 0 {
			root := cfg.Agents.First()
			choice.description = root.Description
		}
		if cfg.Metadata.Description != "" {
			choice.description = cfg.Metadata.Description
		}
		choices = append(choices, choice)
	}
	return choices
}

// selectAgentRef shows a full-screen picker and returns the chosen agent ref.
// When only a single ref is supplied there is nothing to choose, so it is
// returned directly without showing any UI.
func selectAgentRef(ctx context.Context, refs []string, env environment.Provider) (string, error) {
	if len(refs) == 0 {
		return "", errors.New("no agent refs to choose from")
	}
	if len(refs) == 1 {
		return refs[0], nil
	}

	choices := loadAgentChoices(ctx, refs, env)
	m := newAgentPickerModel(choices)

	p := tea.NewProgram(m, tea.WithContext(ctx))
	final, err := p.Run()
	if err != nil {
		return "", err
	}

	result, ok := final.(*agentPickerModel)
	if !ok || result.cancelled {
		return "", errAgentPickerCancelled
	}
	return result.choices[result.cursor].ref, nil
}

// agentPickerKeyMap holds the key bindings for the agent picker.
type agentPickerKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Choose key.Binding
	Quit   key.Binding
}

var agentPickerKeys = agentPickerKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Choose: key.NewBinding(
		key.WithKeys("enter", " "),
		key.WithHelp("enter", "select"),
	),
	Quit: key.NewBinding(
		key.WithKeys("esc", "ctrl+c", "q"),
		key.WithHelp("esc", "cancel"),
	),
}

// agentPickerModel is the bubbletea model backing the full-screen picker.
type agentPickerModel struct {
	choices   []agentChoice
	cursor    int
	width     int
	height    int
	cancelled bool
}

func newAgentPickerModel(choices []agentChoice) *agentPickerModel {
	return &agentPickerModel{choices: choices}
}

func (m *agentPickerModel) Init() tea.Cmd { return nil }

func (m *agentPickerModel) moveUp() {
	if m.cursor > 0 {
		m.cursor--
	}
}

func (m *agentPickerModel) moveDown() {
	if m.cursor < len(m.choices)-1 {
		m.cursor++
	}
}

func (m *agentPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, agentPickerKeys.Quit):
			m.cancelled = true
			return m, tea.Quit
		case key.Matches(msg, agentPickerKeys.Up):
			m.moveUp()
			return m, nil
		case key.Matches(msg, agentPickerKeys.Down):
			m.moveDown()
			return m, nil
		case key.Matches(msg, agentPickerKeys.Choose):
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *agentPickerModel) View() tea.View {
	body := m.render()
	centered := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)

	view := tea.NewView(centered)
	view.AltScreen = true
	view.BackgroundColor = styles.Background
	view.WindowTitle = "Select an agent"
	return view
}

// agent picker card dimensions.
const (
	agentPickerCardWidth    = 64
	agentPickerMinCardWidth = 24
)

// cardWidth returns the card width to use, shrinking to fit narrow terminals.
// The card is wrapped by the outer panel border (1) + padding (3) on each
// side, so it must leave room for that chrome.
func (m *agentPickerModel) cardWidth() int {
	w := agentPickerCardWidth
	if m.width > 0 {
		if fit := m.width - 2*(1+3); fit < w {
			w = fit
		}
	}
	if w < agentPickerMinCardWidth {
		w = agentPickerMinCardWidth
	}
	return w
}

func (m *agentPickerModel) render() string {
	title := styles.HighlightWhiteStyle.Render("Choose an agent to run")
	subtitle := styles.MutedStyle.Render("Pick the agent you want to start a session with.")

	cards := make([]string, 0, len(m.choices))
	cardWidth := m.cardWidth()
	for i, choice := range m.choices {
		cards = append(cards, m.renderCard(choice, cardWidth, i == m.cursor))
	}
	list := lipgloss.JoinVertical(lipgloss.Left, cards...)

	help := styles.MutedStyle.Render(
		strings.Join([]string{
			agentPickerKeys.Up.Help().Key + " " + agentPickerKeys.Up.Help().Desc,
			agentPickerKeys.Choose.Help().Key + " " + agentPickerKeys.Choose.Help().Desc,
			agentPickerKeys.Quit.Help().Key + " " + agentPickerKeys.Quit.Help().Desc,
		}, "   "),
	)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		subtitle,
		"",
		list,
		"",
		help,
	)

	return styles.BaseStyle.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.BorderSecondary).
		Padding(1, 3).
		Render(content)
}

func (m *agentPickerModel) renderCard(choice agentChoice, cardWidth int, selected bool) string {
	marker := "  "
	nameStyle := styles.BoldStyle
	borderColor := styles.BorderMuted
	if selected {
		marker = styles.SuccessStyle.Render("❯ ")
		nameStyle = styles.HighlightWhiteStyle
		borderColor = styles.BorderPrimary
	}

	// The marker occupies 2 columns and the card chrome (border + padding)
	// 4, so the ref text gets cardWidth-6.
	header := marker + nameStyle.Render(toolcommon.TruncateText(choice.ref, cardWidth-6))

	// Descriptions and load errors can come from arbitrary (including
	// remote) configs, so collapse them to a single line and truncate to
	// the card width to keep the layout intact. The detail sits inside the
	// card's 2-space indent and 1-column horizontal padding on each side.
	detailWidth := cardWidth - 4
	var detail string
	switch {
	case choice.err != nil:
		detail = styles.ErrorStyle.Render(truncateDetail("failed to load: "+choice.err.Error(), detailWidth))
	case choice.description != "":
		detail = styles.SecondaryStyle.Render(truncateDetail(choice.description, detailWidth))
	default:
		detail = styles.MutedStyle.Render("No description available")
	}

	card := lipgloss.JoinVertical(lipgloss.Left, header, "  "+detail)

	return styles.BaseStyle.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(cardWidth).
		Padding(0, 1).
		Render(card)
}

// truncateDetail collapses whitespace (including newlines) into single spaces
// and truncates the result to width columns. This keeps card-detail text on a
// single line so untrusted or multi-line descriptions can't break the layout.
func truncateDetail(text string, width int) string {
	return toolcommon.TruncateText(strings.Join(strings.Fields(text), " "), width)
}

// prependAgentRef returns args with ref inserted as the leading positional
// argument. After an --agent-picker selection the remaining positional args
// are user messages, and the rest of the run pipeline expects args[0] to be
// the agent ref.
func prependAgentRef(ref string, args []string) []string {
	return append([]string{ref}, args...)
}

// parseAgentPickerRefs splits a comma-separated list of agent refs, trims
// whitespace, and drops empty entries. An empty or all-whitespace input
// yields the built-in defaults.
func parseAgentPickerRefs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return defaultAgentPickerRefs
	}
	var refs []string
	for part := range strings.SplitSeq(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			refs = append(refs, trimmed)
		}
	}
	if len(refs) == 0 {
		return defaultAgentPickerRefs
	}
	return refs
}
