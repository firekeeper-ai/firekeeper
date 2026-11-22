package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/session"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
)

// TestLoadRecentSessionsReadsProviderTranscripts runs the real Claude
// enumerator against a synthetic home and checks the list entry.
func TestLoadRecentSessionsReadsProviderTranscripts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, name := range []string{"CODEX_HOME", "COPILOT_HOME", "KIMI_CODE_HOME", "OPENCODE_DB", "XDG_DATA_HOME", "GEMINI_CLI_HOME", "CURSOR_CONFIG_DIR"} {
		t.Setenv(name, filepath.Join(home, "none-"+name))
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	project := filepath.Join(home, "repo")
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	head, _ := json.Marshal(map[string]any{"cwd": project, "timestamp": time.Now().Add(-time.Hour).Format(time.RFC3339)})
	dir := filepath.Join(home, ".claude", "projects", "-encoded")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const id = "0199c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d"
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, append(head, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	recent := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, recent, recent); err != nil {
		t.Fatal(err)
	}

	got := loadRecentSessions(context.Background(), time.Now())
	for _, s := range got {
		if s.provider == transcript.ProviderClaude && s.id == id {
			if providerName(s.provider) != "Claude" || s.project != "repo" || s.path != path || s.last.IsZero() {
				t.Fatalf("recent session fields wrong: %+v", s)
			}
			return
		}
	}
	t.Fatalf("claude session missing from %d results", len(got))
}

func TestBuildRecentReportJSON(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	last := now.Add(-3 * 24 * time.Hour)
	report := buildRecentReport(now, []recentSession{{
		provider: transcript.ProviderClaude,
		id:       "abc-123",
		project:  "firekeeper",
		title:    "",
		cwd:      "/tmp/firekeeper",
		last:     last,
	}})
	if report.GeneratedAt != now || len(report.Sessions) != 1 {
		t.Fatalf("report = %+v", report)
	}
	row := report.Sessions[0]
	if row.Provider != "claude" || row.SessionID != "abc-123" || row.Title == "" || row.LastActivityAt == nil || !row.LastActivityAt.Equal(last) {
		t.Fatalf("row = %+v", row)
	}
}

func TestRunRecentFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
		opts recentOptions
		json bool
	}{
		{"defaults", nil, 0, recentOptions{limit: 10, since: recentSessionAge, providers: map[transcript.Provider]bool{}, filterActivity: true}, false},
		{"json and filters", []string{"--json", "--limit", "3", "--since", "2h", "--provider", "claude", "--provider", "CODEX", "--project", "repo"}, 0, recentOptions{limit: 3, since: 2 * time.Hour, providers: map[transcript.Provider]bool{transcript.ProviderClaude: true, transcript.ProviderCodex: true}, project: "repo", filterActivity: true}, true},
		{"zero limit", []string{"--limit", "0"}, 2, recentOptions{}, false},
		{"negative limit", []string{"--limit", "-1"}, 2, recentOptions{}, false},
		{"noninteger limit", []string{"--limit", "x"}, 2, recentOptions{}, false},
		{"invalid since", []string{"--since", "no"}, 2, recentOptions{}, false},
		{"zero since", []string{"--since", "0s"}, 2, recentOptions{}, false},
		{"negative since", []string{"--since", "-1h"}, 2, recentOptions{}, false},
		{"invalid provider", []string{"--provider", "other"}, 2, recentOptions{}, false},
		{"empty provider", []string{"--provider", ""}, 2, recentOptions{}, false},
		{"empty project", []string{"--project", " "}, 2, recentOptions{}, false},
		{"extra argument", []string{"extra"}, 2, recentOptions{}, false},
		{"help", []string{"--help"}, 0, recentOptions{}, false},
	}
	previous := recentReportSource
	t.Cleanup(func() { recentReportSource = previous })
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got recentOptions
			called := false
			recentReportSource = func(_ context.Context, now time.Time, opts recentOptions) []recentSession {
				called, got = true, opts
				return []recentSession{{provider: transcript.ProviderCodex, id: "x", title: "Hello", last: now}}
			}
			var out, errb bytes.Buffer
			code := RunRecent(tt.args, &out, &errb)
			if code != tt.code {
				t.Fatalf("code=%d stderr=%q", code, errb.String())
			}
			if !called {
				if tt.code == 0 && tt.name != "help" {
					t.Fatal("source not called")
				}
				return
			}
			if tt.code != 0 || !reflect.DeepEqual(got, tt.opts) {
				t.Fatalf("options = %+v, want %+v", got, tt.opts)
			}
			if tt.json {
				var report RecentReport
				if err := json.Unmarshal(out.Bytes(), &report); err != nil || len(report.Sessions) != 1 || report.Sessions[0].Title != "Hello" {
					t.Fatalf("invalid JSON report: %v", err)
				}
			} else if !strings.Contains(out.String(), "PROVIDER") || !strings.Contains(out.String(), "Hello") {
				t.Fatal("missing table header or row")
			}
		})
	}
}

func TestRecentEnrichment(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	path := filepath.Join("..", "transcript", "claude", "testdata", "0199c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d.jsonl")
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	meta := session.Meta{ID: "0199c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d", RolloutPath: path, StartedAt: &start, State: session.SessionStateEnded}
	s := newRecentSession(transcript.ProviderClaude, meta)
	row := buildRecentReport(start, []recentSession{s}).Sessions[0]
	enrichRecentReport(context.Background(), &row, s)
	if row.Model != "claude-test" || row.Turns == nil || *row.Turns != 3 || row.Tokens == nil || row.Tokens.Total != 3786 || row.StartedAt == nil || row.State == nil || *row.State != session.SessionStateEnded {
		t.Fatalf("unexpected enrichment: model=%q turns=%v tokens=%v", row.Model, row.Turns, row.Tokens)
	}
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"session_id", "provider", "title", "project", "cwd", "last_activity_at", "model", "turns", "tokens", "started_at", "state"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("missing JSON field %q", key)
		}
	}
	s.meta.RolloutPath = filepath.Join("missing", "no.jsonl")
	row = buildRecentReport(start, []recentSession{s}).Sessions[0]
	enrichRecentReport(context.Background(), &row, s)
	if row.Turns != nil || row.Tokens != nil {
		t.Fatal("unreadable transcript must not produce totals")
	}
}

func TestFindRecentSessionsFilters(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, name := range []string{"CODEX_HOME", "COPILOT_HOME", "KIMI_CODE_HOME", "OPENCODE_DB", "XDG_DATA_HOME", "GEMINI_CLI_HOME", "CURSOR_CONFIG_DIR"} {
		t.Setenv(name, filepath.Join(home, "none-"+name))
	}
	claudeHome := filepath.Join(home, ".claude")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	dir := filepath.Join(claudeHome, "projects", "-encoded")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	fixtures := []struct {
		id      string
		project string
		age     time.Duration
	}{
		{"0199c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c60", "other", time.Minute},
		{"0199c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c61", "repo", 2 * time.Minute},
		{"0199c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c62", "repo", 3 * time.Minute},
		{"0199c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c63", "repo", 2 * time.Hour},
	}
	for _, f := range fixtures {
		path := filepath.Join(dir, f.id+".jsonl")
		head, _ := json.Marshal(map[string]any{"cwd": filepath.Join(home, f.project), "timestamp": now.Add(-f.age).Format(time.RFC3339)})
		if err := os.WriteFile(path, append(head, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-f.age), now.Add(-f.age)); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name string
		opts recentOptions
		ids  []string
	}{
		{"limit after project", recentOptions{limit: 1, since: time.Hour, project: "repo", filterActivity: true}, []string{fixtures[1].id}},
		{"since and order", recentOptions{limit: 10, since: time.Hour, filterActivity: true}, []string{fixtures[0].id, fixtures[1].id, fixtures[2].id}},
		{"provider included", recentOptions{limit: 10, since: time.Hour, providers: map[transcript.Provider]bool{transcript.ProviderClaude: true}, filterActivity: true}, []string{fixtures[0].id, fixtures[1].id, fixtures[2].id}},
		{"provider excluded", recentOptions{limit: 10, since: time.Hour, providers: map[transcript.Provider]bool{transcript.ProviderCodex: true}, filterActivity: true}, nil},
		{"missing project", recentOptions{limit: 10, since: time.Hour, project: "missing", filterActivity: true}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ids []string
			for _, s := range findRecentSessions(context.Background(), now, tt.opts) {
				ids = append(ids, s.id)
			}
			if !reflect.DeepEqual(ids, tt.ids) {
				t.Fatalf("got %d sessions, want %d", len(ids), len(tt.ids))
			}
		})
	}
}
