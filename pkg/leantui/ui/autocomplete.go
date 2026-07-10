package ui

import "strings"

const autocompleteMaxRows = 8

// Autocomplete drives the slash-command popup. It is active whenever the input
// is a partial command: a single token starting with "/" and no spaces yet.
type Autocomplete struct {
	all      []Command
	matches  []Command
	selected int
	Active   bool
}

func NewAutocomplete() *Autocomplete {
	return &Autocomplete{}
}

func (a *Autocomplete) SetCommands(cmds []Command) {
	a.all = cmds
}

// Sync recomputes the popup state from the current editor text. It returns true
// while the popup is showing.
func (a *Autocomplete) Sync(input string) bool {
	if !strings.HasPrefix(input, "/") || strings.ContainsAny(input, " \n") {
		a.Active = false
		a.matches = nil
		a.selected = 0
		return false
	}
	a.matches = FilterCommands(a.all, input[1:])
	a.Active = len(a.matches) > 0
	if a.selected >= len(a.matches) {
		a.selected = len(a.matches) - 1
	}
	if a.selected < 0 {
		a.selected = 0
	}
	return a.Active
}

func (a *Autocomplete) MoveUp() {
	if !a.Active {
		return
	}
	if a.selected > 0 {
		a.selected--
	}
}

func (a *Autocomplete) MoveDown() {
	if !a.Active {
		return
	}
	if a.selected < len(a.matches)-1 {
		a.selected++
	}
}

func (a *Autocomplete) Current() (Command, bool) {
	if !a.Active || a.selected >= len(a.matches) {
		return Command{}, false
	}
	return a.matches[a.selected], true
}

func (a *Autocomplete) Dismiss() {
	a.Active = false
	a.matches = nil
	a.selected = 0
}

// Render returns the popup rows (top to bottom) for the given width.
func (a *Autocomplete) Render(width int) []string {
	if !a.Active || len(a.matches) == 0 {
		return nil
	}

	start := 0
	if a.selected >= autocompleteMaxRows {
		start = a.selected - autocompleteMaxRows + 1
	}
	end := min(start+autocompleteMaxRows, len(a.matches))

	nameWidth := 0
	for _, c := range a.matches {
		if w := len(c.Name) + 1; w > nameWidth {
			nameWidth = w
		}
	}
	nameWidth = min(nameWidth, 24)

	var rows []string
	for i := start; i < end; i++ {
		c := a.matches[i]
		name := PadRight("/"+c.Name, nameWidth)
		line := " " + name + "  " + c.Desc
		line = Truncate(line, width)
		if i == a.selected {
			rows = append(rows, lipglossSelected(line, width))
		} else {
			rows = append(rows, StMuted().Render(line))
		}
	}
	return rows
}

// lipglossSelected highlights the selected popup row across the full width.
func lipglossSelected(line string, width int) string {
	padded := PadRight(line, width)
	return StAccent().Bold(true).Render(padded)
}
