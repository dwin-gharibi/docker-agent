package toolexec

import (
	"github.com/docker/docker-agent/pkg/permissions"
)

// PermissionOutcome is the resolved decision after evaluating the full
// approval pipeline.
type PermissionOutcome int

const (
	// OutcomeAllow means the tool can run without asking the user.
	OutcomeAllow PermissionOutcome = iota
	// OutcomeDeny means the tool must be rejected; the caller should
	// surface a tool-error response that mentions Source.
	OutcomeDeny
	// OutcomeAsk means the user must be asked for explicit confirmation.
	OutcomeAsk
)

// PermissionReason explains *why* a [PermissionDecision] was reached.
// Callers use it to produce accurate log messages and to know which
// auto-approval path was taken (yolo, checker rule, read-only hint, or
// default).
type PermissionReason int

const (
	// ReasonYolo: --yolo (sess.ToolsApproved) auto-approved the tool.
	ReasonYolo PermissionReason = iota
	// ReasonChecker: a configured permission checker (session-level or
	// team-level) produced a definitive Allow/Deny/ForceAsk verdict.
	// PermissionDecision.Source identifies which checker.
	ReasonChecker
	// ReasonReadOnlyHint: no checker matched and the tool's ReadOnlyHint
	// annotation auto-approved it.
	ReasonReadOnlyHint
	// ReasonDefault: nothing matched; the user must confirm.
	ReasonDefault
)

// NamedChecker pairs a [permissions.Checker] with a human-readable source
// label (e.g. "session permissions", "permissions configuration") used to
// construct denial messages and debug logs.
type NamedChecker struct {
	Checker *permissions.Checker
	Source  string
}

// PermissionDecision is the result of [Decide]: an outcome plus the
// reason and (when the reason is [ReasonChecker]) the source label of the
// checker that produced it.
type PermissionDecision struct {
	Outcome PermissionOutcome
	Reason  PermissionReason
	Source  string
}

// Decide resolves the final permission outcome for a tool call by walking
// the configured pipeline in priority order:
//
//  1. checkers (in order; typically session-level first, then team-level)
//     — the first checker that returns Allow / Deny / ForceAsk wins.
//     However, if the outcome is ForceAsk and yoloApproved is true,
//     YOLO overrides ForceAsk and auto-allows the call.
//  2. yoloApproved (--yolo) — auto-allow everything else.
//  3. readOnlyHint — auto-allow.
//  4. default — Ask.
//
// Decide is pure (no I/O, no side effects) so the entire approval matrix
// can be exhaustively unit-tested without a runtime.
func Decide(
	yoloApproved bool,
	checkers []NamedChecker,
	toolName string,
	toolArgs map[string]any,
	readOnlyHint bool,
) PermissionDecision {
	for _, pc := range checkers {
		switch pc.Checker.CheckWithArgs(toolName, toolArgs) {
		case permissions.Deny:
			return PermissionDecision{Outcome: OutcomeDeny, Reason: ReasonChecker, Source: pc.Source}
		case permissions.Allow:
			return PermissionDecision{Outcome: OutcomeAllow, Reason: ReasonChecker, Source: pc.Source}
		case permissions.ForceAsk:
			if yoloApproved {
				return PermissionDecision{Outcome: OutcomeAllow, Reason: ReasonYolo}
			}
			return PermissionDecision{Outcome: OutcomeAsk, Reason: ReasonChecker, Source: pc.Source}
		case permissions.Ask:
			// No explicit match at this level; fall through to next checker.
		}
	}

	if yoloApproved {
		return PermissionDecision{Outcome: OutcomeAllow, Reason: ReasonYolo}
	}

	if readOnlyHint {
		return PermissionDecision{Outcome: OutcomeAllow, Reason: ReasonReadOnlyHint}
	}
	return PermissionDecision{Outcome: OutcomeAsk, Reason: ReasonDefault}
}
