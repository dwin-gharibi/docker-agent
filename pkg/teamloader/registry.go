package teamloader

import (
	"cmp"
	"context"
	"fmt"
	"maps"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/a2a"
	agenttool "github.com/docker/docker-agent/pkg/tools/builtin/agent"
	"github.com/docker/docker-agent/pkg/tools/builtin/api"
	"github.com/docker/docker-agent/pkg/tools/builtin/fetch"
	"github.com/docker/docker-agent/pkg/tools/builtin/filesystem"
	"github.com/docker/docker-agent/pkg/tools/builtin/lsp"
	"github.com/docker/docker-agent/pkg/tools/builtin/mcpcatalog"
	"github.com/docker/docker-agent/pkg/tools/builtin/memory"
	"github.com/docker/docker-agent/pkg/tools/builtin/modelpicker"
	"github.com/docker/docker-agent/pkg/tools/builtin/openapi"
	"github.com/docker/docker-agent/pkg/tools/builtin/shell"
	"github.com/docker/docker-agent/pkg/tools/builtin/tasks"
	"github.com/docker/docker-agent/pkg/tools/builtin/think"
	"github.com/docker/docker-agent/pkg/tools/builtin/todo"
	"github.com/docker/docker-agent/pkg/tools/builtin/userprompt"
	"github.com/docker/docker-agent/pkg/tools/mcp"
)

// ToolsetCreator is a function that creates a toolset based on the provided configuration.
// configName identifies the agent config file (e.g. "memory_agent" from "memory_agent.yaml").
type ToolsetCreator func(ctx context.Context, toolset latest.Toolset, parentDir string, runConfig *config.RuntimeConfig, configName string) (tools.ToolSet, error)

// extraCreators holds toolset creators contributed by packages that opt in via
// RegisterToolsetCreator. This keeps optional toolsets (e.g. "rag", which pulls
// in a cgo tree-sitter dependency) out of teamloader's static import graph:
// they are linked only when the owning package is blank-imported by the binary.
var extraCreators = map[string]ToolsetCreator{}

// RegisterToolsetCreator registers a creator for the given toolset type, to be
// included in every registry returned by NewDefaultToolsetRegistry. Packages
// call it from an init() so that a blank import is enough to make their toolset
// available; binaries that don't import the package don't pay for its
// dependencies. It panics on a duplicate registration to surface wiring bugs at
// startup. Not safe for concurrent use; call only from init().
func RegisterToolsetCreator(toolsetType string, creator ToolsetCreator) {
	if _, exists := extraCreators[toolsetType]; exists {
		panic(fmt.Sprintf("teamloader: toolset creator %q already registered", toolsetType))
	}
	extraCreators[toolsetType] = creator
}

// ToolsetRegistry manages the registration of toolset creators by type.
type ToolsetRegistry interface {
	CreateTool(ctx context.Context, toolset latest.Toolset, parentDir string, runConfig *config.RuntimeConfig, agentName string) (tools.ToolSet, error)
}

func NewDefaultToolsetRegistry() ToolsetRegistry {
	r := &toolsetRegistry{
		creators: map[string]ToolsetCreator{
			"todo": func(_ context.Context, toolset latest.Toolset, _ string, _ *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				return todo.CreateToolSet(toolset)
			},
			"tasks": func(_ context.Context, toolset latest.Toolset, parentDir string, runConfig *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				return tasks.CreateToolSet(toolset, parentDir, runConfig)
			},
			"memory": func(_ context.Context, toolset latest.Toolset, parentDir string, runConfig *config.RuntimeConfig, configName string) (tools.ToolSet, error) {
				return memory.CreateToolSet(toolset, parentDir, runConfig, configName)
			},
			"think": func(_ context.Context, _ latest.Toolset, _ string, _ *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				return think.CreateToolSet()
			},
			"shell": func(ctx context.Context, toolset latest.Toolset, _ string, runConfig *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				return shell.CreateToolSet(ctx, toolset, runConfig)
			},
			"script": func(ctx context.Context, toolset latest.Toolset, _ string, runConfig *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				return shell.CreateScriptToolSet(ctx, toolset, runConfig)
			},
			"filesystem": func(_ context.Context, toolset latest.Toolset, _ string, runConfig *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				return filesystem.CreateToolSet(toolset, runConfig)
			},
			"fetch": func(_ context.Context, toolset latest.Toolset, _ string, runConfig *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				return fetch.CreateToolSet(toolset, runConfig)
			},
			"mcp": func(ctx context.Context, toolset latest.Toolset, _ string, runConfig *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				return mcp.CreateToolSet(ctx, toolset, runConfig)
			},
			"mcp_catalog": func(_ context.Context, toolset latest.Toolset, _ string, runConfig *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				var opts []mcpcatalog.Option
				if len(toolset.AllowedServers) > 0 {
					opts = append(opts, mcpcatalog.WithAllowedServers(toolset.AllowedServers))
				}
				if len(toolset.BlockedServers) > 0 {
					opts = append(opts, mcpcatalog.WithBlockedServers(toolset.BlockedServers))
				}
				return mcpcatalog.New(opts...), nil
			},
			"api": func(_ context.Context, toolset latest.Toolset, _ string, runConfig *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				return api.CreateToolSet(toolset, runConfig)
			},
			"a2a": func(ctx context.Context, toolset latest.Toolset, _ string, runConfig *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				return a2a.CreateToolSet(ctx, toolset, runConfig)
			},
			"lsp": func(ctx context.Context, toolset latest.Toolset, _ string, runConfig *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				return lsp.CreateToolSet(ctx, toolset, runConfig)
			},
			"user_prompt": func(_ context.Context, _ latest.Toolset, _ string, _ *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				return userprompt.CreateToolSet()
			},
			"openapi": func(ctx context.Context, toolset latest.Toolset, _ string, runConfig *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				return openapi.CreateToolSet(ctx, toolset, runConfig)
			},
			"model_picker": func(_ context.Context, toolset latest.Toolset, _ string, _ *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				return modelpicker.CreateToolSet(toolset)
			},
			"background_agents": func(_ context.Context, _ latest.Toolset, _ string, _ *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
				return agenttool.CreateToolSet()
			},
		},
	}
	// Merge in creators contributed via RegisterToolsetCreator (e.g. "rag").
	maps.Copy(r.creators, extraCreators)
	return r
}

// toolsetRegistry manages the registration of toolset creators by type.
type toolsetRegistry struct {
	creators map[string]ToolsetCreator
}

// CreateTool creates a toolset using the registered creator for the given type.
//
// Every successful toolset is decorated with tools.WithName so status
// surfaces (the /tools dialog, error messages, …) always have a stable
// user-facing label. The decoration is a no-op for toolsets that
// already advertise a non-empty Name(): it only fills the gap left by
// built-in toolsets that don't take a `name:` field in YAML, replacing
// the previous fallback to fmt.Sprintf("%T", ts).
func (r *toolsetRegistry) CreateTool(ctx context.Context, toolset latest.Toolset, parentDir string, runConfig *config.RuntimeConfig, agentName string) (tools.ToolSet, error) {
	creator, ok := r.creators[toolset.Type]
	if !ok {
		return nil, fmt.Errorf("unknown toolset type: %s", toolset.Type)
	}
	ts, err := creator(ctx, toolset, parentDir, runConfig, agentName)
	if err != nil {
		return nil, err
	}
	return tools.WithName(ts, cmp.Or(toolset.Name, toolset.Type)), nil
}
