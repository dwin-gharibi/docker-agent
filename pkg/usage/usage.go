// Package usage aggregates token and cost figures out of persisted sessions.
//
// It exists so the numbers the TUI shows in its cost dialog are also reachable
// from a headless run: `docker agent usage` reads the session store and reports
// what was spent, per session, per model, and per tool.
//
// # What the numbers mean
//
// Cost is summed from the per-message cost the runtime recorded as it ran — this
// package never re-prices anything. Token figures come from the same per-message
// usage, which is also the only place the cached-input and cache-write breakdown
// is persisted. Both are read from the same items, so a per-model cost breakdown
// falls out for free.
//
// Cost and tokens are both read per message, so they agree with each other and
// with what the TUI's cost dialog shows. Tokens with no cost means the model was
// missing from the pricing catalogue, and is surfaced as
// [SessionRow.CostIncomplete] and [Report.UnpricedModels] rather than being
// silently reported as $0.00.
//
// # Sub-sessions
//
// Delegated work lives in sub-sessions, and in a multi-agent run that is where
// most of the spend is. Aggregation recurses into them, matching
// [session.Session.TotalCost] and the TUI. A sub-agent's tokens, cost, model
// calls and tool calls all land in the parent session's row, since that is the
// unit a user starts and pays for.
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
//
// Total is for display. Use [Tokens.AnySpend] to ask whether anything was
// consumed at all — a run can burn reasoning tokens without moving a single
// input or output token, and Total would report 0 for it.
func (t Tokens) Total() int64 { return t.Input + t.Output }

// AnySpend reports whether any tokens at all were consumed, including the kinds
// [Tokens.Total] deliberately leaves out. It is what "did this cost anything?"
// should be keyed on.
func (t Tokens) AnySpend() bool {
	return t.Input > 0 || t.CachedInput > 0 || t.CacheWrite > 0 || t.Output > 0 || t.Reasoning > 0
}

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
//
// Calls counts model responses, not tool calls, and counts a response whether or
// not the provider reported usage for it: the call happened either way, and
// dropping it would hide a model that a usage-tracking-disabled provider served
// entirely. Unmetered is how many of those calls carried no usage, so short
// token columns are explained rather than mysterious.
type ModelRow struct {
	Model     string  `json:"model"`
	Calls     int     `json:"calls"`
	Unmetered int     `json:"unmetered_calls,omitempty"`
	Tokens    Tokens  `json:"tokens"`
	Cost      float64 `json:"cost"`
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

	models := map[string]*ModelRow{}
	toolCalls := map[string]int{}
	unpriced := map[string]struct{}{}

	for _, s := range sessions {
		if s == nil {
			continue
		}

		row := SessionRow{ID: s.ID, Title: s.Title, CreatedAt: s.CreatedAt}
		sessionModels := map[string]struct{}{}

		walkSession(s, &row, sessionModels, models, toolCalls)

		row.Models = sortedKeys(sessionModels)

		// Tokens consumed but nothing charged: the model is unpriced. Keyed on
		// AnySpend, not Total, so a reasoning-only run is not silently reported
		// as a genuine $0.00.
		if row.Tokens.AnySpend() && row.Cost == 0 {
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

	for _, mr := range models {
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

// walkSession accumulates one session's spend into row and the report-wide
// tallies, recursing into sub-sessions.
//
// Sub-session spend is folded into the parent's row rather than reported
// separately: a delegating run is one thing the user started, and
// [session.Session.TotalCost] counts it the same way. The store agrees — its
// session listing is root-only — so a sub-session has nowhere else to be
// reported.
func walkSession(s *session.Session, row *SessionRow, sessionModels map[string]struct{},
	models map[string]*ModelRow, toolCalls map[string]int,
) {
	if s == nil {
		return
	}

	// MessagesSnapshot copies under the session lock, so a live session being
	// written to cannot race this walk.
	for _, item := range s.MessagesSnapshot() {
		if item.IsSubSession() {
			walkSession(item.SubSession, row, sessionModels, models, toolCalls)
		}

		model, itemUsage, itemCost := itemSpend(&item)

		row.Cost += itemCost
		addUsage(&row.Tokens, itemUsage)

		if model != "" {
			sessionModels[model] = struct{}{}
			mr, ok := models[model]
			if !ok {
				mr = &ModelRow{Model: model}
				models[model] = mr
			}
			// An attributed model with no usage still happened — the provider
			// just did not report tokens (usage tracking off, or an older
			// session). Counting it keeps the model visible; Unmetered records
			// why its token columns are short.
			mr.Calls++
			if itemUsage == nil {
				mr.Unmetered++
			}
			addUsage(&mr.Tokens, itemUsage)
			mr.Cost += itemCost
		}

		if item.IsMessage() {
			for _, tc := range item.Message.Message.ToolCalls {
				if tc.Function.Name != "" {
					toolCalls[tc.Function.Name]++
				}
			}
		}
	}
}

// itemSpend returns the model, usage and cost behind one session item, whichever
// shape it takes: an assistant message, or a non-message item such as a
// compaction summary that records its own spend.
//
// An item can carry both — a message plus an item-level compaction cost — so the
// two costs are added rather than chosen between, matching
// [session.Session.TotalCost].
func itemSpend(item *session.Item) (model string, usage *chat.Usage, cost float64) {
	cost = item.Cost
	if item.IsMessage() {
		msg := &item.Message.Message
		return msg.Model, msg.Usage, cost + msg.Cost
	}
	return item.Model, item.Usage, cost
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
