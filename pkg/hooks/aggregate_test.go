package hooks

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAggregateTracksMostRestrictiveDecision pins the new
// Result.Decision contract: when multiple pre_tool_use hooks fire on a
// single tool call, the aggregated verdict is the most-restrictive
// (Deny > Ask > Allow). The runtime's tool-approval flow consults this
// to short-circuit the user prompt for Allow and to escalate Ask, so
// the ordering must be stable.
func TestAggregateTracksMostRestrictiveDecision(t *testing.T) {
	t.Parallel()

	mk := func(d Decision, reason string) hookResult {
		return hookResult{HandlerResult: HandlerResult{Output: &Output{
			HookSpecificOutput: &HookSpecificOutput{
				HookEventName:            EventPreToolUse,
				PermissionDecision:       d,
				PermissionDecisionReason: reason,
			},
		}}}
	}

	cases := []struct {
		name        string
		results     []hookResult
		wantVerdict Decision
		wantReason  string
		wantAllowed bool
	}{
		{
			name:        "no decision: Allowed=true, Decision empty",
			results:     []hookResult{{}},
			wantVerdict: "",
			wantAllowed: true,
		},
		{
			name:        "single allow",
			results:     []hookResult{mk(DecisionAllow, "safe")},
			wantVerdict: DecisionAllow,
			wantReason:  "safe",
			wantAllowed: true,
		},
		{
			name:        "single ask escalates over no decision",
			results:     []hookResult{{}, mk(DecisionAsk, "unclear")},
			wantVerdict: DecisionAsk,
			wantReason:  "unclear",
			wantAllowed: true, // Ask doesn't flip Allowed; the runtime handles the prompt.
		},
		{
			name: "deny beats ask beats allow",
			results: []hookResult{
				mk(DecisionAllow, "looks fine"),
				mk(DecisionAsk, "second-guess"),
				mk(DecisionDeny, "destructive"),
			},
			wantVerdict: DecisionDeny,
			wantReason:  "destructive",
			wantAllowed: false,
		},
		{
			name: "first reason wins on ties",
			results: []hookResult{
				mk(DecisionAsk, "first ask"),
				mk(DecisionAsk, "second ask"),
			},
			wantVerdict: DecisionAsk,
			wantReason:  "first ask",
			wantAllowed: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			final := aggregate(tc.results, EventPreToolUse)
			assert.Equal(t, tc.wantVerdict, final.Decision)
			assert.Equal(t, tc.wantReason, final.DecisionReason)
			assert.Equal(t, tc.wantAllowed, final.Allowed)
		})
	}
}

// TestAggregateDecisionEmptyForNonPreToolUse documents that
// Result.Decision is meaningful only for pre_tool_use (both the
// default and preempt-yolo lanes). Other events (turn_start, post_tool_use, ...)
// MUST leave it empty so a runtime that consults it can't accidentally
// act on a stale verdict from an unrelated hook.
func TestAggregateDecisionEmptyForNonPreToolUse(t *testing.T) {
	t.Parallel()

	results := []hookResult{{HandlerResult: HandlerResult{Output: &Output{
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName:      EventTurnStart,
			PermissionDecision: DecisionAllow, // misconfigured but possible
		},
	}}}}

	final := aggregate(results, EventTurnStart)
	assert.Equal(t, Decision(""), final.Decision)
	assert.Empty(t, final.DecisionReason)
}

// TestAggregateMergesPermissionRequestMetadata pins the metadata
// contract for permission_request hooks: keys from every matching hook
// are merged, and on a clash the later hook in config order wins (results
// is iterated in registration order).
func TestAggregateMergesPermissionRequestMetadata(t *testing.T) {
	t.Parallel()

	mk := func(meta map[string]string) hookResult {
		return hookResult{HandlerResult: HandlerResult{Output: &Output{
			HookSpecificOutput: &HookSpecificOutput{
				HookEventName: EventPermissionRequest,
				Metadata:      meta,
			},
		}}}
	}

	results := []hookResult{
		mk(map[string]string{"a": "1", "shared": "first"}),
		mk(map[string]string{"b": "2", "shared": "second"}),
	}

	final := aggregate(results, EventPermissionRequest)
	assert.Equal(t, map[string]string{
		"a":      "1",
		"b":      "2",
		"shared": "second",
	}, final.Metadata)
}

// TestAggregateIgnoresMetadataForUnrelatedEvent documents that
// Metadata collection is scoped: only permission_request and the
// preempt-yolo lane of pre_tool_use collect it. Other events that
// happen to set the field on their HookSpecificOutput get nil.
func TestAggregateIgnoresMetadataForUnrelatedEvent(t *testing.T) {
	t.Parallel()

	results := []hookResult{{HandlerResult: HandlerResult{Output: &Output{
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName: EventTurnStart,
			Metadata:      map[string]string{"a": "1"},
		},
	}}}}

	final := aggregate(results, EventTurnStart)
	assert.Nil(t, final.Metadata)
}

// TestAggregatePreToolUsePreYolo_MergesMetadata pins the
// preempt-yolo lane's metadata collection. The default pre_tool_use
// lane (EventPreToolUse) does NOT collect Metadata; the preempt lane
// (EventPreToolUsePreYolo) does, with last-writer-wins on key clashes
// — same shape as permission_request. This is the only meaningful
// aggregator difference between the two lanes; Decision/Allowed/
// UpdatedInput semantics are shared and covered by the existing
// pre_tool_use tests.
func TestAggregatePreToolUsePreYolo_MergesMetadata(t *testing.T) {
	t.Parallel()

	mk := func(meta map[string]string) hookResult {
		return hookResult{HandlerResult: HandlerResult{Output: &Output{
			HookSpecificOutput: &HookSpecificOutput{
				HookEventName:      EventPreToolUse,
				PermissionDecision: DecisionAsk,
				Metadata:           meta,
			},
		}}}
	}

	results := []hookResult{
		mk(map[string]string{"blast_radius": "medium", "category": "fs-modify"}),
		mk(map[string]string{"blast_radius": "high", "reason": "irreversible"}),
	}

	final := aggregate(results, EventPreToolUsePreYolo)
	assert.Equal(t, map[string]string{
		"blast_radius": "high",
		"category":     "fs-modify",
		"reason":       "irreversible",
	}, final.Metadata)
}

// TestAggregatePreToolUseDefault_IgnoresMetadata documents the
// lane distinction from the other direction: aggregating EventPreToolUse
// (the default lane) does NOT collect Metadata even when hooks set it.
// Default-lane consumers (the regular pre_tool_use chain in the
// dispatcher) have no use for Metadata; collecting it would be a
// silent no-op behavior change that could surprise hook authors.
func TestAggregatePreToolUseDefault_IgnoresMetadata(t *testing.T) {
	t.Parallel()

	results := []hookResult{{HandlerResult: HandlerResult{Output: &Output{
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName:      EventPreToolUse,
			PermissionDecision: DecisionAsk,
			Metadata:           map[string]string{"blast_radius": "high"},
		},
	}}}}

	final := aggregate(results, EventPreToolUse)
	assert.Nil(t, final.Metadata,
		"default pre_tool_use lane must not collect Metadata; only the preempt_yolo lane does")
}
