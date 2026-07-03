package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/compaction"
	"github.com/docker/docker-agent/pkg/promptfiles"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tools"
)

// ContextCategory aggregates the estimated token footprint of one slice of
// the context window along with the number of items composing it (messages,
// tool definitions, or prompt files depending on the category).
type ContextCategory struct {
	Tokens int64 `json:"tokens"`
	Items  int   `json:"items"`
}

func (c *ContextCategory) add(tokens int64) {
	c.Tokens += tokens
	c.Items++
}

// ContextBreakdown describes the estimated composition of the prompt the
// runtime would send on the next model call, broken down by category. All
// token counts are estimates produced by [compaction.EstimateMessageTokens]
// (provider-reported counts where available, a chars-per-token heuristic
// otherwise); the actual provider tokenizer may count differently.
type ContextBreakdown struct {
	// SystemPrompt covers the invariant system messages: the agent
	// instruction, multi-agent/handoff prompts, and toolset instructions.
	SystemPrompt ContextCategory `json:"system_prompt"`
	// ToolDefinitions covers the JSON schemas (name, description,
	// parameters) of every tool exposed to the agent.
	ToolDefinitions ContextCategory `json:"tool_definitions"`
	// PromptFiles covers the add_prompt_files content (AGENTS.md, ...)
	// injected as transient context at every turn.
	PromptFiles ContextCategory `json:"prompt_files"`
	// Messages covers the user and assistant conversation turns.
	Messages ContextCategory `json:"messages"`
	// ToolResults covers the tool-role result messages.
	ToolResults ContextCategory `json:"tool_results"`
	// CompactionSummary covers the synthetic message that carries the
	// latest compaction summary, when the session has been compacted.
	CompactionSummary ContextCategory `json:"compaction_summary"`

	// ContextLimit is the resolved context window of the effective model,
	// or 0 when it cannot be determined (harness-backed agents, models
	// absent from the catalogue).
	ContextLimit int64 `json:"context_limit"`
	// Model is the effective model label ("provider/model", or the
	// harness label for harness-backed agents).
	Model string `json:"model"`
}

// TotalTokens returns the estimated size of the whole prompt.
func (b *ContextBreakdown) TotalTokens() int64 {
	return b.SystemPrompt.Tokens +
		b.ToolDefinitions.Tokens +
		b.PromptFiles.Tokens +
		b.Messages.Tokens +
		b.ToolResults.Tokens +
		b.CompactionSummary.Tokens
}

// ContextBreakdown computes the estimated context-window composition for
// sess, categorizing the output of [session.Session.GetMessages] and adding
// the tool definitions and prompt files that accompany every model call.
//
// Tool listing failures and unreadable prompt files degrade gracefully: the
// corresponding category is computed from whatever could be gathered and the
// failure is logged, so a broken toolset never hides the rest of the data.
func (r *LocalRuntime) ContextBreakdown(ctx context.Context, sess *session.Session) (*ContextBreakdown, error) {
	if sess == nil {
		return nil, errors.New("no active session")
	}
	a := r.resolveSessionAgent(sess)
	if a == nil {
		return nil, errors.New("no active agent")
	}

	b := &ContextBreakdown{Model: agentModelLabel(ctx, a)}
	if !a.HasHarness() {
		b.ContextLimit = r.contextLimitForAgentModel(ctx, a, r.getEffectiveModelID(ctx, a))
	}

	messages := sess.GetMessages(a)
	// Calibrate the heuristic against the provider-reported usage already
	// recorded on this conversation, mirroring what the proactive
	// compaction trigger does (see compactIfNeeded).
	estimator := compaction.NewSliceEstimator(messages)

	summaryContent := ""
	if summary := sess.LastSummary(); summary != "" {
		summaryContent = session.SummaryMessageContent(summary)
	}

	for i := range messages {
		msg := &messages[i]
		tokens := estimator.EstimateMessageTokens(msg)
		switch {
		case msg.Role == chat.MessageRoleSystem:
			b.SystemPrompt.add(tokens)
		case msg.Role == chat.MessageRoleTool:
			b.ToolResults.add(tokens)
		case summaryContent != "" && msg.Role == chat.MessageRoleUser && msg.Content == summaryContent:
			b.CompactionSummary.add(tokens)
		default:
			b.Messages.add(tokens)
		}
	}

	agentTools, err := a.Tools(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Context breakdown: failed to list tools; tool definitions omitted",
			"agent", a.Name(), "session_id", sess.ID, "error", err)
	}
	for i := range agentTools {
		b.ToolDefinitions.add(estimateToolDefinitionTokens(&agentTools[i]))
	}

	b.PromptFiles = r.promptFilesCategory(ctx, a.AddPromptFiles())

	return b, nil
}

// estimateToolDefinitionTokens estimates the prompt cost of one tool
// definition from the parts every provider serializes into the request:
// name, description, and the parameters JSON schema. The estimate runs
// through the same chars-per-token heuristic as messages; the synthetic
// message's per-message overhead stands in for the provider's per-tool
// wrapper tokens.
func estimateToolDefinitionTokens(tool *tools.Tool) int64 {
	content := tool.Name + tool.Description
	if tool.Parameters != nil {
		if params, err := json.Marshal(tool.Parameters); err == nil {
			content += string(params)
		}
	}
	return compaction.EstimateMessageTokens(&chat.Message{
		Role:    chat.MessageRoleSystem,
		Content: content,
	})
}

// promptFilesCategory estimates the transient context injected by the
// add_prompt_files builtin at every turn. It resolves each configured name
// through the same lookup the hook uses (workdir hierarchy plus home or
// staged kit, keyed off the runtime working directory the hooks executor is
// built with) and sizes the joined contents as the single system message the
// hook would produce. Items counts the files found, not the names configured.
func (r *LocalRuntime) promptFilesCategory(ctx context.Context, names []string) ContextCategory {
	var category ContextCategory
	if len(names) == 0 {
		return category
	}
	home, _ := os.UserHomeDir() // empty string disables the home-dir lookup
	var parts []string
	for _, name := range names {
		for _, path := range promptfiles.PathsFromEnv(r.workingDir, home, name) {
			content, err := os.ReadFile(path)
			if err != nil {
				slog.WarnContext(ctx, "Context breakdown: failed to read prompt file", "path", path, "error", err)
				continue
			}
			parts = append(parts, string(content))
			category.Items++
		}
	}
	if len(parts) == 0 {
		return category
	}
	category.Tokens = compaction.EstimateMessageTokens(&chat.Message{
		Role:    chat.MessageRoleSystem,
		Content: strings.Join(parts, "\n\n"),
	})
	return category
}
