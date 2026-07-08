package providerutil

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseRerankScores parses a JSON payload of the form {"scores":[...]} and
// validates that it holds exactly expected scores. If the whole payload is
// not valid JSON (e.g. the model added prose around it), the first {...}
// block is extracted and parsed as a fallback. A payload that parses but
// carries the wrong number of scores fails with a count-specific error.
func ParseRerankScores(raw string, expected int) ([]float64, error) {
	type rerankResponse struct {
		Scores []float64 `json:"scores"`
	}

	raw = strings.TrimSpace(raw)

	tryParse := func(s string) ([]float64, bool) {
		var rr rerankResponse
		if err := json.Unmarshal([]byte(s), &rr); err != nil {
			return nil, false
		}
		return rr.Scores, true
	}
	validate := func(scores []float64) ([]float64, error) {
		if len(scores) != expected {
			return nil, fmt.Errorf("expected %d scores, got %d", expected, len(scores))
		}
		return scores, nil
	}

	// First attempt: parse whole string as JSON.
	if scores, ok := tryParse(raw); ok {
		return validate(scores)
	}

	// Fallback: extract the first {...} block and try again, in case the model added prose.
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		if scores, ok := tryParse(raw[start : end+1]); ok {
			return validate(scores)
		}
	}

	return nil, fmt.Errorf("invalid rerank JSON: %s", raw)
}
