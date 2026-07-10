package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SamplingHandler is a function type that handles sampling/createMessage
// requests from an MCP server.
//
// MCP servers can use sampling to ask the host application's LLM to generate
// text on their behalf. The host is in control: it may inspect, modify, or
// decline the request, and it decides which model is used. The handler is
// expected to call the host's model with the supplied messages and return
// the model's response (or an error if the request was declined or failed).
type SamplingHandler func(ctx context.Context, req *mcp.CreateMessageParams) (*mcp.CreateMessageResult, error)

// SamplingWithToolsHandler handles sampling/createMessage requests that may
// involve tool use. The request carries a tools array and supports messages
// with multi-block content (tool_use, tool_result). The handler is expected
// to forward the tools to the host's model and return any tool_use blocks
// the model emits — the requesting MCP server executes the tools and
// continues the loop in a follow-up sampling request.
type SamplingWithToolsHandler func(ctx context.Context, req *mcp.CreateMessageWithToolsParams) (*mcp.CreateMessageWithToolsResult, error)
