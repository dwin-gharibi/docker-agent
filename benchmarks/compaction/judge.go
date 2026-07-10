// Command judge scores compaction summaries produced by bench-compaction.
// For each session it renders the transcript the summarizers saw, appends the
// three candidate summaries under shuffled blind labels, and asks a judge
// model (claude opus) to score each on coverage, accuracy, continuation
// usefulness and hallucination-freedom.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/model/provider"
	"github.com/docker/docker-agent/pkg/model/provider/options"
	"github.com/docker/docker-agent/pkg/model/provider/providers"
	"github.com/docker/docker-agent/pkg/session"
)

const judgeModel = "anthropic/claude-opus-4.8"

const (
	judgeInPerTok  = 0.000005
	judgeOutPerTok = 0.000025
)

const systemPrompt = `You are an expert evaluator of conversation summaries used for LLM context compaction.
A summary replaces the full conversation history; the agent continues working from the summary alone,
so it must capture: what was done, key decisions, files/artifacts touched, current state, and next steps.

Score each candidate summary on four dimensions, each 1-10:
- coverage: are all important facts, decisions and outcomes captured?
- accuracy: are the captured facts correct w.r.t. the transcript (10 = no errors)?
- continuation: how well could an agent seamlessly continue the work from this summary alone?
- no_hallucination: 10 = nothing invented; deduct for fabricated facts, files, or events.

Also pick the single best candidate ("best": the label).
Be strict and discriminating; use the full scale. Respond ONLY with JSON, using EXACTLY these keys
("coverage", "accuracy", "continuation", "no_hallucination", "comment") for every candidate:
{"scores":{"<label>":{"coverage":N,"accuracy":N,"continuation":N,"no_hallucination":N,"comment":"one line"}},"best":"<label>"}`

type judgeScores struct {
	Coverage        float64 `json:"coverage"`
	Accuracy        float64 `json:"accuracy"`
	Continuation    float64 `json:"continuation"`
	NoHallucination float64 `json:"no_hallucination"`
	Comment         string  `json:"comment"`
}

// parseScores decodes one candidate's scores, tolerating judge-invented key
// variants for the hallucination dimension.
func parseScores(raw json.RawMessage) (judgeScores, error) {
	var s judgeScores
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, err
	}
	if s.NoHallucination == 0 {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err == nil {
			for k, v := range m {
				if f, ok := v.(float64); ok && strings.Contains(strings.ToLower(k), "halluc") {
					s.NoHallucination = f
					break
				}
			}
		}
	}
	return s, nil
}

type judgment struct {
	SessionID string                 `json:"session_id"`
	Labels    map[string]string      `json:"labels"` // label -> model name
	Scores    map[string]judgeScores `json:"scores"` // model name -> scores
	Best      string                 `json:"best"`   // model name
	CostUSD   float64                `json:"cost_usd"`
	Raw       string                 `json:"raw,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

func runJudge(ctx context.Context) error {
	store, err := session.NewSQLiteSessionStore(ctx, "session-copy.db")
	if err != nil {
		return err
	}
	defer store.Close()

	env := environment.NewDefaultProvider()
	cfg := &latest.ModelConfig{
		Provider:            "openrouter",
		Model:               judgeModel,
		BypassModelsGateway: true,
		TrackUsage:          boolPtr(true),
		ProviderOpts:        map[string]any{"api_type": "openai_chatcompletions"},
	}
	judge, err := providers.NewDefaultRegistry().New(ctx, cfg, env, options.WithMaxTokens(4000))
	if err != nil {
		return err
	}

	files, err := filepath.Glob("results/*.json")
	if err != nil {
		return err
	}
	if err := os.MkdirAll("judgments", 0o755); err != nil {
		return err
	}

	for i, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".json")
		outPath := filepath.Join("judgments", id+".json")
		if _, err := os.Stat(outPath); err == nil {
			fmt.Printf("[%d/%d] %s already judged\n", i+1, len(files), id)
			continue
		}

		raw, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		var res sessionResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return err
		}
		ok := 0
		for _, r := range res.Runs {
			if r.Error == "" && r.Summary != "" {
				ok++
			}
		}
		if ok < 2 {
			fmt.Printf("[%d/%d] %s: <2 valid summaries, skipping\n", i+1, len(files), id)
			continue
		}

		sess, err := store.GetSession(ctx, res.SessionID)
		if err != nil {
			return err
		}
		transcript := renderTranscript(sess)

		j := judgeOne(ctx, judge, res, transcript)
		data, _ := json.MarshalIndent(j, "", " ")
		if err := os.WriteFile(outPath, data, 0o644); err != nil {
			return err
		}
		status := "ok best=" + j.Best
		if j.Error != "" {
			status = "ERROR: " + j.Error
		}
		fmt.Printf("[%d/%d] %s: %s ($%.4f)\n", i+1, len(files), id, status, j.CostUSD)
	}
	return nil
}

func judgeOne(ctx context.Context, judge provider.Provider, res sessionResult, transcript string) judgment {
	j := judgment{SessionID: res.SessionID, Labels: map[string]string{}, Scores: map[string]judgeScores{}}

	// Deterministic per-session shuffle to neutralize position bias.
	h := fnv.New64a()
	h.Write([]byte(res.SessionID))
	rng := rand.New(rand.NewSource(int64(h.Sum64())))
	order := rng.Perm(len(res.Runs))

	var sb strings.Builder
	sb.WriteString("# Original conversation transcript\n\n")
	sb.WriteString(transcript)
	sb.WriteString("\n\n# Candidate summaries\n")
	labels := []string{"A", "B", "C"}
	li := 0
	for _, idx := range order {
		r := res.Runs[idx]
		if r.Error != "" || r.Summary == "" {
			continue
		}
		label := labels[li]
		li++
		j.Labels[label] = r.Model
		fmt.Fprintf(&sb, "\n## Candidate %s\n\n%s\n", label, r.Summary)
	}
	sb.WriteString("\nScore the candidates. JSON only.")

	messages := []chat.Message{
		{Role: chat.MessageRoleSystem, Content: systemPrompt, CreatedAt: time.Now().Format(time.RFC3339)},
		{Role: chat.MessageRoleUser, Content: sb.String(), CreatedAt: time.Now().Format(time.RFC3339)},
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	stream, err := judge.CreateChatCompletionStream(ctx, messages, nil)
	if err != nil {
		j.Error = err.Error()
		return j
	}
	defer stream.Close()

	var out strings.Builder
	var inTok, outTok int64
	for {
		chunk, err := stream.Recv()
		if err != nil {
			if !strings.Contains(err.Error(), "EOF") {
				j.Error = err.Error()
			}
			break
		}
		if u := chunk.Usage; u != nil {
			inTok = u.InputTokens + u.CachedInputTokens + u.CacheWriteTokens
			outTok = u.OutputTokens
		}
		for _, c := range chunk.Choices {
			out.WriteString(c.Delta.Content)
		}
	}
	j.CostUSD = float64(inTok)*judgeInPerTok + float64(outTok)*judgeOutPerTok
	j.Raw = out.String()
	if j.Error != "" {
		return j
	}

	var verdict struct {
		Scores map[string]json.RawMessage `json:"scores"`
		Best   string                     `json:"best"`
	}
	if err := json.Unmarshal(extractJSON(out.String()), &verdict); err != nil {
		j.Error = fmt.Sprintf("parse verdict: %v: %s", err, truncate(out.String(), 300))
		return j
	}
	for label, raw := range verdict.Scores {
		model, ok := j.Labels[label]
		if !ok {
			continue
		}
		scores, err := parseScores(raw)
		if err != nil {
			j.Error = fmt.Sprintf("parse scores for %s: %v", label, err)
			return j
		}
		j.Scores[model] = scores
	}
	j.Best = j.Labels[strings.TrimSpace(verdict.Best)]
	return j
}

// renderTranscript renders the same compaction input the summarizers saw as
// readable text for the judge.
func renderTranscript(sess *session.Session) string {
	messages := buildCompactionMessages(sess)
	var sb strings.Builder
	for _, m := range messages[1 : len(messages)-1] { // strip compaction prompts
		switch m.Role {
		case chat.MessageRoleUser:
			fmt.Fprintf(&sb, "\n[USER]\n%s\n", m.Content)
		case chat.MessageRoleAssistant:
			if m.Content != "" {
				fmt.Fprintf(&sb, "\n[ASSISTANT]\n%s\n", m.Content)
			}
			for _, tc := range m.ToolCalls {
				fmt.Fprintf(&sb, "\n[TOOL CALL] %s(%s)\n", tc.Function.Name, truncate(tc.Function.Arguments, 500))
			}
		case chat.MessageRoleTool:
			fmt.Fprintf(&sb, "\n[TOOL RESULT]\n%s\n", truncate(m.Content, 2000))
		}
	}
	return sb.String()
}

var jsonRe = regexp.MustCompile(`(?s)\{.*\}`)

func extractJSON(s string) []byte {
	if m := jsonRe.FindString(s); m != "" {
		return []byte(m)
	}
	return []byte(s)
}
