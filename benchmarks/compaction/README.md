# Compaction model benchmark

Compares candidate summarization models for session compaction against *real*
sessions from a local docker-agent session database, measuring **speed** (total
latency, time-to-first-token), **cost** (from provider token pricing) and
**quality** (blind LLM-judge scoring).

The harness reuses docker-agent's actual compaction pipeline —
`session.CompactionInput`, the keep-tail / context budgets, and the canonical
compaction prompts from `pkg/compaction` — so every model summarizes exactly
what a real compaction would send it. A fixed 200k context limit is used for
all models so they all see identical input.

All models are called through OpenRouter (`OPENROUTER_API_KEY` required);
edit `specs` in `main.go` and `judgeModel` in `judge.go` to change the lineup.

## Usage

Everything runs from this directory and is resumable (one JSON file per
session; already-completed work is skipped, and new model variants added to
`specs` are appended to existing result files without re-running the rest).

```bash
cd benchmarks/compaction

# 1. Snapshot your session DB (never benchmark against the live file).
sqlite3 ~/.cagent/session.db ".backup session-copy.db"

# 2. Pick a stratified sample: short → long, chat-only → tool-heavy.
python3 select_sessions.py session-copy.db 50

# 3. Summarize every selected session with every model in `specs`.
go run . bench

# 4. Blind-judge the summaries (shuffled A/B/C labels, scores 1-10 on
#    coverage / accuracy / continuation / hallucination-freedom).
go run . judge

# 5. Aggregate into a comparison report.
python3 report.py
```

## Notes

- Reasoning effort: cagent's OpenAI client keys `reasoning_effort` off the
  model name; for provider-prefixed ids the harness injects OpenRouter's
  normalized `reasoning: {effort}` field at the HTTP layer instead (see
  `injectReasoningEffort`).
- Judge cost dominates on long sessions (the judge re-reads the full
  transcript); expect a few dollars for 50 sessions with an Opus-class judge.
- All derived data (`session-copy.db`, `selected.json`, `results/`,
  `judgments/`, logs) contains personal session content and is gitignored;
  never commit it.
