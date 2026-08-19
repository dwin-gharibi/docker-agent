package root

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	pathx "github.com/docker/docker-agent/pkg/path"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/session/sqlitestore"
	"github.com/docker/docker-agent/pkg/usage"
)

type usageFlags struct {
	sessionDB string
	sessionID string
	since     time.Duration
	asJSON    bool
}

func newUsageCmd() *cobra.Command {
	var flags usageFlags

	cmd := &cobra.Command{
		Use:     "usage",
		Short:   "Report token and cost usage from recorded sessions",
		GroupID: "advanced",
		Args:    cobra.NoArgs,
		Long: `Report what recorded sessions spent, broken down by session, model, and tool.

Cost and token figures are summed from what the runtime recorded per message and
per non-message item, recursing into delegated sub-agents — the same walk
session.TotalCost() performs, so a session's total here matches the TUI's cost
dialog. Nothing is re-priced. Summing per-message usage is also where the
cached-input breakdown lives, so prompt-caching wins are visible.

A session that moved tokens but recorded no cost means the model was missing from
the pricing catalogue. Those are flagged rather than reported as $0.00, because a
report that quietly understates spend is worse than no report.

--session accepts a full session ID, any unambiguous ID prefix (including the
shortened form this report prints), or a relative reference such as -1 for the
most recent run.`,
		Example: `  docker agent usage
  docker agent usage --since 24h
  docker agent usage --session a1b2c3d4
  docker agent usage --session -1
  docker agent usage --json | jq '.cost'`,
		RunE: flags.run,
	}

	cmd.Flags().StringVarP(&flags.sessionDB, "session-db", "s", "", "Path to the session database (default: <data-dir>/session.db)")
	cmd.Flags().StringVar(&flags.sessionID, "session", "", "Report only this session: a full ID, an unambiguous ID prefix, or a relative ref such as -1")
	cmd.Flags().DurationVar(&flags.since, "since", 0, "Report only sessions created within this duration (e.g. 24h)")
	cmd.Flags().BoolVar(&flags.asJSON, "json", false, "Emit the report as JSON")

	return cmd
}

func (f *usageFlags) run(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	dbPath, err := pathx.ExpandHomeDir(sessionDBPath(f.sessionDB))
	if err != nil {
		return err
	}

	store, err := sqlitestore.New(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("opening session store: %w", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			slog.ErrorContext(ctx, "Failed to close session store", "error", err)
		}
	}()

	sessions, err := loadUsageSessions(ctx, store, f.sessionID, f.since, time.Now())
	if err != nil {
		return err
	}

	return renderUsage(cmd.OutOrStdout(), usage.Aggregate(sessions), f.asJSON)
}

// loadUsageSessions fetches either one session or every session in the window.
//
// The listing is filtered on metadata first and only the survivors are loaded
// with their items. GetSessions pulls every message of every session into
// memory, which on a large store is gigabytes — far too much for a report that
// may be printing three rows, and for the CI use case --json exists for.
func loadUsageSessions(ctx context.Context, store session.Store, sessionRef string,
	since time.Duration, now time.Time,
) ([]*session.Session, error) {
	if sessionRef != "" {
		id, err := resolveSessionRef(ctx, store, sessionRef)
		if err != nil {
			return nil, err
		}
		s, err := store.GetSession(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("reading session %q: %w", sessionRef, err)
		}
		return []*session.Session{s}, nil
	}

	summaries, err := store.GetSessionSummaries(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}

	sessions := make([]*session.Session, 0, len(summaries))
	for _, summary := range keepSummariesSince(summaries, since, now) {
		s, err := store.GetSession(ctx, summary.ID)
		if err != nil {
			return nil, fmt.Errorf("reading session %q: %w", summary.ID, err)
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// keepSummariesSince drops sessions created before now-since. A non-positive
// since keeps everything, and a session with a zero CreatedAt is kept rather
// than silently dropped — an unknown timestamp is not evidence of age.
func keepSummariesSince(summaries []session.Summary, since time.Duration, now time.Time) []session.Summary {
	if since <= 0 {
		return summaries
	}
	cutoff := now.Add(-since)
	return slices.DeleteFunc(slices.Clone(summaries), func(s session.Summary) bool {
		return !s.CreatedAt.IsZero() && s.CreatedAt.Before(cutoff)
	})
}

// resolveSessionRef turns a user-supplied reference into a session ID,
// accepting the same relative forms as the rest of the CLI (-1 for the most
// recent) plus any unambiguous ID prefix.
//
// The prefix form exists because the report prints shortened IDs: rejecting the
// very string it just displayed makes --session unusable without a separate
// lookup, and there is no `sessions ls` to do that lookup with.
func resolveSessionRef(ctx context.Context, store session.Store, ref string) (string, error) {
	id, err := session.ResolveSessionID(ctx, store, ref)
	if err != nil {
		return "", err
	}
	if _, err := store.GetSession(ctx, id); err == nil {
		return id, nil
	}

	summaries, err := store.GetSessionSummaries(ctx)
	if err != nil {
		return "", fmt.Errorf("listing sessions: %w", err)
	}

	var matches []string
	for _, summary := range summaries {
		if strings.HasPrefix(summary.ID, id) {
			matches = append(matches, summary.ID)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no session matches %q", ref)
	default:
		return "", fmt.Errorf("%q matches %d sessions; use more characters", ref, len(matches))
	}
}

// renderUsage writes the report as JSON or as aligned text.
func renderUsage(w io.Writer, report usage.Report, asJSON bool) error {
	if asJSON {
		// Emit [] rather than null for the collections: `jq '.sessions[]'` on a
		// quiet day should yield nothing, not an error.
		if report.Sessions == nil {
			report.Sessions = []usage.SessionRow{}
		}
		if report.Models == nil {
			report.Models = []usage.ModelRow{}
		}
		if report.Tools == nil {
			report.Tools = []usage.ToolRow{}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	if len(report.Sessions) == 0 {
		_, err := fmt.Fprintln(w, "No sessions recorded.")
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)

	fmt.Fprintln(tw, "SESSION\tCREATED\tINPUT\tCACHED\tOUTPUT\tCOST\tTITLE")
	for _, s := range report.Sessions {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			shortSessionID(s.ID),
			formatUsageTime(s.CreatedAt),
			formatTokens(s.Tokens.Input),
			formatTokens(s.Tokens.CachedInput),
			formatTokens(s.Tokens.Output),
			formatUsageCost(s.Cost, s.CostIncomplete),
			truncateTitle(s.Title, 40),
		)
	}
	fmt.Fprintf(tw, "\t\t\t\t\t\t\n")
	fmt.Fprintf(tw, "%d session(s)\t\t%s\t%s\t%s\t%s\t\n",
		len(report.Sessions),
		formatTokens(report.Tokens.Input),
		formatTokens(report.Tokens.CachedInput),
		formatTokens(report.Tokens.Output),
		formatUsageCost(report.Cost, len(report.UnpricedModels) > 0),
	)

	if len(report.Models) > 0 {
		fmt.Fprintln(tw, "\t\t\t\t\t\t")
		fmt.Fprintln(tw, "MODEL\tCALLS\tINPUT\tCACHED\tOUTPUT\tCOST\t")
		for _, m := range report.Models {
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t\n",
				m.Model, m.Calls,
				formatTokens(m.Tokens.Input),
				formatTokens(m.Tokens.CachedInput),
				formatTokens(m.Tokens.Output),
				formatUsageCost(m.Cost, m.Tokens.AnySpend() && m.Cost == 0),
			)
		}
	}

	if len(report.Tools) > 0 {
		fmt.Fprintln(tw, "\t\t\t\t\t\t")
		fmt.Fprintln(tw, "TOOL\tCALLS\t\t\t\t\t")
		for _, t := range report.Tools {
			fmt.Fprintf(tw, "%s\t%d\t\t\t\t\t\n", t.Tool, t.Calls)
		}
	}

	if err := tw.Flush(); err != nil {
		return err
	}

	if len(report.UnpricedModels) > 0 {
		fmt.Fprintf(w, "\n! Cost is understated: no pricing for %s\n",
			strings.Join(report.UnpricedModels, ", "))
	}
	if unmetered := unmeteredModels(report.Models); len(unmetered) > 0 {
		fmt.Fprintf(w, "\n! Token counts are understated: no usage reported for some calls to %s\n",
			strings.Join(unmetered, ", "))
	}
	return nil
}

// unmeteredModels names the models that served at least one call the provider
// reported no usage for. Their token columns are short for a reason the table
// itself cannot show.
func unmeteredModels(models []usage.ModelRow) []string {
	var out []string
	for _, m := range models {
		if m.Unmetered > 0 {
			out = append(out, m.Model)
		}
	}
	return out
}

// formatUsageCost renders a cost, marking the ones known to be incomplete with a
// trailing "+" so an understated figure is never mistaken for the real one.
func formatUsageCost(cost float64, incomplete bool) string {
	s := fmt.Sprintf("$%.4f", cost)
	if cost >= 0.01 {
		s = fmt.Sprintf("$%.2f", cost)
	}
	if incomplete {
		s += "+"
	}
	return s
}

// formatTokens abbreviates large counts (1.2K, 3.4M) so columns stay narrow.
func formatTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return strconv.FormatInt(n, 10)
	}
}

func formatUsageTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func shortSessionID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// truncateTitle bounds a title to max display runes, so a long title cannot
// break the column layout. Rune-based to avoid splitting a multi-byte character.
func truncateTitle(title string, maxRunes int) string {
	title = strings.ReplaceAll(title, "\n", " ")
	runes := []rune(title)
	if len(runes) <= maxRunes {
		return title
	}
	if maxRunes <= 1 {
		return "…"
	}
	return string(runes[:maxRunes-1]) + "…"
}
