package builtins

// safer_shell is the destructive-command guard for the shell toolset.
// It fires on EventPreToolUse (registered with preempt_yolo:true so
// the runtime dispatches it before Decide()/--yolo) and classifies
// each shell call against an embedded taxonomy:
//
//   1. Destructive match → Ask + Metadata{blast_radius, category, reason}
//   2. Safe match        → nil (no opinion; falls through to the
//                          regular approval pipeline — Decide() /
//                          --yolo / permissions / readonly hint)
//   3. No match          → Ask + Metadata{blast_radius=unknown}; the
//                          user opted into "safer mode", so an
//                          unrecognised command still surfaces.
//
// Compound shell (a && b, a; b, a | b) does NOT match a single
// destructive or safe pattern and falls through to branch 3 by
// design. The matcher is regex-based on a whitespace-normalised form
// of the command; the `<placeholder>` syntax in patterns matches a
// single non-whitespace token, and `...` matches any tail.

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/docker/docker-agent/pkg/hooks"
)

// SaferShell is the registered name of the builtin that classifies
// destructive shell commands and forces confirmation via SafetyCheck.
const SaferShell = "safer_shell"

// shellToolName is the tool name that this builtin acts on. The shell
// builtin's canonical name is duplicated here as a string literal so
// pkg/hooks/builtins does not depend on pkg/tools/builtin/shell. The
// name is part of the user-facing wire protocol — if it ever changes,
// the rename is caught by tests in both packages.
const shellToolName = "shell"

// Metadata keys the safer_shell builtin emits. The runtime carries
// these through hooks.Result.Metadata into the tool-call confirmation
// event so renderers can highlight destructive calls. The TUI
// confirmation dialog renders the blast_radius key as a colored badge
// (see pkg/tui/dialog/tool_confirmation.go). Treated as opaque
// strings by the runtime; documented for hook authors who want to
// match the same key names.
const (
	metaBlastRadius = "blast_radius"
	metaCategory    = "category"
	metaReason      = "reason"
)

//go:embed safety_patterns.json
var safetyPatternsJSON []byte

type safetyPattern struct {
	Pattern     string
	BlastRadius string
	Category    string
	regexp      *regexp.Regexp
}

type safePattern struct {
	Pattern  string
	Category string
	regexp   *regexp.Regexp
}

type safetyPatternEntry struct {
	Pattern     string `json:"pattern"`
	BlastRadius string `json:"blast_radius"`
	Category    string `json:"category"`
}

type safePatternEntry struct {
	Pattern  string `json:"pattern"`
	Category string `json:"category"`
}

type compiledPatterns struct {
	destructive []safetyPattern
	safe        []safePattern
}

var loadSafetyPatterns = sync.OnceValues(func() (compiledPatterns, error) {
	var root map[string]any
	if err := json.Unmarshal(safetyPatternsJSON, &root); err != nil {
		return compiledPatterns{}, fmt.Errorf("parse shell safety patterns: %w", err)
	}

	destructive, err := compileDestructive(root["destructive"])
	if err != nil {
		return compiledPatterns{}, err
	}
	safe, err := compileSafe(root["safe"])
	if err != nil {
		return compiledPatterns{}, err
	}
	return compiledPatterns{destructive: destructive, safe: safe}, nil
})

func compileDestructive(value any) ([]safetyPattern, error) {
	entries := collectDestructiveEntries(value)
	out := make([]safetyPattern, 0, len(entries))
	for _, entry := range entries {
		pattern := normalizeCommand(entry.Pattern)
		re, err := regexp.Compile(patternToRegexp(pattern))
		if err != nil {
			return nil, fmt.Errorf("compile destructive pattern %q: %w", entry.Pattern, err)
		}
		out = append(out, safetyPattern{
			Pattern:     entry.Pattern,
			BlastRadius: normalizeBlastRadius(entry.BlastRadius),
			Category:    entry.Category,
			regexp:      re,
		})
	}
	return out, nil
}

func compileSafe(value any) ([]safePattern, error) {
	entries := collectSafeEntries(value)
	out := make([]safePattern, 0, len(entries))
	for _, entry := range entries {
		pattern := normalizeCommand(entry.Pattern)
		re, err := regexp.Compile(patternToSafeRegexp(pattern))
		if err != nil {
			return nil, fmt.Errorf("compile safe pattern %q: %w", entry.Pattern, err)
		}
		out = append(out, safePattern{
			Pattern:  entry.Pattern,
			Category: entry.Category,
			regexp:   re,
		})
	}
	return out, nil
}

// saferShell is the [hooks.BuiltinFunc] registered under [SaferShell].
// See the file-level comment for the priority order.
//
// On taxonomy load failure the call is routed to user confirmation
// with blast_radius=unknown rather than allowed silently — failing
// closed is the security-meaningful choice and matches [failClosed]'s
// treatment of EventSafetyCheck.
func saferShell(_ context.Context, in *hooks.Input, _ []string) (*hooks.Output, error) {
	if in == nil || in.HookEventName != hooks.EventPreToolUse {
		return nil, nil
	}
	if in.ToolName != shellToolName {
		return nil, nil
	}

	command, _ := shellCommandArg(in.ToolInput)

	patterns, err := loadSafetyPatterns()
	if err != nil {
		return askWithMetadata("unknown", "", "Safety pattern load failed: "+err.Error()), nil
	}

	if command != "" {
		// 1) Destructive match wins.
		if match := bestDestructiveMatch(command, patterns.destructive); match != nil {
			return askWithMetadata(match.BlastRadius, match.Category,
				"Command matches destructive operation: "+match.Pattern), nil
		}
		// 2) Safe match → no opinion (falls through to regular pipeline).
		if matchesSafe(command, patterns.safe) {
			return nil, nil
		}
	}
	// 3) Unknown → ask with no classified severity.
	return askWithMetadata("unknown", "",
		"Shell command requires safer-mode confirmation."), nil
}

func shellCommandArg(input map[string]any) (string, bool) {
	if v, ok := input["cmd"].(string); ok {
		return v, true
	}
	if v, ok := input["command"].(string); ok {
		return v, true
	}
	return "", false
}

func bestDestructiveMatch(command string, patterns []safetyPattern) *safetyPattern {
	normalized := normalizeCommand(command)
	var best *safetyPattern
	bestSeverity := 0
	for i := range patterns {
		if !patterns[i].regexp.MatchString(normalized) {
			continue
		}
		severity := blastRadiusSeverity(patterns[i].BlastRadius)
		if severity <= bestSeverity {
			continue
		}
		bestSeverity = severity
		best = &patterns[i]
	}
	return best
}

func matchesSafe(command string, patterns []safePattern) bool {
	normalized := normalizeCommand(command)
	// Compound shell falls through to ask — refuse to safe-match anything
	// that smells like a pipeline or chain. The matcher does not parse
	// shell syntax, so we approximate via separator tokens.
	if containsShellSeparator(normalized) {
		return false
	}
	for i := range patterns {
		if patterns[i].regexp.MatchString(normalized) {
			return true
		}
	}
	return false
}

// containsShellSeparator returns true when the normalised command
// contains a whitespace-separated operator that chains or pipes
// multiple commands. The matcher then refuses to treat the whole
// string as safe even if one of the segments looks like a known
// safe command.
func containsShellSeparator(command string) bool {
	for _, sep := range []string{"&&", "||", "|", ";"} {
		if strings.Contains(" "+command+" ", " "+sep+" ") {
			return true
		}
	}
	return false
}

func askWithMetadata(blastRadius, category, reason string) *hooks.Output {
	meta := map[string]string{
		metaBlastRadius: blastRadius,
		metaReason:      reason,
	}
	if category != "" {
		meta[metaCategory] = category
	}
	return &hooks.Output{
		HookSpecificOutput: &hooks.HookSpecificOutput{
			HookEventName:            hooks.EventPreToolUse,
			PermissionDecision:       hooks.DecisionAsk,
			PermissionDecisionReason: reason,
			Metadata:                 meta,
		},
	}
}

// collectDestructiveEntries walks the JSON destructive section. The
// shape is map[category-name][]entry where each entry has pattern +
// blast_radius (+ optional category override).
func collectDestructiveEntries(value any) []safetyPatternEntry {
	switch v := value.(type) {
	case []any:
		var entries []safetyPatternEntry
		for _, item := range v {
			entries = append(entries, collectDestructiveEntries(item)...)
		}
		return entries
	case map[string]any:
		if pattern, ok := v["pattern"].(string); ok {
			if blastRadius, ok := v["blast_radius"].(string); ok {
				category, _ := v["category"].(string)
				return []safetyPatternEntry{{Pattern: pattern, BlastRadius: blastRadius, Category: category}}
			}
		}
		var entries []safetyPatternEntry
		for _, item := range v {
			entries = append(entries, collectDestructiveEntries(item)...)
		}
		return entries
	default:
		return nil
	}
}

// collectSafeEntries walks the JSON safe section. Shape is the same
// as destructive minus the blast_radius field — entries that look
// destructive (carry a blast_radius) are ignored here so a stray
// destructive entry in the safe section can't accidentally allow a
// dangerous command through.
func collectSafeEntries(value any) []safePatternEntry {
	switch v := value.(type) {
	case []any:
		var entries []safePatternEntry
		for _, item := range v {
			entries = append(entries, collectSafeEntries(item)...)
		}
		return entries
	case map[string]any:
		if pattern, ok := v["pattern"].(string); ok {
			if _, hasBlast := v["blast_radius"]; !hasBlast {
				category, _ := v["category"].(string)
				return []safePatternEntry{{Pattern: pattern, Category: category}}
			}
		}
		var entries []safePatternEntry
		for _, item := range v {
			entries = append(entries, collectSafeEntries(item)...)
		}
		return entries
	default:
		return nil
	}
}

// patternToRegexp converts a destructive pattern into a regex that
// matches anywhere in the normalised command. Destructive intent is
// the priority — a destructive pattern hidden inside a larger
// command (e.g. `cd /tmp && rm -rf foo`) should still match.
func patternToRegexp(pattern string) string {
	var b strings.Builder
	b.WriteString(`(?i)(?:^|.*\b)`)
	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case '<':
			if end := strings.IndexByte(pattern[i:], '>'); end >= 0 {
				b.WriteString(`\S+`)
				i += end + 1
				continue
			}
		case '.':
			if strings.HasPrefix(pattern[i:], "...") {
				b.WriteString(`.*`)
				i += len("...")
				continue
			}
		}
		b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		i++
	}
	b.WriteString(`(?:$|\b.*)`)
	return b.String()
}

// patternToSafeRegexp anchors the safe pattern to the start AND end
// of the command. Safe matching must be strict: `ls -la` should match
// the safe pattern `ls -<flags>`, but `ls -la && rm -rf /` must not
// (the compound shell check upstream already blocks this, but
// anchoring is a belt-and-braces second line of defence).
func patternToSafeRegexp(pattern string) string {
	var b strings.Builder
	b.WriteString(`(?i)^`)
	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case '<':
			if end := strings.IndexByte(pattern[i:], '>'); end >= 0 {
				b.WriteString(`\S+`)
				i += end + 1
				continue
			}
		case '.':
			if strings.HasPrefix(pattern[i:], "...") {
				b.WriteString(`.*`)
				i += len("...")
				continue
			}
		}
		b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		i++
	}
	b.WriteString(`$`)
	return b.String()
}

// normalizeBlastRadius collapses the JSON taxonomy's hyphenated
// levels onto the four canonical strings the hook schema carries.
func normalizeBlastRadius(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "LOW":
		return "low"
	case "MEDIUM", "LOW-MEDIUM":
		return "medium"
	case "HIGH", "MEDIUM-HIGH":
		return "high"
	default:
		return "unknown"
	}
}

// blastRadiusSeverity ranks the wire-format blast-radius strings so
// [bestDestructiveMatch] can pick the worst match across patterns.
// "unknown" outranks "medium" by design: when a hook can't classify
// a call but flags it for safety, that's more dangerous than a
// confidently-medium hit.
func blastRadiusSeverity(level string) int {
	switch level {
	case "high":
		return 4
	case "unknown":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func normalizeCommand(command string) string {
	return strings.Join(strings.Fields(strings.ToLower(command)), " ")
}
