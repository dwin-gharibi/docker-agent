---
title: "Webhook Tool"
description: "Send outbound notifications to Slack, Discord, Telegram, IFTTT, and more."
keywords: docker agent, ai agents, tools, toolsets, webhook, slack, discord, telegram, ifttt
linkTitle: "Webhook"
weight: 145
canonical: https://docs.docker.com/ai/docker-agent/tools/webhook/
---

_Send outbound notifications to Slack, Discord, Telegram, IFTTT, and more._

## Overview

The webhook toolset lets an agent POST a message to a webhook, shaping the JSON
payload for the target service. Delivery is one-way — the tool reports the HTTP
status, not a response body. It uses the SSRF-safe HTTP client (requests to
non-public addresses are refused), and pairs naturally with the
[`scheduler`](../scheduler/index.md) tool for alerting.

## Configuration

```yaml
toolsets:
  - type: webhook
```

No configuration options.

## `send_webhook`

| Parameter | Required | Description |
| --- | --- | --- |
| `url` | Yes | The webhook URL to POST to. |
| `message` | Yes | The message text. |
| `provider` | No | Payload format (default `generic`). |
| `value2`, `value3` | No | IFTTT extra fields (`provider=ifttt`). |
| `chat_id` | No | Telegram chat ID (`provider=telegram`). |

## Providers

| Provider | Payload |
| --- | --- |
| `slack`, `mattermost`, `rocketchat`, `googlechat`, `teams`, `generic` | `{"text": message}` |
| `discord` | `{"content": message}` |
| `ifttt` | `{"value1": message, "value2": …, "value3": …}` |
| `telegram` | `{"chat_id": …, "text": message}` |

## Example

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    instruction: If a scheduled check fails, notify the team via send_webhook.
    toolsets:
      - type: webhook
      - type: scheduler
```

> [!TIP]
> Combine with the `scheduler`: "every 15 minutes, check the build; if it
> broke, `send_webhook` to Slack."