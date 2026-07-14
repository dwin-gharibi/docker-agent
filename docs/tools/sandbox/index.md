---
title: "Sandbox Tool"
description: "Run code snippets in an ephemeral, isolated container."
keywords: docker agent, ai agents, tools, toolsets, sandbox tool, code interpreter
linkTitle: "Sandbox"
weight: 125
canonical: https://docs.docker.com/ai/docker-agent/tools/sandbox/
---

_Run code snippets in an ephemeral, isolated container._

## Overview

The sandbox toolset lets an agent execute a code snippet (`python`, `node`, or `bash`) inside a throwaway, isolated container and get back stdout, stderr, and the exit code. Every run is disposable (`--rm`), resource-limited, and **network-disabled by default**, so the agent can run computed or model-generated code without touching the host.

Execution goes through a docker-compatible container runtime — **docker**, **podman**, or **nerdctl** — whichever is available.

> [!NOTE]
> Unlike the [`shell`](../shell/index.md) tool, which runs on the host with full access, the sandbox runs code in a disposable container isolated from the host filesystem and (by default) the network.

## Configuration

```yaml
toolsets:
  - type: sandbox
```

No configuration options. Requires a container runtime (docker, podman, or nerdctl) on the host; `run_code` returns a clear error if none is found.

## Tools

### `run_code`

| Parameter | Required | Description |
| --- | --- | --- |
| `language` | Yes | `python`, `node`, or `bash` (aliases: `py`, `js`, `sh`, …). |
| `code` | Yes | The source code to execute (piped to the interpreter's stdin). |
| `timeout_seconds` | No | Maximum run time (default 30, capped at 300). |
| `network` | No | Allow network access (default `false` = fully isolated). |

Returns the exit code plus stdout and stderr.

## How it runs

Each call runs, for example:

```text
docker run --rm -i --memory 512m --cpus 1 --pids-limit 256 --network none python:3.12-slim python3 -
```

with the code piped to the container's stdin. Languages map to pinned images (`python:3.12-slim`, `node:22-alpine`, `bash:5`).

## Example

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    description: A data assistant
    instruction: |
      When you need to compute something, use run_code with python.
    toolsets:
      - type: sandbox
```

Example result:

```text
run_code(language="python", code="print(2 + 2)")

Exit code: 0

--- stdout ---
4
```

> [!TIP]
> **When to use**
>
> Use the sandbox to run untrusted or model-generated code, compute a result, or reproduce a snippet in a clean environment. Set `network: true` only when the code genuinely needs it (for example, to install a package).
