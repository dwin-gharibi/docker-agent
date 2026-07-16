package builtins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/hooks"
)

// TestSaferShell_MatchesDestructivePatterns covers the destructive
// taxonomy: each fixture must produce an Ask verdict with the
// expected blast-radius level when run under EventPreToolUse against
// a shell tool call. Metadata carries blast_radius + category + reason.
func TestSaferShell_MatchesDestructivePatterns(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		cmd         string
		wantLevel   string
		wantPattern string
	}{
		{"rm -rf path", "rm -rf /tmp/x", "high", "rm -rf"},
		{"rm -r path", "rm -r /tmp/x", "high", "rm -r"},
		{"docker volume rm", "docker volume rm cache", "high", "docker volume rm"},
		{"docker system prune all volumes", "docker system prune -af --volumes", "high", "docker system prune"},
		{"mkfs", "mkfs.ext4 /dev/sda1", "high", "mkfs"},
		{"rmdir empty", "rmdir /tmp/x", "low", "rmdir"},
		{"chmod recursive 777", "chmod -R 777 /tmp/x", "low", "chmod -R 777"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := saferShell(t.Context(), &hooks.Input{
				HookEventName: hooks.EventPreToolUse,
				ToolName:      shellToolName,
				ToolInput:     map[string]any{"cmd": tc.cmd},
			}, nil)
			require.NoError(t, err)
			require.NotNil(t, out, "destructive command %q should produce a verdict", tc.cmd)
			require.NotNil(t, out.HookSpecificOutput)
			assert.Equal(t, hooks.EventPreToolUse, out.HookSpecificOutput.HookEventName)
			assert.Equal(t, hooks.DecisionAsk, out.HookSpecificOutput.PermissionDecision)
			assert.Equal(t, tc.wantLevel, out.HookSpecificOutput.Metadata[metaBlastRadius],
				"unexpected blast radius for %q", tc.cmd)
			assert.Contains(t, out.HookSpecificOutput.PermissionDecisionReason, tc.wantPattern,
				"reason should name the matched pattern")
			assert.NotEmpty(t, out.HookSpecificOutput.Metadata[metaCategory],
				"destructive matches must carry a category tag")
		})
	}
}

// Under strict (default), safe-list matches Ask with blast_radius=safe
// so the UI can render a SAFE chip. The verdict is still Ask — safe
// metadata is decoration, not a bypass.
func TestSaferShell_SafeCommandsUnderStrictAskWithSafeRadius(t *testing.T) {
	t.Parallel()

	safeCases := []string{
		"ls",
		"ls /tmp",
		"ls -la",
		"ls -la /tmp",
		"cat README.md",
		"head -n 50 main.go",
		"tail -n 20 logs/app.log",
		"pwd",
		"whoami",
		"hostname",
		"date",
		"env",
		"printenv PATH",
		"which docker",
		"echo hello world",
		"printf %s\\n value",
		"basename /tmp/x",
		"dirname /tmp/x",
		"df -h",
		"du -sh /tmp",
		"grep -n pattern file.go",
		"rg pattern",
		"rg pattern src",
		"sort file.txt",
		"uniq file.txt",
		"wc file.txt",
		"wc -l file.txt",
		"stat go.mod",
		"file go.mod",
		"ps aux",
		"top -n 1",
		"git status",
		"git diff",
		"git diff HEAD~1",
		"git log --oneline -10",
		"git show HEAD",
		"git branch",
		"git remote -v",
		"docker ps",
		"docker ps -a",
		"docker images",
		"docker inspect web",
		"docker logs web",
		"docker logs --tail 100 web",
		"docker stats --no-stream",
		"docker version",
		"docker info",
		"docker system df",
		"kubectl get pods",
		"kubectl describe pod web",
		"kubectl logs web",
		"kubectl version",
	}
	for _, cmd := range safeCases {
		t.Run(cmd, func(t *testing.T) {
			out, err := saferShell(t.Context(), &hooks.Input{
				HookEventName: hooks.EventPreToolUse,
				ToolName:      shellToolName,
				ToolInput:     map[string]any{"cmd": cmd},
			}, nil)
			require.NoError(t, err, "safe command %q must produce a verdict", cmd)
			require.NotNil(t, out, "safe command %q must produce a verdict under strict", cmd)
			require.NotNil(t, out.HookSpecificOutput)
			assert.Equal(t, hooks.DecisionAsk, out.HookSpecificOutput.PermissionDecision,
				"strict must still ask; the safe verdict is UI decoration, not a bypass")
			assert.Equal(t, radiusSafe, out.HookSpecificOutput.Metadata[metaBlastRadius])
			assert.Contains(t, out.HookSpecificOutput.PermissionDecisionReason, "safe read-only pattern")
		})
	}
}

// TestSaferShell_CompoundShellFallsThroughToAsk pins the contract
// that a destructive command hidden inside a chain still triggers
// the prompt — directly via a destructive match on the inner
// command — and that a safe-looking compound never silently passes
// the safe allowlist (the matcher refuses safe-match on any string
// containing a shell separator).
func TestSaferShell_CompoundShellFallsThroughToAsk(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cmd  string
	}{
		{"safe-then-destructive AND", "cd /tmp && rm -rf foo"},
		{"safe-then-destructive semicolon", "cd /tmp; rm -rf foo"},
		{"safe-then-destructive pipe", "find /tmp | xargs rm -rf"},
		{"two safes chained does NOT silently match safe list", "ls && pwd"},
		{"safe OR safe still asks", "git status || git diff"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := saferShell(t.Context(), &hooks.Input{
				HookEventName: hooks.EventPreToolUse,
				ToolName:      shellToolName,
				ToolInput:     map[string]any{"cmd": tc.cmd},
			}, nil)
			require.NoError(t, err)
			require.NotNil(t, out, "compound command %q must produce a verdict", tc.cmd)
			require.NotNil(t, out.HookSpecificOutput)
			assert.Equal(t, hooks.DecisionAsk, out.HookSpecificOutput.PermissionDecision)
		})
	}
}

// TestSaferShell_AcceptsCommandAliasKey pins the "command" alias for
// the canonical "cmd" arg — the shell tool accepts both. Without
// this the alias path would silently bypass the matcher.
func TestSaferShell_AcceptsCommandAliasKey(t *testing.T) {
	t.Parallel()

	out, err := saferShell(t.Context(), &hooks.Input{
		HookEventName: hooks.EventPreToolUse,
		ToolName:      shellToolName,
		ToolInput:     map[string]any{"command": "rm -rf /tmp/x"},
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "high", out.HookSpecificOutput.Metadata[metaBlastRadius])
}

// TestSaferShell_NoOpForNonShellTool keeps the no-op contract: the
// builtin is registered under matcher "*", so it sees every
// pre_tool_use dispatch. It must return nil for tools it doesn't
// classify.
func TestSaferShell_NoOpForNonShellTool(t *testing.T) {
	t.Parallel()

	out, err := saferShell(t.Context(), &hooks.Input{
		HookEventName: hooks.EventPreToolUse,
		ToolName:      "filesystem",
		ToolInput:     map[string]any{"cmd": "rm -rf /tmp/x"},
	}, nil)
	require.NoError(t, err)
	assert.Nil(t, out)
}

// TestSaferShell_NoOpUnderWrongEvent: the builtin only acts on
// EventPreToolUse. An operator who wires it under a different event
// (e.g. post_tool_use) gets a no-op rather than a misleading verdict.
func TestSaferShell_NoOpUnderWrongEvent(t *testing.T) {
	t.Parallel()

	out, err := saferShell(t.Context(), &hooks.Input{
		HookEventName: hooks.EventPostToolUse,
		ToolName:      shellToolName,
		ToolInput:     map[string]any{"cmd": "rm -rf /tmp/x"},
	}, nil)
	require.NoError(t, err)
	assert.Nil(t, out)
}

// TestSaferShell_UnknownCommandAsksWithUnknownRadius pins the
// fail-closed default: a shell command that matches neither the
// destructive taxonomy nor the safe allowlist still asks, with
// blast_radius=unknown.
func TestSaferShell_UnknownCommandAsksWithUnknownRadius(t *testing.T) {
	t.Parallel()

	out, err := saferShell(t.Context(), &hooks.Input{
		HookEventName: hooks.EventPreToolUse,
		ToolName:      shellToolName,
		ToolInput:     map[string]any{"cmd": "myproject-cli deploy --prod"},
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, out)
	require.NotNil(t, out.HookSpecificOutput)
	assert.Equal(t, hooks.DecisionAsk, out.HookSpecificOutput.PermissionDecision)
	assert.Equal(t, "unknown", out.HookSpecificOutput.Metadata[metaBlastRadius])
}

// TestSaferShell_EmptyOrMissingCommandAsksWithUnknown: empty or
// missing cmd / command keys still produce an Ask verdict. The shell
// tool shouldn't be emitting empty cmds in practice, but if the LLM
// does, safer mode wants the user to see the prompt rather than
// rubber-stamping it.
func TestSaferShell_EmptyOrMissingCommandAsksWithUnknown(t *testing.T) {
	t.Parallel()

	cases := []map[string]any{
		nil,
		{},
		{"cmd": ""},
		{"unrelated": "rm -rf /tmp"},
	}
	for i, in := range cases {
		out, err := saferShell(t.Context(), &hooks.Input{
			HookEventName: hooks.EventPreToolUse,
			ToolName:      shellToolName,
			ToolInput:     in,
		}, nil)
		require.NoError(t, err, "case %d", i)
		require.NotNil(t, out, "case %d: %v", i, in)
		require.NotNil(t, out.HookSpecificOutput)
		assert.Equal(t, hooks.DecisionAsk, out.HookSpecificOutput.PermissionDecision, "case %d", i)
		assert.Equal(t, "unknown", out.HookSpecificOutput.Metadata[metaBlastRadius], "case %d", i)
	}
}

// TestSaferShell_NilInputIsNoOp covers the executor's defensive nil
// passthrough.
func TestSaferShell_NilInputIsNoOp(t *testing.T) {
	t.Parallel()

	out, err := saferShell(t.Context(), nil, nil)
	require.NoError(t, err)
	assert.Nil(t, out)
}

// TestSaferShell_ApplyAgentDefaultsAutoInjectsBuiltin pins the YAML
// sugar contract: setting AgentDefaults.SaferShell=true must produce
// a pre_tool_use entry that names the safer_shell builtin and flags
// PreemptYolo so the entry fires before Decide()/--yolo.
func TestSaferShell_ApplyAgentDefaultsAutoInjectsBuiltin(t *testing.T) {
	t.Parallel()

	cfg := ApplyAgentDefaults(nil, AgentDefaults{SaferShell: true})
	require.NotNil(t, cfg, "SaferShell=true must produce a non-empty config")
	require.Len(t, cfg.PreToolUse, 1, "expected exactly one pre_tool_use matcher entry")
	mc := cfg.PreToolUse[0]
	assert.Equal(t, "*", mc.Matcher,
		"wildcard matcher keeps the hook generic so other pre_tool_use hooks can coexist")
	require.NotNil(t, mc.PreemptYolo, "preempt_yolo must be set on the auto-injected entry")
	assert.True(t, *mc.PreemptYolo,
		"preempt_yolo must be true so the entry fires before Decide()/--yolo")
	require.Len(t, mc.Hooks, 1)
	assert.Equal(t, hooks.HookTypeBuiltin, mc.Hooks[0].Type)
	assert.Equal(t, SaferShell, mc.Hooks[0].Command)
}

func TestSaferShell_PolicyMatrix(t *testing.T) {
	const destructive = "rm -rf /tmp/x"
	const safe = "docker ps"
	const unknown = "myproject-cli deploy --prod"

	type want struct {
		emit        bool
		decision    hooks.Decision
		blastRadius string
	}

	cases := []struct {
		name   string
		policy string
		cmd    string
		want   want
	}{
		{"unsafe + destructive → silent", policyUnsafe, destructive, want{emit: false}},
		{"unsafe + safe → silent", policyUnsafe, safe, want{emit: false}},
		{"unsafe + unknown → silent", policyUnsafe, unknown, want{emit: false}},

		{"safer + destructive → ask (high)", policySafer, destructive, want{emit: true, decision: hooks.DecisionAsk, blastRadius: "high"}},
		{"safer + safe → silent", policySafer, safe, want{emit: false}},
		{"safer + unknown → silent", policySafer, unknown, want{emit: false}},

		{"safe-auto + destructive → ask (high)", policySafeAuto, destructive, want{emit: true, decision: hooks.DecisionAsk, blastRadius: "high"}},
		{"safe-auto + safe → allow (safe)", policySafeAuto, safe, want{emit: true, decision: hooks.DecisionAllow, blastRadius: "safe"}},
		{"safe-auto + unknown → ask (unknown)", policySafeAuto, unknown, want{emit: true, decision: hooks.DecisionAsk, blastRadius: "unknown"}},

		{"strict + destructive → ask (high)", policyStrict, destructive, want{emit: true, decision: hooks.DecisionAsk, blastRadius: "high"}},
		{"strict + safe → ask (safe)", policyStrict, safe, want{emit: true, decision: hooks.DecisionAsk, blastRadius: "safe"}},
		{"strict + unknown → ask (unknown)", policyStrict, unknown, want{emit: true, decision: hooks.DecisionAsk, blastRadius: "unknown"}},

		{"empty + destructive → ask (high)", "", destructive, want{emit: true, decision: hooks.DecisionAsk, blastRadius: "high"}},
		{"empty + safe → ask (safe)", "", safe, want{emit: true, decision: hooks.DecisionAsk, blastRadius: "safe"}},
		{"empty + unknown → ask (unknown)", "", unknown, want{emit: true, decision: hooks.DecisionAsk, blastRadius: "unknown"}},
		{"unrecognised policy → strict fallback", "future", destructive, want{emit: true, decision: hooks.DecisionAsk, blastRadius: "high"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := saferShell(t.Context(), &hooks.Input{
				HookEventName: hooks.EventPreToolUse,
				ToolName:      shellToolName,
				ToolInput:     map[string]any{"cmd": tc.cmd},
				SafetyPolicy:  tc.policy,
			}, nil)
			require.NoError(t, err)
			if !tc.want.emit {
				assert.Nil(t, out, "policy=%q cmd=%q must be silent", tc.policy, tc.cmd)
				return
			}
			require.NotNil(t, out, "policy=%q cmd=%q must produce a verdict", tc.policy, tc.cmd)
			require.NotNil(t, out.HookSpecificOutput)
			assert.Equal(t, tc.want.decision, out.HookSpecificOutput.PermissionDecision)
			assert.Equal(t, tc.want.blastRadius, out.HookSpecificOutput.Metadata[metaBlastRadius])
		})
	}
}

// args[0] pins the mode regardless of Input.SafetyPolicy;
// unrecognised args fall through to the session policy.
func TestSaferShell_ArgsOverrideSessionPolicy(t *testing.T) {
	cases := []struct {
		name          string
		sessionPolicy string
		args          []string
		cmd           string
		wantEmit      bool
		wantRadius    string
	}{
		{
			name:          "args=safer overrides unsafe session: destructive gates",
			sessionPolicy: policyUnsafe,
			args:          []string{"safer"},
			cmd:           "rm -rf /tmp/x",
			wantEmit:      true,
			wantRadius:    "high",
		},
		{
			name:          "args=safer overrides unsafe session: unknown stays silent",
			sessionPolicy: policyUnsafe,
			args:          []string{"safer"},
			cmd:           "docker build .",
			wantEmit:      false,
		},
		{
			name:          "args=safer overrides unsafe session: safe stays silent",
			sessionPolicy: policyUnsafe,
			args:          []string{"safer"},
			cmd:           "docker ps",
			wantEmit:      false,
		},
		{
			name:          "args=safe-auto overrides unsafe session: safe allow",
			sessionPolicy: policyUnsafe,
			args:          []string{"safe-auto"},
			cmd:           "docker ps",
			wantEmit:      true,
			wantRadius:    "safe",
		},
		{
			name:          "args=safe-auto overrides unsafe session: destructive ask",
			sessionPolicy: policyUnsafe,
			args:          []string{"safe-auto"},
			cmd:           "rm -rf /tmp/x",
			wantEmit:      true,
			wantRadius:    "high",
		},
		{
			name:          "args=safe-auto overrides unsafe session: unknown ask",
			sessionPolicy: policyUnsafe,
			args:          []string{"safe-auto"},
			cmd:           "myproject-cli deploy",
			wantEmit:      true,
			wantRadius:    "unknown",
		},
		{
			name:          "args=strict overrides safer session: unknown now gates",
			sessionPolicy: policySafer,
			args:          []string{"strict"},
			cmd:           "docker build .",
			wantEmit:      true,
			wantRadius:    "unknown",
		},
		{
			name:          "args=unsafe overrides strict session: silent on destructive",
			sessionPolicy: policyStrict,
			args:          []string{"unsafe"},
			cmd:           "rm -rf /tmp/x",
			wantEmit:      false,
		},
		{
			name:          "unrecognised args value falls through to session policy (unsafe → silent)",
			sessionPolicy: policyUnsafe,
			args:          []string{"yolo"}, // typo
			cmd:           "rm -rf /tmp/x",
			wantEmit:      false,
		},
		{
			name:          "unrecognised args value falls through to session policy (safer → ask)",
			sessionPolicy: policySafer,
			args:          []string{"yolo"}, // typo
			cmd:           "rm -rf /tmp/x",
			wantEmit:      true,
			wantRadius:    "high",
		},
		{
			name:          "empty args + empty session: strict fallback gates destructive",
			sessionPolicy: "",
			args:          nil,
			cmd:           "rm -rf /tmp/x",
			wantEmit:      true,
			wantRadius:    "high",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := saferShell(t.Context(), &hooks.Input{
				HookEventName: hooks.EventPreToolUse,
				ToolName:      shellToolName,
				ToolInput:     map[string]any{"cmd": tc.cmd},
				SafetyPolicy:  tc.sessionPolicy,
			}, tc.args)
			require.NoError(t, err)
			if !tc.wantEmit {
				assert.Nil(t, out)
				return
			}
			require.NotNil(t, out)
			require.NotNil(t, out.HookSpecificOutput)
			wantDecision := hooks.DecisionAsk
			if tc.wantRadius == "safe" {
				wantDecision = hooks.DecisionAllow
			}
			assert.Equal(t, wantDecision, out.HookSpecificOutput.PermissionDecision)
			assert.Equal(t, tc.wantRadius, out.HookSpecificOutput.Metadata[metaBlastRadius])
		})
	}
}

// unsafe short-circuits before the taxonomy loader is called.
func TestSaferShell_UnsafeReturnsBeforeTaxonomyLoad(t *testing.T) {
	out, err := saferShell(t.Context(), &hooks.Input{
		HookEventName: hooks.EventPreToolUse,
		ToolName:      shellToolName,
		ToolInput:     map[string]any{"cmd": "rm -rf /tmp/x"},
		SafetyPolicy:  policyUnsafe,
	}, nil)
	require.NoError(t, err)
	assert.Nil(t, out, "unsafe must short-circuit before classification")
}
