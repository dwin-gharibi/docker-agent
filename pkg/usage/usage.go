// Package usage aggregates token and cost figures out of persisted sessions.
//
// It exists so the numbers the TUI shows in its cost dialog are also reachable
// from a headless run: `docker agent usage` reads the session store and reports
// what was spent, per session, per model, and per tool.
//
// # What the numbers mean
//
// Cost is taken from each session's own cumulative counter, which is what the
// runtime recorded as it ran — this package never re-prices anything. Token
// figures are summed from per-message usage, which is the only place the
// cached-input and cache-write breakdown is persisted.
//
// Those two sources can disagree: a session whose messages carry no usage (an
// older session, or one recorded by a provider with usage tracking disabled)
// reports zero tokens while still reporting a cost. The reverse — tokens with no
// cost — means the model was missing from the pricing catalogue, and is surfaced
// as [SessionRow.CostIncomplete] and [Report.UnpricedModels] rather than being
// silently reported as $0.00.
//
// Per-model *cost* is deliberately absent: cost is persisted per session and per
// non-message item, never per message, so attributing it across the models used
// inside one session is not possible without a schema change.
package usage

import (
	"cmp"
	"slices"
	"time"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/session"
)

// Tokens is a token breakdown. CachedInput and CacheWrite are reported
// separately from Input so the effect of prompt caching is visible.
type Tokens struct {
	Input       int64 `json:"input"`
	CachedInput int64 `json:"cached_input"`
	CacheWrite  int64 `json:"cache_write"`
	Output      int64 `json:"output"`
	Reasoning   int64 `json:"reasoning"`
}

// Total is the headline "tokens moved" figure: input plus output. CachedInput is
// already a subset of Input, and CacheWrite/Reasoning are reported on their own
// rather than folded in, so adding them would double-count.
func (t Tokens) Total() int64 { return t.Input + t.Output }

// addUsage accumulates u into dst. A free function rather than a method so
// Tokens keeps value receivers throughout (it is embedded in JSON-serialized
// rows, where a mixed receiver set is a trap). A nil u is a no-op so callers can
// pass an optional usage without checking.
func addUsage(dst *Tokens, u *chat.Usage) {
	if u == nil {
		return
	}
	dst.Input += u.InputTokens
	dst.CachedInput += u.CachedInputTokens
	dst.CacheWrite += u.CacheWriteTokens
	dst.Output += u.OutputTokens
	dst.Reasoning += u.ReasoningTokens
}

// SessionRow is one session's spend.
type SessionRow struct {
	ID        string    `json:"id"`
	Title     string    `json:"title,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// Models used in the session, sorted and de-duplicated.
	Models []string `json:"models,omitempty"`
	Tokens Tokens   `json:"tokens"`
	Cost   float64  `json:"cost"`
	// CostIncomplete marks a session that moved tokens but recorded no cost,
	// i.e. at least one model was missing from the pricing catalogue.
	CostIncomplete bool `json:"cost_incomplete,omitempty"`
}

// ModelRow is one model's token spend across every session in the report.
// Calls counts model responses, not tool calls.
type ModelRow struct {
	Model  string `json:"model"`
	Calls  int    `json:"calls"`
	Tokens Tokens `json:"tokens"`
}

// ToolRow is how often a tool was called across every session in the report.
type ToolRow struct {
	Tool  string `json:"tool"`
	Calls int    `json:"calls"`
}

// Report is the aggregate view. Sessions are newest first; Models and Tools are
// ordered by size descending so the cost drivers come first.
type Report struct {
	Sessions []SessionRow `json:"sessions"`
	Models   []ModelRow   `json:"models"`
	Tools    []ToolRow    `json:"tools"`
	Tokens   Tokens       `json:"tokens"`
	Cost     float64      `json:"cost"`
	// UnpricedModels lists models that moved tokens in a session that recorded
	// no cost. Sorted.
	UnpricedModels []string `json:"unpriced_models,omitempty"`
}

// Aggregate builds a [Report] from the supplied sessions. Nil entries are
// skipped, so a caller can pass a store listing straight through.
func Aggregate(sessions []*session.Session) Report {
	var report Report

	modelTokens := map[string]*ModelRow{}
	toolCalls := map[string]int{}
	unpriced := map[string]struct{}{}

	for _, s := range sessions {
		if s == nil {
			continue
		}

		row := SessionRow{ID: s.ID, Title: s.Title, CreatedAt: s.CreatedAt, Cost: s.Cost}
		sessionModels := map[string]struct{}{}

		for i := range s.Messages {
			item := &s.Messages[i]

			// Non-message items (compaction summaries and the like) carry their
			// own model and usage; they are real spend.
			model, itemUsage := itemModelAndUsage(item)
			if itemUsage == nil && model == "" {
				continue
			}

			addUsage(&row.Tokens, itemUsage)
			if model != "" {
				sessionModels[model] = struct{}{}
				mr, ok := modelTokens[model]
				if !ok {
					mr = &ModelRow{Model: model}
					modelTokens[model] = mr
				}
				mr.Calls++
				addUsage(&mr.Tokens, itemUsage)
			}

			if item.Message != nil {
				for _, tc := range item.Message.Message.ToolCalls {
					if tc.Function.Name != "" {
						toolCalls[tc.Function.Name]++
					}
				}
			}
		}

		row.Models = sortedKeys(sessionModels)

		// Tokens moved but nothing was charged: the model is unpriced.
		if row.Tokens.Total() > 0 && row.Cost == 0 {
			row.CostIncomplete = true
			for _, m := range row.Models {
				unpriced[m] = struct{}{}
			}
		}

		report.Sessions = append(report.Sessions, row)
		report.Cost += row.Cost
		report.Tokens.Input += row.Tokens.Input
		report.Tokens.CachedInput += row.Tokens.CachedInput
		report.Tokens.CacheWrite += row.Tokens.CacheWrite
		report.Tokens.Output += row.Tokens.Output
		report.Tokens.Reasoning += row.Tokens.Reasoning
	}

	slices.SortFunc(report.Sessions, func(a, b SessionRow) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})

	for _, mr := range modelTokens {
		report.Models = append(report.Models, *mr)
	}
	slices.SortFunc(report.Models, func(a, b ModelRow) int {
		if c := cmp.Compare(b.Tokens.Total(), a.Tokens.Total()); c != 0 {
			return c
		}
		return cmp.Compare(a.Model, b.Model)
	})

	for name, calls := range toolCalls {
		report.Tools = append(report.Tools, ToolRow{Tool: name, Calls: calls})
	}
	slices.SortFunc(report.Tools, func(a, b ToolRow) int {
		if c := cmp.Compare(b.Calls, a.Calls); c != 0 {
			return c
		}
		return cmp.Compare(a.Tool, b.Tool)
	})

	report.UnpricedModels = sortedKeys(unpriced)
	return report
}

// itemModelAndUsage returns the model and usage behind one session item,
// whichever shape it takes: an assistant message, or a non-message item such as
// a compaction summary that records its own spend.
func itemModelAndUsage(item *session.Item) (string, *chat.Usage) {
	if item.Message != nil {
		return item.Message.Message.Model, item.Message.Message.Usage
	}
	return item.Model, item.Usage
}

func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
