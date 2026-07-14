package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/tools"
)

const (
	ToolNameRunCode = "run_code"

	category = "sandbox"

	defaultMemory    = "512m"
	defaultCPUs      = "1"
	defaultPidsLimit = "256"

	defaultTimeout = 30 * time.Second
	maxTimeout     = 300 * time.Second
)

var supportedRuntimes = []string{"docker", "podman", "nerdctl"}

type RunSpec struct {
	Language string
	Code     string
	Timeout  time.Duration
	Network  bool
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Executor interface {
	Run(ctx context.Context, spec RunSpec) (Result, error)
}

type langSpec struct {
	image string
	argv  []string
}

var languages = map[string]langSpec{
	"python": {"python:3.12-slim", []string{"python3", "-"}},
	"node":   {"node:22-alpine", []string{"node"}},
	"bash":   {"bash:5", []string{"bash"}},
}

func resolveLang(l string) (langSpec, string, bool) {
	name := strings.ToLower(strings.TrimSpace(l))
	switch name {
	case "py", "python", "python3":
		name = "python"
	case "js", "javascript", "node":
		name = "node"
	case "sh", "bash", "shell":
		name = "bash"
	}
	ls, ok := languages[name]
	return ls, name, ok
}

func buildRunArgs(spec RunSpec) ([]string, error) {
	ls, _, ok := resolveLang(spec.Language)
	if !ok {
		return nil, fmt.Errorf("unsupported language %q (supported: bash, node, python)", spec.Language)
	}
	args := []string{
		"run", "--rm", "-i",
		"--memory", defaultMemory,
		"--cpus", defaultCPUs,
		"--pids-limit", defaultPidsLimit,
	}
	if !spec.Network {
		args = append(args, "--network", "none")
	}
	args = append(args, ls.image)
	args = append(args, ls.argv...)
	return args, nil
}

type cliExecutor struct {
	program string
}

func (c cliExecutor) Run(ctx context.Context, spec RunSpec) (Result, error) {
	if c.program == "" {
		return Result{}, fmt.Errorf("no container runtime found (looked for %s); install Docker or a compatible runtime",
			strings.Join(supportedRuntimes, ", "))
	}
	args, err := buildRunArgs(spec)
	if err != nil {
		return Result{}, err
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.program, args...)
	cmd.Stdin = strings.NewReader(spec.Code)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	if runErr != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return res, fmt.Errorf("execution timed out after %s", timeout)
		}
		return res, fmt.Errorf("%s run: %w", c.program, runErr)
	}
	return res, nil
}

func detectRuntime() string {
	for _, p := range supportedRuntimes {
		if _, err := exec.LookPath(p); err == nil {
			return p
		}
	}
	return ""
}

type ToolSet struct {
	exec           Executor
	defaultTimeout time.Duration
	maxTimeout     time.Duration
}

var (
	_ tools.ToolSet      = (*ToolSet)(nil)
	_ tools.Instructable = (*ToolSet)(nil)
)

func New() *ToolSet {
	return &ToolSet{
		exec:           cliExecutor{program: detectRuntime()},
		defaultTimeout: defaultTimeout,
		maxTimeout:     maxTimeout,
	}
}

func CreateToolSet(_ *config.RuntimeConfig) (tools.ToolSet, error) {
	return New(), nil
}

type RunCodeArgs struct {
	Language       string `json:"language" jsonschema:"Language to run: python, node, or bash"`
	Code           string `json:"code" jsonschema:"The source code to execute"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Maximum seconds to run (default 30, capped at 300)"`
	Network        bool   `json:"network,omitempty" jsonschema:"Allow network access (default false = fully isolated)"`
}

func (t *ToolSet) runCode(ctx context.Context, args RunCodeArgs) (*tools.ToolCallResult, error) {
	if strings.TrimSpace(args.Code) == "" {
		return tools.ResultError("Error: code is required."), nil
	}
	if _, _, ok := resolveLang(args.Language); !ok {
		return tools.ResultError(fmt.Sprintf("Error: unsupported language %q (supported: bash, node, python).", args.Language)), nil
	}

	timeout := t.defaultTimeout
	if args.TimeoutSeconds > 0 {
		timeout = time.Duration(args.TimeoutSeconds) * time.Second
	}
	if timeout > t.maxTimeout {
		timeout = t.maxTimeout
	}

	res, err := t.exec.Run(ctx, RunSpec{
		Language: args.Language,
		Code:     args.Code,
		Timeout:  timeout,
		Network:  args.Network,
	})
	if err != nil {
		return tools.ResultError("Error: " + err.Error()), nil
	}
	return tools.ResultSuccess(formatResult(res)), nil
}

func formatResult(res Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Exit code: %d\n", res.ExitCode)
	if res.Stdout != "" {
		fmt.Fprintf(&b, "\n--- stdout ---\n%s", ensureTrailingNewline(res.Stdout))
	}
	if res.Stderr != "" {
		fmt.Fprintf(&b, "\n--- stderr ---\n%s", ensureTrailingNewline(res.Stderr))
	}
	if res.Stdout == "" && res.Stderr == "" {
		b.WriteString("\n(no output)\n")
	}
	return b.String()
}

func ensureTrailingNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func (t *ToolSet) Tools(context.Context) ([]tools.Tool, error) {
	return []tools.Tool{
		{
			Name:                    ToolNameRunCode,
			Category:                category,
			Description:             "Run a code snippet (python, node, or bash) in an ephemeral, isolated container and return stdout, stderr, and exit code. Network is disabled by default; set network=true only when required.",
			Parameters:              tools.MustSchemaFor[RunCodeArgs](),
			OutputSchema:            tools.MustSchemaFor[string](),
			Handler:                 tools.NewHandler(t.runCode),
			Annotations:             tools.ToolAnnotations{Title: "Run Code (Sandbox)"},
			AddDescriptionParameter: true,
		},
	}, nil
}

func (t *ToolSet) Instructions() string {
	return `## Sandbox Tool

Run a code snippet in an ephemeral, isolated container:

- run_code(language, code, timeout_seconds?, network?) executes python, node, or
  bash and returns stdout, stderr, and the exit code.
- Every run is disposable (removed on exit), resource-limited, and
  network-disabled by default. Set network=true only when needed (e.g. to install
  a package).

Requires a docker-compatible container runtime (docker, podman, or nerdctl).`
}
