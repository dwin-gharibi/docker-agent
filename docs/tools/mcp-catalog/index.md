---
title: "MCP Catalog Tool"
description: "Let the agent discover and activate remote MCP servers from the Docker MCP Catalog on demand."
permalink: /tools/mcp-catalog/
---

# MCP Catalog Tool

_Let the agent discover and activate remote MCP servers from the Docker MCP Catalog on demand._

## Overview

The `mcp_catalog` toolset gives an agent access to a curated subset of the [Docker MCP Catalog](https://hub.docker.com/search?q=&type=mcp) — every server in this subset is reachable over the **streamable-http** transport, so docker-agent can talk to it directly without the MCP gateway or a local subprocess.

Servers are **not** active by default. Instead, the toolset exposes a small set of meta-tools the agent uses to search, enable, and disable servers as a turn unfolds. Tools from un-enabled servers stay hidden, so the prompt is not flooded with hundreds of tool definitions the agent will never use.

<div class="callout callout-info" markdown="1">
<div class="callout-title">When to use it
</div>
  <p>Use <code>mcp_catalog</code> when you want the agent to <em>decide at runtime</em> which third-party services it needs (Notion, Stripe, Brave Search, …) instead of pinning that decision in YAML up front. For a fixed set of servers, declare each one with <a href="{{ '/configuration/tools/#mcp-tools' | relative_url }}"><code>type: mcp</code></a> directly — the catalog adds an extra layer of meta-tools that pure <code>type: mcp</code> entries do not need.</p>
</div>

## Configuration

```yaml
toolsets:
  - type: mcp_catalog
```

The toolset takes no options: the catalog is embedded in the docker-agent binary and refreshed with each release.

## Meta-Tools

Up to five tools are exposed to the model. The disable / reset-auth pair only appears once at least one server is enabled, so the meta-tool surface stays minimal until the agent activates something.

| Tool                            | When visible            | Description                                                                                                                                          |
| ------------------------------- | ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `search_remote_mcp_servers`     | Always                  | Case-insensitive fuzzy search over id, title, description, category and tags. Returns id, auth requirements (`oauth` / `api_key` / `none`) and URL. |
| `enable_remote_mcp_server`      | Always                  | Activate a server by id. The actual MCP handshake (and any OAuth flow) is deferred until the host next enumerates tools.                            |
| `list_remote_mcp_servers`       | Always                  | Show currently enabled servers and their connection state.                                                                                           |
| `disable_remote_mcp_server`     | After first enable      | Stop a server and remove its tools from the active set.                                                                                              |
| `reset_remote_mcp_server_auth`  | After first enable      | Drop persisted OAuth credentials so the next enable triggers a fresh authorization flow. No-op for `api_key` / `none` servers.                       |

### Workflow

1. The agent calls `search_remote_mcp_servers` with a keyword matching the user's intent (`"notion"`, `"stripe"`, `"docs"`, `"browser"`, …).
2. It picks a matching server id and calls `enable_remote_mcp_server`. The server's tools become available on the **next turn**.
3. It uses the newly activated tools as it would any other.
4. When done, it calls `disable_remote_mcp_server` to remove the server from the active set.

## Authentication

The catalog distinguishes three auth flavours:

- **`oauth`** — On the first turn that enumerates the server's tools, an authorization URL is surfaced through the elicitation pipeline (the same one used by YAML-declared remote MCP toolsets). Once the user authorizes, tokens are persisted in the OS keyring and re-used on subsequent runs. Use `reset_remote_mcp_server_auth` to wipe them.
- **`api_key`** — The server expects one or more env vars to be set in the agent's environment (e.g. `APIFY_API_KEY`, `BRAVE_API_KEY`). `enable_remote_mcp_server` warns if any required variable is missing — set it, then re-enable the server.
- **`none`** — No authentication. The server is reachable as soon as it is enabled.

Search results only carry the auth flavour (`oauth` / `api_key` / `none`); the specific env-var names are surfaced by `enable_remote_mcp_server` when it detects an unset variable, so you may need to enable an `api_key` server once just to learn which variable to set.

## Example

```yaml
agents:
  root:
    model: anthropic/claude-sonnet-4-5
    description: Agent that can on-demand connect to remote MCP servers from the Docker MCP Catalog.
    instruction: |
      You can discover and activate remote MCP servers on demand.
      Use search_remote_mcp_servers to find a server matching the
      user's intent, then enable_remote_mcp_server to activate it.
      Be conservative: enable only the servers you actually need for
      the task at hand. Disable a server with disable_remote_mcp_server
      once you are done with it.
    toolsets:
      - type: mcp_catalog
```

A complete, runnable configuration lives in [`examples/mcp_catalog.yaml`](https://github.com/docker/docker-agent/blob/main/examples/mcp_catalog.yaml).

## Notes and Limitations

- **Streamable-http only.** The catalog deliberately excludes servers that require a local subprocess or the MCP gateway — declare those with [`type: mcp`]({{ '/configuration/tools/#mcp-tools' | relative_url }}) instead.
- **Lazy connect.** DNS, TCP, MCP handshake and OAuth flow happen the first time the host enumerates tools for a freshly enabled server. On startup the runtime probes tools non-interactively, so OAuth-pending servers fail fast and are silently deferred to the next interactive turn.
- **No prompt discovery.** MCP prompt lookups (`/prompts`) walk YAML-declared `mcp` toolsets directly; prompts exposed by servers activated through the catalog are not surfaced. Tools — the primary interface — work fine.
- **Frozen at build time.** The list of servers is embedded in the binary. New entries land with each docker-agent release.

<div class="callout callout-tip" markdown="1">
<div class="callout-title">Pair with permissions
</div>
  <p>Because the agent decides which third-party services to talk to, this toolset works best with explicit <a href="{{ '/configuration/permissions/' | relative_url }}">permissions</a> on the surrounding tools (filesystem writes, shell commands) so a misrouted server cannot exfiltrate data unnoticed.</p>
</div>
