package config

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/docker/docker-agent/pkg/config/latest"
)

// shellEnvVarRef matches a `${IDENT}` where IDENT looks like an environment
// variable name accepted by os.Expand (letters, digits, underscores; leading
// non-digit). Used to flag shell-style references that appear in JS-template
// fields, where they will be silently passed through as literals instead of
// expanded.
var shellEnvVarRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// jsEnvRef matches the JS-template form `${env.X}`. The leading `\$\{` is
// followed by optional whitespace and then `env.IDENT`; we deliberately do
// not anchor the closing brace so the match also fires for expressions like
// `${env.X || 'fallback'}`.
var jsEnvRef = regexp.MustCompile(`\$\{\s*env\.[A-Za-z_][A-Za-z0-9_]*`)

// warnExpansionMismatches scans a loaded config for fields whose contents use
// the wrong variable-expansion syntax for that field. Two incompatible
// syntaxes coexist today (issue #2615):
//
//   - JS template literals (`${env.X}`) for prompt/instruction/header/command
//     fields rendered through pkg/js.
//   - Shell-style (`$VAR` / `${VAR}` / `~`) accepted alongside `${env.X}` in
//     path and env-value fields.
//
// Toolset path (working_dir, path) and env-value fields are not checked here:
// they flow through pkg/path.ExpandPath / pkg/environment.Expand, both of
// which now accept ${env.X} as an alias for ${X} (#2615). ScriptShell and
// hook working_dir/env fields expand ${env.X} at execution time, so they
// are not checked either.
//
// Mixing them up currently fails silently; we emit warnings to make the
// problem visible without changing runtime behavior.
//
// The logger is injected (rather than read from slog.Default()) so tests can
// capture warnings without racing with other goroutines that share the
// global default logger.
func warnExpansionMismatches(ctx context.Context, logger *slog.Logger, cfg *latest.Config) {
	if logger == nil {
		logger = slog.Default()
	}
	for i := range cfg.Agents {
		a := &cfg.Agents[i]
		warnJSField(ctx, logger, a.Name, "description", a.Description)
		warnJSField(ctx, logger, a.Name, "welcome_message", a.WelcomeMessage)
		warnJSField(ctx, logger, a.Name, "instruction", a.Instruction)

		for name, cmd := range a.Commands {
			warnJSField(ctx, logger, a.Name, "commands."+name+".instruction", cmd.Instruction)
			warnJSField(ctx, logger, a.Name, "commands."+name+".description", cmd.Description)
		}

		for j := range a.Toolsets {
			t := &a.Toolsets[j]
			loc := agentToolsetLocation(a.Name, t, j)

			warnJSField(ctx, logger, loc, "instruction", t.Instruction)
			for k, v := range t.Headers {
				warnJSField(ctx, logger, loc, "headers."+k, v)
			}
			for k, v := range t.Remote.Headers {
				warnJSField(ctx, logger, loc, "remote.headers."+k, v)
			}

			// APIConfig fields (api toolset): endpoint and headers go through
			// the JS expander; instruction is rendered as plain text but the
			// agent still sees it, so a `${VAR}` typo there is silently broken.
			warnJSField(ctx, logger, loc, "api_config.endpoint", t.APIConfig.Endpoint)
			warnJSField(ctx, logger, loc, "api_config.instruction", t.APIConfig.Instruction)
			for k, v := range t.APIConfig.Headers {
				warnJSField(ctx, logger, loc, "api_config.headers."+k, v)
			}
		}
	}
}

func agentToolsetLocation(agentName string, t *latest.Toolset, idx int) string {
	kind := t.Type
	if kind == "" {
		kind = "?"
	}
	return "agent " + agentName + " toolset[" + strconv.Itoa(idx) + "] (" + kind + ")"
}

// warnJSField warns when a JS-template field contains a `${IDENT}` reference
// that isn't a `${env.X}` expression and no `${env.X}` appears elsewhere in
// the same value. Such references are kept literal at runtime instead of
// being expanded.
func warnJSField(ctx context.Context, logger *slog.Logger, loc, field, value string) {
	if value == "" || !strings.Contains(value, "${") {
		return
	}
	if jsEnvRef.MatchString(value) {
		// Has a real ${env.X}; assume any other ${...} is intentional JS.
		return
	}
	for _, m := range shellEnvVarRef.FindAllStringSubmatch(value, -1) {
		logger.WarnContext(ctx,
			"shell-style ${VAR} in JS-expanded field will not be substituted; use ${env.VAR}",
			"location", loc,
			"field", field,
			"variable", m[1],
			"see", "https://github.com/docker/docker-agent/issues/2615",
		)
	}
}
