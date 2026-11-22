package tui

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/firekeeper-ai/firekeeper/internal/config"
	"github.com/firekeeper-ai/firekeeper/internal/parallel"
	"github.com/firekeeper-ai/firekeeper/internal/session"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
	"github.com/firekeeper-ai/firekeeper/internal/transcript/reader"
	"github.com/firekeeper-ai/firekeeper/internal/window"
)

// recent.go lists recently active sessions across providers in the idle
// Processes view. They are read from each provider's transcript enumerator,
// loaded only while nothing is running, and open in the chat view.

const (
	recentSessionLimit = 10
	recentSessionAge   = 30 * 24 * time.Hour
	recentRefreshEvery = 30 * time.Second
	recentPage         = 5
)

type recentSession struct {
	provider transcript.Provider
	id       string
	project  string
	title    string
	cwd      string
	path     string
	last     time.Time
	meta     session.Meta
}

type recentSessionsMsg struct {
	sessions []recentSession
	loadedAt time.Time
}

// recentSessionsSource loads recent sessions; tests replace it.
var recentSessionsSource = loadRecentSessions

// loadRecentSessions enumerates every provider concurrently. A provider that
// fails or has nothing is left out so one adapter never blocks the rest.
func loadRecentSessions(ctx context.Context, now time.Time) []recentSession {
	return findRecentSessions(ctx, now, recentOptions{limit: recentSessionLimit, since: recentSessionAge})
}

type recentOptions struct {
	limit          int
	since          time.Duration
	providers      map[transcript.Provider]bool
	project        string
	filterActivity bool
}

var recentReportSource = findRecentSessions

func findRecentSessions(ctx context.Context, now time.Time, opts recentOptions) []recentSession {
	providers := reader.Registered()
	results := make([][]recentSession, len(providers))
	parallel.Each(len(providers), func(index int) {
		provider := providers[index]
		if len(opts.providers) > 0 && !opts.providers[provider] {
			return
		}
		enumerator, ok := reader.EnumeratorFor(provider)
		if !ok {
			return
		}
		var enumerateOpts reader.EnumerateOptions
		if !opts.filterActivity {
			enumerateOpts.ModifiedAfter = now.Add(-opts.since)
		}
		metas, _ := enumerator.Enumerate(ctx, enumerateOpts)
		for _, meta := range metas {
			recent := newRecentSession(provider, meta)
			if (opts.filterActivity && recent.last.Before(now.Add(-opts.since))) || (opts.project != "" && recent.project != opts.project) {
				continue
			}
			results[index] = append(results[index], recent)
			if len(results[index]) == opts.limit {
				break
			}
		}
	})
	var all []recentSession
	for _, part := range results {
		all = append(all, part...)
	}
	sortRecentSessions(all)
	if len(all) > opts.limit {
		all = all[:opts.limit]
	}
	return all
}

func newRecentSession(provider transcript.Provider, meta session.Meta) recentSession {
	recent := recentSession{
		provider: provider,
		id:       meta.ID,
		project:  reader.ProjectName(meta.CWD),
		title:    meta.Title,
		cwd:      meta.CWD,
		path:     meta.RolloutPath,
		meta:     meta,
	}
	switch {
	case meta.LastActivityAt != nil:
		recent.last = *meta.LastActivityAt
	case meta.RolloutPath != "":
		if info, err := os.Stat(meta.RolloutPath); err == nil {
			recent.last = info.ModTime()
		}
	}
	return recent
}

func sortRecentSessions(sessions []recentSession) {
	for i := 1; i < len(sessions); i++ {
		for j := i; j > 0 && recentBefore(sessions[j-1], sessions[j]); j-- {
			sessions[j-1], sessions[j] = sessions[j], sessions[j-1]
		}
	}
}

// recentBefore reports whether a sorts after b (older, ties by id).
func recentBefore(a, b recentSession) bool {
	if !a.last.Equal(b.last) {
		return a.last.Before(b.last)
	}
	return a.id > b.id
}

func loadRecentSessionsCmd() tea.Cmd {
	return func() tea.Msg {
		now := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return recentSessionsMsg{sessions: recentSessionsSource(ctx, now), loadedAt: now}
	}
}

// maybeLoadRecent starts a load when the idle view needs fresh data.
func (m *model) maybeLoadRecent() tea.Cmd {
	if len(m.processGroups) != 0 || m.processErr != "" || m.recentLoading {
		return nil
	}
	if !m.recentLoadedAt.IsZero() && time.Since(m.recentLoadedAt) < recentRefreshEvery {
		return nil
	}
	m.recentLoading = true
	return loadRecentSessionsCmd()
}

func (m *model) applyRecentSessions(msg recentSessionsMsg) {
	selected := ""
	if m.recentCursor < len(m.recentSessions) {
		selected = m.recentSessions[m.recentCursor].id
	}
	m.recentLoading = false
	m.recentLoadedAt = msg.loadedAt
	m.recentSessions = msg.sessions
	m.recentCursor = 0
	for index, recent := range msg.sessions {
		if recent.id == selected {
			m.recentCursor = index
			break
		}
	}
}

// idleListActive reports whether the idle recent list takes the cursor keys.
func (m model) idleListActive() bool {
	return m.activeTab == processesTab && len(m.processGroups) == 0 && len(m.recentSessions) > 0
}

func (m *model) moveRecentCursor(delta int) {
	m.recentCursor = min(max(m.recentCursor+delta, 0), len(m.recentSessions)-1)
}

func (m *model) openRecentChat() tea.Cmd {
	if m.recentCursor >= len(m.recentSessions) {
		return nil
	}
	recent := m.recentSessions[m.recentCursor]
	m.chat = chatState{
		open:     true,
		provider: recent.provider,
		meta: session.Meta{
			ID:          recent.id,
			Provider:    recent.provider,
			CWD:         recent.cwd,
			RolloutPath: recent.path,
		},
		title:   emptyFallback(recent.project, "session") + "/" + dashShortID(recent.id),
		loading: true,
		follow:  true,
	}
	return loadChat(0, m.chat.meta, nil, nil)
}

// recentLines renders the list as one block centered in width, windowed
// around the cursor to capacity rows (one of which is the heading).
func (m model) recentLines(capacity int) []string {
	if len(m.recentSessions) == 0 || capacity < 2 {
		return nil
	}
	const fixed = 2 + 9 + 16 + 5 // marker, tool, project, age
	titleWidth := 0
	for _, recent := range m.recentSessions {
		titleWidth = max(titleWidth, ansi.StringWidth(recentTitle(recent)))
	}
	titleWidth = min(titleWidth, 32, max(m.width-fixed-2, 0))
	pad := dashPaint(strings.Repeat(" ", max((m.width-fixed-titleWidth)/2, 0)), usageTextColor, false)
	heading := "Recent sessions"
	lines := []string{dashPaint(strings.Repeat(" ", max((m.width-len(heading))/2, 0)), usageTextColor, false) + dashPaint(heading, usageBrightColor, true)}
	start, end := usageSessionWindow(len(m.recentSessions), m.recentCursor, capacity-1)
	now := time.Now()
	for index := start; index < end; index++ {
		recent := m.recentSessions[index]
		marker, markerColor := "  ", usageMutedColor
		if index == m.recentCursor {
			marker, markerColor = "▸ ", dashOrange
		}
		lines = append(lines, pad+
			dashPaint(marker, markerColor, true)+
			dashPaint(padText(toolLabel(recent.provider), 9+toolIconWidth()), providerColor(recent.provider), true)+
			dashPaint(padText(sanitizeProcessCommand(emptyFallback(recent.project, "—")), 16), usageTextColor, false)+
			dashPaint(padText(usageAgo(recent.last, now), 5), usageMutedColor, false)+
			dashPaint(ansi.Truncate(recentTitle(recent), titleWidth, "…"), usageMutedColor, false))
	}
	return lines
}

func recentTitle(recent recentSession) string {
	return sanitizeProcessCommand(emptyFallback(recent.title, dashShortID(recent.id)))
}

// RecentSessionReport is one row of `firekeeper recent --json`.
type RecentSessionReport struct {
	SessionID      string              `json:"session_id"`
	Provider       transcript.Provider `json:"provider"`
	Title          string              `json:"title"`
	Project        string              `json:"project"`
	CWD            string              `json:"cwd"`
	LastActivityAt *time.Time          `json:"last_activity_at"`
	Model          string              `json:"model,omitempty"`
	Turns          *int                `json:"turns,omitempty"`
	Tokens         *window.Tokens      `json:"tokens,omitempty"`
	StartedAt      *time.Time          `json:"started_at,omitempty"`
	State          *session.State      `json:"state,omitempty"`
}

// RecentReport is the output of `firekeeper recent --json`: the same cross-
// provider list (by default, last 30 days and newest 10).
type RecentReport struct {
	GeneratedAt time.Time             `json:"generated_at"`
	Sessions    []RecentSessionReport `json:"sessions"`
}

// RunRecent implements the recent subcommand for the menu bar and scripts.
func RunRecent(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("firekeeper recent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print recent sessions as JSON")
	ndjson := fs.Bool("ndjson", false, "print one session per JSON line")
	limit := fs.Int("limit", recentSessionLimit, "maximum number of sessions (positive)")
	since := fs.String("since", recentSessionAge.String(), "look back by a positive duration (for example 24h)")
	project := fs.String("project", "", "match an exact project name")
	providers := make(map[transcript.Provider]bool)
	fs.Func("provider", "filter by provider (repeatable)", func(value string) error {
		p, err := config.ParseProvider(value)
		if err != nil {
			return err
		}
		providers[p] = true
		return nil
	})
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *asJSON && *ndjson {
		fmt.Fprintln(stderr, "firekeeper recent: use --json or --ndjson, not both")
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "firekeeper recent: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *limit <= 0 {
		fmt.Fprintln(stderr, "firekeeper recent: --limit must be positive")
		return 2
	}
	duration, err := time.ParseDuration(*since)
	if err != nil || duration <= 0 {
		fmt.Fprintln(stderr, "firekeeper recent: --since must be a positive duration")
		return 2
	}
	projectSet := false
	fs.Visit(func(f *flag.Flag) { projectSet = projectSet || f.Name == "project" })
	if projectSet && strings.TrimSpace(*project) == "" {
		fmt.Fprintln(stderr, "firekeeper recent: --project must not be empty")
		return 2
	}

	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sessions := recentReportSource(ctx, now, recentOptions{limit: *limit, since: duration, providers: providers, project: *project, filterActivity: true})
	report := buildRecentReport(now, sessions)
	parallel.Bounded(len(sessions), func(i int) {
		enrichRecentReport(ctx, &report.Sessions[i], sessions[i])
	})
	if !*asJSON && !*ndjson {
		if err := writeRecentTable(stdout, report); err != nil {
			fmt.Fprintf(stderr, "firekeeper recent: write table: %v\n", err)
			return 1
		}
		return 0
	}
	enc := json.NewEncoder(stdout)
	if *ndjson {
		for _, session := range report.Sessions {
			if err := enc.Encode(session); err != nil {
				fmt.Fprintf(stderr, "firekeeper recent: write JSONL: %v\n", err)
				return 1
			}
		}
		return 0
	}
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(stderr, "firekeeper recent: write JSON: %v\n", err)
		return 1
	}
	return 0
}

func buildRecentReport(now time.Time, sessions []recentSession) RecentReport {
	out := make([]RecentSessionReport, 0, len(sessions))
	for _, s := range sessions {
		row := RecentSessionReport{
			SessionID: s.id,
			Provider:  s.provider,
			Title:     emptyFallback(s.title, dashShortID(s.id)),
			Project:   s.project,
			CWD:       s.cwd,
			Model:     s.meta.Model,
			StartedAt: s.meta.StartedAt,
		}
		if s.meta.State != session.SessionStateUnknown {
			state := s.meta.State
			row.State = &state
		}
		if !s.last.IsZero() {
			last := s.last
			row.LastActivityAt = &last
		}
		out = append(out, row)
	}
	return RecentReport{GeneratedAt: now, Sessions: out}
}

// enrichRecentReport reads only the selected sessions. On any read failure,
// partial totals are omitted rather than presented as complete.
func enrichRecentReport(ctx context.Context, row *RecentSessionReport, s recentSession) {
	source, ok := reader.For(s.provider)
	if !ok || ctx.Err() != nil {
		return
	}
	paths, err := source.Locate(s.meta)
	if err != nil || len(paths) == 0 {
		return
	}
	var turns int
	var tokens window.Tokens
	model := row.Model
	for _, path := range paths {
		if ctx.Err() != nil {
			return
		}
		events, _, err := source.Read(path, 0)
		if err != nil {
			return
		}
		for _, event := range events {
			if event.Role == transcript.RoleAssistant {
				turns++
			}
			tokens.Add(event.Tokens)
			if event.Model != nil && *event.Model != "" {
				model = *event.Model
			}
		}
	}
	if ctx.Err() != nil {
		return
	}
	row.Model, row.Turns, row.Tokens = model, &turns, &tokens
}

func writeRecentTable(out io.Writer, report RecentReport) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tPROJECT\tSESSION\tTITLE\tMODEL\tTURNS\tTOKENS\tSTARTED\tSTATE\tLAST ACTIVITY")
	for _, row := range report.Sessions {
		turns, tokens, started, state, last := "—", "—", "—", "—", "—"
		if row.Turns != nil {
			turns = fmt.Sprint(*row.Turns)
		}
		if row.Tokens != nil {
			tokens = fmt.Sprint(row.Tokens.Total)
		}
		if row.StartedAt != nil {
			started = row.StartedAt.Format(time.RFC3339)
		}
		if row.State != nil {
			state = row.State.String()
		}
		if row.LastActivityAt != nil {
			last = row.LastActivityAt.Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Provider, sanitizeProcessCommand(emptyFallback(row.Project, "—")),
			sanitizeProcessCommand(row.SessionID), sanitizeProcessCommand(row.Title),
			sanitizeProcessCommand(emptyFallback(row.Model, "—")), turns, tokens, started, state, last)
	}
	return w.Flush()
}
