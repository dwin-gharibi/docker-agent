package root

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/usage"
)

func TestFilterSessions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	sessions := []*session.Session{
		{ID: "recent", CreatedAt: now.Add(-1 * time.Hour)},
		{ID: "old", CreatedAt: now.Add(-72 * time.Hour)},
		{ID: "undated"}, // zero CreatedAt
	}

	t.Run("zero since keeps everything", func(t *testing.T) {
		t.Parallel()
		got := filterSessions(sessions, 0, now)
		assert.Len(t, got, 3)
	})

	t.Run("drops sessions older than the cutoff", func(t *testing.T) {
		t.Parallel()
		got := filterSessions(sessions, 24*time.Hour, now)
		ids := make([]string, 0, len(got))
		for _, s := range got {
			ids = append(ids, s.ID)
		}
		// An unknown timestamp is not evidence of age, so "undated" is kept.
		assert.ElementsMatch(t, []string{"recent", "undated"}, ids)
	})

	t.Run("does not mutate the caller's slice", func(t *testing.T) {
		t.Parallel()
		input := []*session.Session{
			{ID: "a", CreatedAt: now.Add(-72 * time.Hour)},
			{ID: "b", CreatedAt: now},
		}
		_ = filterSessions(input, time.Hour, now)
		require.Len(t, input, 2)
		assert.Equal(t, "a", input[0].ID, "filtering must not reorder or clobber the input")
		assert.Equal(t, "b", input[1].ID)
	})
}

func TestRenderUsage_Text(t *testing.T) {
	t.Parallel()

	report := usage.Report{
		Sessions: []usage.SessionRow{{
			ID:        "abcdef0123456789",
			Title:     "fix the bug",
			CreatedAt: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
			Models:    []string{"openai/gpt-5"},
			Tokens:    usage.Tokens{Input: 1500, CachedInput: 1200, Output: 340},
			Cost:      0.25,
		}},
		Models: []usage.ModelRow{{
			Model: "openai/gpt-5", Calls: 3,
			Tokens: usage.Tokens{Input: 1500, CachedInput: 1200, Output: 340},
		}},
		Tools:  []usage.ToolRow{{Tool: "read_file", Calls: 7}},
		Tokens: usage.Tokens{Input: 1500, CachedInput: 1200, Output: 340},
		Cost:   0.25,
	}

	var buf bytes.Buffer
	require.NoError(t, renderUsage(&buf, report, false))
	out := buf.String()

	assert.Contains(t, out, "abcdef01", "session ID is shortened")
	assert.Contains(t, out, "fix the bug")
	assert.Contains(t, out, "1.5K", "token counts are abbreviated")
	assert.Contains(t, out, "$0.25")
	assert.Contains(t, out, "openai/gpt-5")
	assert.Contains(t, out, "read_file")
	assert.Contains(t, out, "1 session(s)")
	assert.NotContains(t, out, "understated", "no unpriced models, so no warning")
}

// A model with no price records $0 cost against real tokens. The report must say
// so rather than presenting an understated total as fact.
func TestRenderUsage_WarnsAboutUnpricedModels(t *testing.T) {
	t.Parallel()

	report := usage.Report{
		Sessions: []usage.SessionRow{{
			ID:             "s1",
			CreatedAt:      time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
			Tokens:         usage.Tokens{Input: 40, Output: 10},
			Cost:           0,
			CostIncomplete: true,
		}},
		Tokens:         usage.Tokens{Input: 40, Output: 10},
		UnpricedModels: []string{"test/fake-root"},
	}

	var buf bytes.Buffer
	require.NoError(t, renderUsage(&buf, report, false))
	out := buf.String()

	assert.Contains(t, out, "understated")
	assert.Contains(t, out, "test/fake-root")
	assert.Contains(t, out, "$0.0000+", "an incomplete cost is marked so it isn't read as exact")
}

func TestRenderUsage_EmptyReport(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	require.NoError(t, renderUsage(&buf, usage.Report{}, false))
	assert.Contains(t, buf.String(), "No sessions recorded.")
}

func TestRenderUsage_JSONIsMachineReadable(t *testing.T) {
	t.Parallel()

	report := usage.Report{
		Sessions: []usage.SessionRow{{ID: "s1", Tokens: usage.Tokens{Input: 10, Output: 2}, Cost: 1.5}},
		Tokens:   usage.Tokens{Input: 10, Output: 2},
		Cost:     1.5,
	}

	var buf bytes.Buffer
	require.NoError(t, renderUsage(&buf, report, true))

	var round usage.Report
	require.NoError(t, json.Unmarshal(buf.Bytes(), &round), "output must be valid JSON")
	assert.InDelta(t, 1.5, round.Cost, 1e-9)
	require.Len(t, round.Sessions, 1)
	assert.Equal(t, "s1", round.Sessions[0].ID)
	assert.Equal(t, int64(10), round.Sessions[0].Tokens.Input)
}

// `jq '.sessions[]'` on an empty report should yield nothing, not error on null.
func TestRenderUsage_JSONEmitsEmptyArraysNotNull(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	require.NoError(t, renderUsage(&buf, usage.Report{}, true))

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(buf.Bytes(), &raw))
	for _, key := range []string{"sessions", "models", "tools"} {
		assert.JSONEqf(t, "[]", string(raw[key]), "%s must be an empty array, not null", key)
	}
}

func TestFormatTokens(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{999_999, "1000.0K"},
		{1_000_000, "1.0M"},
		{2_500_000, "2.5M"},
	} {
		assert.Equalf(t, tc.want, formatTokens(tc.in), "formatTokens(%d)", tc.in)
	}
}

func TestTruncateTitle(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "short", truncateTitle("short", 10))
	assert.Equal(t, "abcd…", truncateTitle("abcdefgh", 5))
	assert.Equal(t, "one two", truncateTitle("one\ntwo", 10), "newlines must not break the table")

	// Rune-safe: must not split a multi-byte character.
	got := truncateTitle(strings.Repeat("é", 10), 5)
	assert.Equal(t, "éééé…", got)
	assert.Len(t, []rune(got), 5)
}

// A model whose provider reported no usage shows real calls against short token
// columns. Without a note that reads as "41 calls, 0 tokens, free", which is the
// same silent-understatement trap as an unpriced cost.
func TestRenderUsage_WarnsAboutUnmeteredCalls(t *testing.T) {
	t.Parallel()

	report := usage.Report{
		Sessions: []usage.SessionRow{{
			ID:        "s1",
			CreatedAt: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC),
			Tokens:    usage.Tokens{Input: 100, Output: 10},
			Cost:      0.2,
		}},
		Models: []usage.ModelRow{
			{Model: "openai/gpt-5", Calls: 3, Unmetered: 2, Tokens: usage.Tokens{Input: 100, Output: 10}},
			{Model: "anthropic/claude-opus-5", Calls: 1, Tokens: usage.Tokens{Input: 10}},
		},
		Tokens: usage.Tokens{Input: 110, Output: 10},
		Cost:   0.2,
	}

	var buf bytes.Buffer
	require.NoError(t, renderUsage(&buf, report, false))
	out := buf.String()

	assert.Contains(t, out, "Token counts are understated")
	assert.Contains(t, out, "openai/gpt-5")
	assert.NotContains(t, out, "no usage reported for some calls to anthropic/claude-opus-5",
		"a fully metered model must not be named")
}

func TestRenderUsage_NoUnmeteredNoteWhenAllCallsMetered(t *testing.T) {
	t.Parallel()

	report := usage.Report{
		Sessions: []usage.SessionRow{{ID: "s1", Tokens: usage.Tokens{Input: 10, Output: 2}, Cost: 1}},
		Models:   []usage.ModelRow{{Model: "openai/gpt-5", Calls: 1, Tokens: usage.Tokens{Input: 10, Output: 2}}},
		Tokens:   usage.Tokens{Input: 10, Output: 2},
		Cost:     1,
	}

	var buf bytes.Buffer
	require.NoError(t, renderUsage(&buf, report, false))
	assert.NotContains(t, buf.String(), "understated")
}
