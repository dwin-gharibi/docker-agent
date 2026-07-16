package ui

import (
	"sort"
	"strings"
)

// CommandKind distinguishes built-in lean-TUI commands (handled locally) from
// agent-provided commands and skills (resolved and sent to the agent).
type CommandKind int

const (
	CmdBuiltin CommandKind = iota
	CmdAgent
)

type Command struct {
	Name       string
	Desc       string
	Value      string
	MatchScore func(query string) (int, bool)
	Kind       CommandKind
}

// FilterCommands returns the commands whose name has the given prefix, built-in
// commands first, then agent commands, each group alphabetically sorted.
func FilterCommands(all []Command, prefix string) []Command {
	prefix = strings.ToLower(prefix)
	return filterCommands(all, func(c Command) bool {
		return strings.HasPrefix(strings.ToLower(c.Name), prefix)
	})
}

// FilterScopedCommands ranks argument completions with their configured matcher.
func FilterScopedCommands(all []Command, query string) []Command {
	query = strings.TrimSpace(query)
	type scoredCommand struct {
		command Command
		score   int
	}
	matches := make([]scoredCommand, 0, len(all))
	for _, command := range all {
		score, ok := 0, query == ""
		if command.MatchScore != nil {
			score, ok = command.MatchScore(query)
		}
		if ok {
			matches = append(matches, scoredCommand{command: command, score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	result := make([]Command, len(matches))
	for i, match := range matches {
		result[i] = match.command
	}
	return result
}

func filterCommands(all []Command, match func(Command) bool) []Command {
	var out []Command
	for _, c := range all {
		if match(c) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}
