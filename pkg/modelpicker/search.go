package modelpicker

import (
	"sort"
	"strings"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"

	"github.com/docker/docker-agent/pkg/runtime"
)

type scoredChoice struct {
	choice runtime.ModelChoice
	score  int
}

// Score returns fzf's match score for a model choice and query.
func Score(choice runtime.ModelChoice, query string) (int, bool) {
	query = strings.TrimSpace(query)
	if query == "" {
		return 0, true
	}

	chars := util.ToChars([]byte(choice.Name + " " + choice.Provider + " " + choice.Model + " " + choice.Ref))
	result, _ := algo.FuzzyMatchV1(
		false,
		false,
		true,
		&chars,
		[]rune(strings.ToLower(query)),
		true,
		nil,
	)
	return result.Score, result.Start >= 0
}

// Filter returns matching choices ranked by fzf score. An empty query keeps
// the input order.
func Filter(choices []runtime.ModelChoice, query string) []runtime.ModelChoice {
	if strings.TrimSpace(query) == "" {
		return append([]runtime.ModelChoice(nil), choices...)
	}

	matches := make([]scoredChoice, 0, len(choices))
	for _, choice := range choices {
		if score, ok := Score(choice, query); ok {
			matches = append(matches, scoredChoice{choice: choice, score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	result := make([]runtime.ModelChoice, len(matches))
	for i, match := range matches {
		result[i] = match.choice
	}
	return result
}
