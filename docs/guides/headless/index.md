---
title: "Running Agents Headless & in CI"
description: "Run Docker Agent without a TUI: structured JSON output, event hooks, auto-approval strategies, and a GitHub Actions example."
keywords: docker agent, ai agents, guides, headless, ci, github actions
weight: 50
canonical: https://docs.docker.com/ai/docker-agent/guides/headless/
---

_Run Docker Agent without a TUI: structured JSON output, event hooks, auto-approval strategies, and a GitHub Actions example._

## `--exec` Mode Basics

`--exec` runs an agent without the interactive TUI: output goes to stdout and the process exits when the conversation is done. It's the mode to use in scripts, CI, and any context without a terminal.

```bash
# One-shot task, message as an argument
$ docker agent run --exec agent.yaml "Summarize the open issues in this repo"

# Pipe the message via stdin instead
$ echo "Summarize the open issues in this repo" | docker agent run --exec agent.yaml -

# Multiple messages are processed as a multi-turn conversation, in order
$ docker agent run --exec agent.yaml "question 1" "question 2" "question 3"
```

See [`docker agent run --exec`](../../features/cli/index.md#docker-agent-run---exec) for the full flag reference.

## Structured Output for Machines

Two independent things make an `--exec` run's output easy to parse: how the transcript is emitted, and what shape the model's own answer takes.

**`--json`** switches the transcript itself from human-readable text to newline-delimited JSON: one JSON object per runtime event (messages, tool calls, tool results, errors, …), instead of formatted text interleaved with tool-call boxes. Pipe it into `jq` or any NDJSON-aware log processor:

```bash
$ docker agent run --exec agent.yaml --json "List the 5 largest files in this repo" | jq -c 'select(.type == "agent_choice")'
```

**`structured_output`** constrains the *model's own response* to a JSON schema you define on the agent, independent of `--json`. Use it when downstream code needs the model's answer in a predictable shape (a list of findings, a classification, …) rather than free-form prose. See [Structured Output](../../configuration/structured-output/index.md) for the full field reference — combine it with `--json` in `--exec` to get both a parseable transcript and a schema-validated final answer.

## Reacting to Events

`--on-event <type>=<cmd>` runs a shell command whenever an event of the given type fires, with the event's JSON payload piped to the command's stdin. Use `*=<cmd>` to match every event type. The flag is repeatable.

> [!WARNING]
> **`--on-event` does nothing under `--exec`**
>
> Event hooks are installed on the interactive App's event bus. A `docker agent run --exec` run returns before that wiring happens, so `--on-event` is silently a no-op there — no error, no hook ever runs. Use `--on-event` with a normal interactive run or `--lean` (which still installs hooks; it just skips the alternate screen). For a headless `--exec` run, get the same effect by parsing the `--json` NDJSON stream yourself and shelling out on the events you care about — for example `stream_stopped`, which fires when a turn ends normally.

```bash
# Post a Slack notification when the agent finishes a turn (interactive or --lean only)
$ docker agent run agent.yaml --lean --on-event stream_stopped="./notify-slack.sh" "Fix the failing test"

# Log every event to a file for later inspection
$ docker agent run agent.yaml --lean --on-event "*=cat >> events.ndjson" "Fix the failing test"

# Headless equivalent: capture the --json NDJSON stream, then react to it yourself
$ docker agent run --exec agent.yaml --json "Fix the failing test" | tee events.ndjson
$ jq -e 'select(.type == "stream_stopped")' events.ndjson >/dev/null && ./notify-slack.sh
```

Hooks run asynchronously and detached from the run, and are never waited on: `main.go` calls `os.Exit` as soon as the run finishes, which terminates any hook still in flight along with the process — a hook's own failure is logged but never fails the run, and there is no guarantee a slow hook completes before the process exits. Don't rely on `--on-event` for anything that must finish before the process exits; have the hook script itself detach (e.g. `nohup`/`disown`) if it needs to outlive the run.

## Auto-Approval Strategy in CI

Interactively, the TUI prompts for confirmation before a tool call runs unless it's covered by an `allow` permission pattern. There's no one to answer that prompt in CI, so an unattended `--exec` run needs an explicit auto-approval strategy — otherwise every tool call the model attempts is rejected outright (there's no stdin to prompt, so `--exec` without one just answers "no" on your behalf; see [`--json`'s auto-reject behavior](#structured-output-for-machines) above).

You have three options, from broadest to narrowest:

- **`--yolo`** auto-approves every tool call. Simplest to set up, but it means the model can run anything its toolsets expose, unattended.
- **Permission allow-lists** (`permissions.allow` on the agent, or `settings.permissions.allow` globally) approve only specific tools or argument patterns and leave everything else to ask (which, remember, means "reject" with no one there to answer). See [Permissions](../../configuration/permissions/index.md).
- **`safe-auto` shell policy** auto-approves only commands an embedded safety classifier judges non-destructive (reads, `ls`, `git status`, …), and still asks — i.e. rejects, in `--exec` — on anything it can't classify as safe (including any compound command with `;`, `&&`, `|`, …, which always skips the safe-list). Setting `safer: true` on a shell toolset only *registers* this classifier — the policy it enforces still defaults to `strict`, which asks on safe, destructive, and unknown commands alike. To get actual auto-approval of safe commands you must also pin the policy to `safe-auto`, via a `pre_tool_use` hook entry as shown below. See the `safer_shell` built-in in the [Hooks reference](../../configuration/hooks/index.md#available-built-ins).

```yaml
# examples/ci_safe_permissions.yaml
agents:
  root:
    model: anthropic/claude-sonnet-4-5
    description: CI agent restricted to safe, read-only shell and MCP calls
    instruction: You are a helpful assistant.
    toolsets:
      - type: shell
        safer: true # registers the safer_shell classifier; the hooks entry below is what pins it to safe-auto
      - type: mcp
        ref: docker:github-official
    hooks:
      pre_tool_use:
        - matcher: "*"
          preempt_yolo: true
          hooks:
            - type: builtin
              command: safer_shell
              args: ["safe-auto"] # without this pin, safer_shell defaults to "strict" and asks for every shell call

permissions:
  allow:
    - "read_file"
    - "mcp:github:get_*"
    - "mcp:github:list_*"
  deny:
    - "shell:cmd=sudo*"
    - "shell:cmd=rm*"
```

> [!NOTE]
> **Command-string matching has limits**
>
> Both `permissions` patterns and the `safer_shell` classifier work by matching against the shell command string (or, for `permissions`, the tool's arguments) — they catch obviously destructive calls, but they are not a sandbox. A cleverly obfuscated or dynamically constructed command can still slip past pattern matching. Treat these as one layer of defense: pair them with least-privilege CI credentials and, for anything that must be a hard boundary, run the job inside `--sandbox` or an isolated `--worktree`.

> [!WARNING]
> **`--yolo` in CI runs untrusted, unattended code with no one watching**
>
> A CI job is exactly the environment where a runaway or misled agent does the most damage before anyone notices — no one is at the keyboard to catch a bad `shell` call before it runs. Prefer a permission allow-list scoped to what the job actually needs over blanket `--yolo`, especially for any agent with a `shell` or `mcp` toolset that can reach production systems, secrets, or your source repository's remote.

## Providing Secrets in CI

Never put provider API keys or MCP tokens in the agent config file. Inject them as environment variables from your CI provider's secret store, or via `--env-from-file` with a file materialized at job start. See [Managing Secrets](../secrets/index.md) for every supported method, including Docker Compose secrets and 1Password references — both of which map cleanly onto CI secret stores.

## Disabling Telemetry

Docker Agent's anonymous usage telemetry is enabled by default. In CI you may want it off:

```bash
$ TELEMETRY_ENABLED=false docker agent run --exec agent.yaml "..."
```

See [Telemetry](../../community/telemetry/index.md) for exactly what is (and isn't) collected.

## Example: GitHub Actions

A bare OCI registry reference (`agentcatalog/coder`) has no local `permissions:`/`hooks:` block you control, so a security-sensitive CI job should check in a small agent config instead — reusing the `permissions` + pinned `safer_shell` pattern from [Auto-Approval Strategy in CI](#auto-approval-strategy-in-ci) — rather than leaning on a hand-rolled shell-matching hook. `permissions` and a `preempt_yolo: true` `pre_tool_use` entry (which is how `safer_shell` is registered) are both enforced by the dispatcher itself, before the tool ever runs. A plain `pre_tool_use` hook (including one added from the CLI with `--hook-pre-tool-use`) only gets a turn when neither of those has already reached a deny/allow verdict, and is *appended* after any hooks the config already declares — it does not override them. This example runs the checked-in config non-interactively against the repository being built:

```yaml
# .github/agents/review-agent.yaml
agents:
  root:
    model: anthropic/claude-sonnet-4-5
    description: Reviews the changes in a pull request for bugs and security issues
    instruction: Review the changes in this PR for bugs and security issues.
    toolsets:
      - type: shell
        safer: true # registers the safer_shell classifier
      - type: mcp
        ref: docker:github-official
    hooks:
      pre_tool_use:
        - matcher: "*"
          preempt_yolo: true
          hooks:
            - type: builtin
              command: safer_shell
              args: ["safe-auto"] # pin safe-auto; the default "strict" policy asks on every shell call

permissions:
  allow:
    - "mcp:github:get_*"
    - "mcp:github:list_*"
  deny:
    - "shell:cmd=sudo*"
    - "shell:cmd=rm*"
```

```yaml
# .github/workflows/agent-review.yml
name: Agent code review
on:
  pull_request:

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install docker-agent
        run: |
          curl -L "https://github.com/docker/docker-agent/releases/latest/download/docker-agent-linux-amd64" -o docker-agent
          chmod +x docker-agent
          sudo mv docker-agent /usr/local/bin/

      - name: Run the review agent
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          TELEMETRY_ENABLED: "false"
        run: |
          docker-agent run --exec .github/agents/review-agent.yaml --json \
            "Review the changes in this PR for bugs and security issues" \
            | tee agent-events.ndjson

      - name: Upload transcript
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: agent-events
          path: agent-events.ndjson
```

See [Permissions](../../configuration/permissions/index.md) and the `safer_shell` entry in [Hooks](../../configuration/hooks/index.md#available-built-ins) for the full pattern and policy reference. Swap the model, toolsets, and provider secret for your own — the shape (checkout, install the binary, run `--exec` with `--json` against a checked-in config, upload the transcript) generalizes to any CI provider that can run a shell step.
