package usage_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/usage"
)

func assistant(model string, u *chat.Usage, toolNames ...string) session.Item {
	msg := chat.Message{Role: chat.MessageRoleAssistant, Model: model, Usage: u}
	for _, name := range toolNames {
		msg.ToolCalls = append(msg.ToolCalls, tools.ToolCall{
			Function: tools.FunctionCall{Name: name},
		})
	}
	return session.Item{Message: &session.Message{AgentName: "root", Message: msg}}
}

func day(n int) time.Time {
	return time.Date(2026, 8, n, 12, 0, 0, 0, time.UTC)
}

func TestAggregate_Empty(t *testing.T) {
	t.Parallel()
	got := usage.Aggregate(nil)
	assert.Empty(t, got.Sessions)
	assert.Empty(t, got.Models)
	assert.Empty(t, got.Tools)
	assert.Zero(t, got.Cost)
	assert.Zero(t, got.Tokens.Total())
}

func TestAggregate_SumsTokensAndCostPerSession(t *testing.T) {
	t.Parallel()

	s := &session.Session{
		ID: "s1", Title: "fix the bug", CreatedAt: day(1), Cost: 0.25,
		Messages: []session.Item{
			assistant("anthropic/claude-opus-5", &chat.Usage{
				InputTokens: 100, OutputTokens: 20, CachedInputTokens: 80, CacheWriteTokens: 5,
			}),
			assistant("anthropic/claude-opus-5", &chat.Usage{
				InputTokens: 200, OutputTokens: 30, ReasoningTokens: 7,
			}),
		},
	}

	got := usage.Aggregate([]*session.Session{s})
	require.Len(t, got.Sessions, 1)

	row := got.Sessions[0]
	assert.Equal(t, "s1", row.ID)
	assert.Equal(t, "fix the bug", row.Title)
	assert.Equal(t, int64(300), row.Tokens.Input)
	assert.Equal(t, int64(50), row.Tokens.Output)
	assert.Equal(t, int64(80), row.Tokens.CachedInput, "cached input must be visible so caching wins can be seen")
	assert.Equal(t, int64(5), row.Tokens.CacheWrite)
	assert.Equal(t, int64(7), row.Tokens.Reasoning)
	assert.InDelta(t, 0.25, row.Cost, 1e-9)
	assert.Equal(t, []string{"anthropic/claude-opus-5"}, row.Models)

	assert.InDelta(t, 0.25, got.Cost, 1e-9, "report total is the sum of session costs")
	assert.Equal(t, int64(350), got.Tokens.Input+got.Tokens.Output)
}

func TestAggregate_AttributesTokensPerModel(t *testing.T) {
	t.Parallel()

	s := &session.Session{
		ID: "s1", CreatedAt: day(1), Cost: 1,
		Messages: []session.Item{
			assistant("anthropic/claude-opus-5", &chat.Usage{InputTokens: 100, OutputTokens: 10}),
			assistant("openai/gpt-5", &chat.Usage{InputTokens: 300, OutputTokens: 40}),
			assistant("openai/gpt-5", &chat.Usage{InputTokens: 50, OutputTokens: 5}),
		},
	}

	got := usage.Aggregate([]*session.Session{s})
	require.Len(t, got.Models, 2)

	// Sorted by total tokens, descending: gpt-5 (395) before opus-5 (110).
	assert.Equal(t, "openai/gpt-5", got.Models[0].Model)
	assert.Equal(t, 2, got.Models[0].Calls)
	assert.Equal(t, int64(350), got.Models[0].Tokens.Input)
	assert.Equal(t, "anthropic/claude-opus-5", got.Models[1].Model)
	assert.Equal(t, 1, got.Models[1].Calls)

	assert.Equal(t, []string{"anthropic/claude-opus-5", "openai/gpt-5"}, got.Sessions[0].Models,
		"a session's models are listed sorted and de-duplicated")
}

func TestAggregate_CountsToolCalls(t *testing.T) {
	t.Parallel()

	s := &session.Session{
		ID: "s1", CreatedAt: day(1),
		Messages: []session.Item{
			assistant("m", &chat.Usage{InputTokens: 1}, "read_file", "read_file", "shell"),
			assistant("m", &chat.Usage{InputTokens: 1}, "read_file"),
		},
	}

	got := usage.Aggregate([]*session.Session{s})
	require.Len(t, got.Tools, 2)
	assert.Equal(t, "read_file", got.Tools[0].Tool, "sorted by call count, descending")
	assert.Equal(t, 3, got.Tools[0].Calls)
	assert.Equal(t, "shell", got.Tools[1].Tool)
	assert.Equal(t, 1, got.Tools[1].Calls)
}

// A model missing from the pricing catalogue records $0 cost despite real token
// usage. Reporting that as "$0.00" would silently under-report spend, so it must
// be flagged instead.
func TestAggregate_FlagsUnpricedUsage(t *testing.T) {
	t.Parallel()

	priced := &session.Session{
		ID: "priced", CreatedAt: day(2), Cost: 0.5,
		Messages: []session.Item{assistant("anthropic/claude-opus-5", &chat.Usage{InputTokens: 100, OutputTokens: 10})},
	}
	unpriced := &session.Session{
		ID: "unpriced", CreatedAt: day(1), Cost: 0,
		Messages: []session.Item{assistant("test/fake-root", &chat.Usage{InputTokens: 40, OutputTokens: 10})},
	}

	got := usage.Aggregate([]*session.Session{priced, unpriced})

	assert.Equal(t, []string{"test/fake-root"}, got.UnpricedModels)

	byID := map[string]usage.SessionRow{}
	for _, r := range got.Sessions {
		byID[r.ID] = r
	}
	assert.True(t, byID["unpriced"].CostIncomplete, "a session with tokens but no cost is flagged")
	assert.False(t, byID["priced"].CostIncomplete)
}

func TestAggregate_SortsSessionsNewestFirst(t *testing.T) {
	t.Parallel()

	got := usage.Aggregate([]*session.Session{
		{ID: "old", CreatedAt: day(1)},
		{ID: "new", CreatedAt: day(3)},
		{ID: "mid", CreatedAt: day(2)},
	})

	require.Len(t, got.Sessions, 3)
	assert.Equal(t, []string{"new", "mid", "old"},
		[]string{got.Sessions[0].ID, got.Sessions[1].ID, got.Sessions[2].ID})
}

// Compaction and other non-message operations carry their own Usage and Cost on
// the item rather than on a message; they are real spend and must be counted.
func TestAggregate_IncludesNonMessageItemUsage(t *testing.T) {
	t.Parallel()

	s := &session.Session{
		ID: "s1", CreatedAt: day(1), Cost: 0.4,
		Messages: []session.Item{
			assistant("anthropic/claude-opus-5", &chat.Usage{InputTokens: 100, OutputTokens: 10}),
			{Model: "anthropic/claude-opus-5", Cost: 0.1, Usage: &chat.Usage{InputTokens: 900, OutputTokens: 50}},
		},
	}

	got := usage.Aggregate([]*session.Session{s})
	assert.Equal(t, int64(1000), got.Sessions[0].Tokens.Input, "compaction tokens count too")
	require.Len(t, got.Models, 1)
	assert.Equal(t, 2, got.Models[0].Calls)
}

func TestAggregate_ToleratesMissingUsageAndModels(t *testing.T) {
	t.Parallel()

	s := &session.Session{
		ID: "s1", CreatedAt: day(1),
		Messages: []session.Item{
			{}, // neither a message nor usage
			{Message: &session.Message{Message: chat.Message{Role: chat.MessageRoleUser}}},
			assistant("", nil), // assistant with no usage and no model
		},
	}

	got := usage.Aggregate([]*session.Session{s})
	require.Len(t, got.Sessions, 1)
	assert.Zero(t, got.Sessions[0].Tokens.Total())
	assert.Empty(t, got.Models, "a message with no model and no usage contributes no model row")
}

func TestTokens_Total(t *testing.T) {
	t.Parallel()
	// Cached input is part of input, and cache writes are billed separately;
	// Total is the headline "tokens moved" figure, so it counts input+output only.
	tk := usage.Tokens{Input: 100, CachedInput: 90, CacheWrite: 10, Output: 5, Reasoning: 3}
	assert.Equal(t, int64(105), tk.Total())
}

// An attributed model whose provider reported no usage still made a call. The
// call is counted — dropping it would hide a model served entirely by a
// usage-tracking-disabled provider — and Unmetered explains the short tokens.
func TestAggregate_UnmeteredCallsAreCountedAndFlagged(t *testing.T) {
	t.Parallel()

	s := &session.Session{
		ID: "s1", CreatedAt: day(1), Cost: 0.1,
		Messages: []session.Item{
			assistant("openai/gpt-5", &chat.Usage{InputTokens: 100, OutputTokens: 10}),
			assistant("openai/gpt-5", nil), // model attributed, no usage reported
			// A non-message item (compaction) with a model but no usage.
			{Model: "openai/gpt-5"},
		},
	}

	got := usage.Aggregate([]*session.Session{s})
	require.Len(t, got.Models, 1)

	assert.Equal(t, 3, got.Models[0].Calls, "every attributed response counts as a call")
	assert.Equal(t, 2, got.Models[0].Unmetered, "the two without usage are flagged")
	assert.Equal(t, int64(100), got.Models[0].Tokens.Input, "tokens only come from reported usage")
}

// Total() is input+output by design, so a run that burned only reasoning tokens
// reports Total()==0. Keying the unpriced check on Total would silently report
// such a session as a genuine $0.00.
func TestAggregate_ReasoningOnlySpendIsFlaggedAsUnpriced(t *testing.T) {
	t.Parallel()

	s := &session.Session{
		ID: "s1", CreatedAt: day(1), Cost: 0,
		Messages: []session.Item{
			assistant("test/fake-root", &chat.Usage{ReasoningTokens: 500}),
		},
	}

	got := usage.Aggregate([]*session.Session{s})
	require.Len(t, got.Sessions, 1)

	require.Zero(t, got.Sessions[0].Tokens.Total(), "Total deliberately excludes reasoning")
	assert.True(t, got.Sessions[0].Tokens.AnySpend(), "but something was consumed")
	assert.True(t, got.Sessions[0].CostIncomplete, "so the zero cost must be flagged")
	assert.Equal(t, []string{"test/fake-root"}, got.UnpricedModels)
}

func TestTokens_AnySpend(t *testing.T) {
	t.Parallel()

	assert.False(t, usage.Tokens{}.AnySpend())
	for name, tk := range map[string]usage.Tokens{
		"input":       {Input: 1},
		"output":      {Output: 1},
		"cached":      {CachedInput: 1},
		"cache write": {CacheWrite: 1},
		"reasoning":   {Reasoning: 1},
	} {
		assert.Truef(t, tk.AnySpend(), "%s alone counts as spend", name)
	}
}
