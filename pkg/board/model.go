// Package board implements a Kanban board for orchestrating docker agents.
// Each card owns an agent running in a tmux session on an isolated git
// worktree; the board observes and drives the agent through the control
// plane its run exposes with --listen. Columns form a pipeline: moving a
// card forward sends the destination column's prompt to its agent.
package board

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/docker/docker-agent/pkg/paths"
	"github.com/docker/docker-agent/pkg/userconfig"
)

// Column is one kanban column with the prompt sent to a card's agent when
// the card moves forward into it.
type Column struct {
	ID     string
	Name   string
	Emoji  string
	Prompt string
}

// DefaultColumns is the pipeline used when the user config defines none.
var DefaultColumns = []Column{
	{ID: "dev", Name: "Dev", Emoji: "🔨"},
	{ID: "review", Name: "Review", Emoji: "🔍", Prompt: "Review the local changes. Look for bugs, security issues, and code quality problems. Fix any issues you find."},
	{ID: "push", Name: "Push", Emoji: "🚀", Prompt: "Start by committing any remaining uncommitted files. Then rebase on top of the upstream default branch and fix any test failures and linter issues. Finally, squash all commits on this branch into a single commit with a clear and concise commit message. Push the branch to your fork (or the appropriate remote). Then use gh to open a pull request."},
	{ID: "done", Name: "Done", Emoji: "✅"},
}

// ColumnsFromConfig maps user-configured columns to board columns, falling
// back to [DefaultColumns] when none are configured. The config file is
// hand-editable, so entries are normalized instead of trusted: a missing id
// is derived from the name (hashed when the name slugs to nothing, e.g.
// non-ASCII, so the id stays stable across restarts), an id-less and
// nameless entry is dropped, and a duplicate id is dropped (card moves
// address columns by id, so duplicates would be ambiguous).
func ColumnsFromConfig(cols []userconfig.BoardColumn) []Column {
	out := make([]Column, 0, len(cols))
	seen := make(map[string]bool, len(cols))
	for _, c := range cols {
		id := strings.TrimSpace(c.ID)
		name := collapseSpace(c.Name)
		if id == "" {
			id = columnID(name)
		}
		if id == "" && name != "" {
			id = fallbackColumnID(name)
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if name == "" {
			name = id
		}
		out = append(out, Column{ID: id, Name: name, Emoji: collapseSpace(c.Emoji), Prompt: c.Prompt})
	}
	if len(out) == 0 {
		// Cloned: the caller may edit the pipeline in place.
		return slices.Clone(DefaultColumns)
	}
	return out
}

// columnID derives a column id from its display name: lowercased, with
// runs of separators collapsed to a dash and anything else non-alphanumeric
// dropped. Names that slug to nothing (e.g. emoji-only) yield "".
func columnID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			if s := b.String(); s != "" && !strings.HasSuffix(s, "-") {
				b.WriteByte('-')
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// fallbackColumnID returns a stable id for a name [columnID] slugs to
// nothing (e.g. non-ASCII or emoji-only): a hash of the name, so the id
// stays the same across restarts and cards stay attached to their column.
func fallbackColumnID(name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	return fmt.Sprintf("col-%08x", h.Sum32())
}

// collapseSpace trims a string and collapses inner whitespace (including
// newlines) to single spaces. Column names and emoji are rendered on
// single-line headers whose mouse hitboxes are computed arithmetically, so
// an embedded newline from a hand-edited config would break the layout.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// CardStatus tracks what a card's agent is doing.
type CardStatus string

const (
	// StatusStarting, StatusLoading, and StatusAttaching are the startup
	// phases, in launch order. The agent has not answered on its control
	// plane yet; the watcher refines the phase from the milestones the
	// agent materializes on disk, so a stuck launch shows how far it got,
	// and replaces it with a real status as soon as the agent emits events.
	//
	// StatusStarting: the tmux session is created and the agent process is
	// booting; it has not created the card's worktree yet.
	StatusStarting CardStatus = "starting"
	// StatusLoading: the worktree exists, so the agent is loading its
	// configuration, models, and tools.
	StatusLoading CardStatus = "loading"
	// StatusAttaching: the control-plane socket is bound; the board is
	// waiting for the agent to answer its first snapshot.
	StatusAttaching CardStatus = "attaching"

	StatusRunning CardStatus = "running"
	StatusWaiting CardStatus = "waiting"
	// StatusPaused marks a card whose turn is blocked on /pause. It lasts
	// until the runtime emits events again (resume) or the turn ends.
	StatusPaused CardStatus = "paused"
	// StatusError marks a card whose last turn failed. It is sticky: the
	// watcher keeps it until the next turn starts.
	StatusError CardStatus = "error"
)

// StartingUp reports whether the card is in a startup phase: its agent was
// launched but its control plane has not answered yet.
func (s CardStatus) StartingUp() bool {
	return s == StatusStarting || s == StatusLoading || s == StatusAttaching
}

// Busy reports whether the card's agent cannot accept a prompt right now: it
// is either still starting or in the middle of a turn.
func (s CardStatus) Busy() bool {
	return s.StartingUp() || s == StatusRunning
}

// Card is one task on the board.
type Card struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	Column   string     `json:"column"`
	Status   CardStatus `json:"status"`
	Project  string     `json:"project"`
	Agent    string     `json:"agent"`
	RepoPath string     `json:"repoPath"`
	Branch   string     `json:"branch"`
	Worktree string     `json:"worktree"`
	Session  string     `json:"session"`
	// AgentSession is the docker-agent conversation ID the card owns. It is
	// passed to `docker agent run --session` on every launch, so a session
	// recreated after the agent (or tmux) dies resumes the same conversation
	// instead of starting over.
	AgentSession string `json:"agentSession"`
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// newWorktreeName returns a unique worktree name for a card. docker-agent
// derives the worktree directory (<data>/worktrees/<name>) and branch
// (worktree-<name>) from it, so the name must be a single path segment.
func newWorktreeName() string {
	return "board-" + newID()
}

// newSessionName returns a unique tmux session name for a card.
func newSessionName() string {
	return "board-" + newID()[:8]
}

// socketPath returns the unix socket a card's agent control plane listens
// on. It is derived from the (unique) docker-agent session id, so it is
// stable across board restarts and needs no extra storage. The socket lives
// in the board's per-user socket dir (see socketDir) — not under the data
// dir, whose bind mount into a docker sandbox cannot host unix sockets: the
// agent would die at startup failing to bind --listen. Kept short to stay
// under the ~104-byte unix sun_path limit.
func socketPath(agentSession string) string {
	dir, err := socketDir()
	if err != nil {
		// The board cannot run at all when the socket dir is unusable (its
		// tmux socket lives there too); keep the path deterministic anyway.
		dir = os.TempDir()
	}
	return filepath.Join(dir, agentSession+".sock")
}

// worktreeDir returns the directory docker-agent creates for a worktree of
// the given name, mirroring its --worktree convention so the board can
// locate the worktree for diffs and cleanup.
func worktreeDir(name string) string {
	return filepath.Join(paths.GetDataDir(), "worktrees", name)
}

// worktreeBranch returns the branch docker-agent checks out for a worktree
// of the given name.
func worktreeBranch(name string) string {
	return "worktree-" + name
}

// placeholderTitle is the short temporary title shown until the agent emits
// its session_title event. It is the prompt's first line, trimmed and cut to
// a few words so a long prompt never becomes an unwieldy card title.
func placeholderTitle(prompt string) string {
	title := prompt
	if i := strings.IndexByte(title, '\n'); i >= 0 {
		title = title[:i]
	}
	title = strings.TrimSpace(title)

	const maxLen = 40
	runes := []rune(title)
	if len(runes) <= maxLen {
		return title
	}

	cut := string(runes[:maxLen])
	// Prefer a word boundary so the title does not end mid-word.
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut) + "…"
}
