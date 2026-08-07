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

Cost comes from each session's own recorded counter; this command never re-prices
anything. Token figures are summed from per-message usage, which is where the
cached-input breakdown lives, so prompt-caching wins are visible.

A session that moved tokens but recorded no cost means the model was missing from
the pricing catalogue. Those are flagged rather than reported as $0.00, because a
report that quietly understates spend is worse than no report.`,
		Example: `  docker agent usage
  docker agent usage --since 24h
  docker agent usage --session <id>
  docker agent usage --json | jq '.cost'`,
		RunE: flags.run,
	}

	cmd.Flags().StringVarP(&flags.sessionDB, "session-db", "s", "", "Path to the session database (default: <data-dir>/session.db)")
	cmd.Flags().StringVar(&flags.sessionID, "session", "", "Report only this session ID")
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

	sessions, err := loadUsageSessions(ctx, store, f.sessionID)
	if err != nil {
		return err
	}

	report := usage.Aggregate(filterSessions(sessions, f.since, time.Now()))
	return renderUsage(cmd.OutOrStdout(), report, f.asJSON)
}

// loadUsageSessions fetches either one session or all of them.
func loadUsageSessions(ctx context.Context, store session.Store, sessionID string) ([]*session.Session, error) {
	if sessionID != "" {
		s, err := store.GetSession(ctx, sessionID)
		if err != nil {
			return nil, fmt.Errorf("reading session %q: %w", sessionID, err)
		}
		return []*session.Session{s}, nil
	}
	sessions, err := store.GetSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}
	return sessions, nil
}

// filterSessions drops sessions created before now-since. A non-positive since
// keeps everything, and a session with a zero CreatedAt is kept rather than
// silently dropped — an unknown timestamp is not evidence of age.
func filterSessions(sessions []*session.Session, since time.Duration, now time.Time) []*session.Session {
	if since <= 0 {
		return sessions
	}
	cutoff := now.Add(-since)
	return slices.DeleteFunc(slices.Clone(sessions), func(s *session.Session) bool {
		return s != nil && !s.CreatedAt.IsZero() && s.CreatedAt.Before(cutoff)
	})
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
		fmt.Fprintln(tw, "MODEL\tCALLS\tINPUT\tCACHED\tOUTPUT\t\t")
		for _, m := range report.Models {
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t\t\n",
				m.Model, m.Calls,
				formatTokens(m.Tokens.Input),
				formatTokens(m.Tokens.CachedInput),
				formatTokens(m.Tokens.Output),
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
