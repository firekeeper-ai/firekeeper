package claudecloud

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/reporter"
	"github.com/firekeeper-ai/firekeeper/internal/session"
	"github.com/firekeeper-ai/firekeeper/internal/testenv/fakeapi"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
	"github.com/firekeeper-ai/firekeeper/internal/transcript/reader"
)

const testMachine = "0199ffff-0000-7000-8000-000000000001"

// cloudLocalRepo plants local metadata under a path that a cloud API might
// report. The synthetic reflog predates the cloud session.
func cloudLocalRepo(t *testing.T, marker bool) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "local-repo")
	cwd := filepath.Join(root, "nested")
	for _, dir := range []string{cwd, filepath.Join(root, ".git", "logs")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	hash := "1111111111111111111111111111111111111111"
	log := fmt.Sprintf("%040d %s Test <test@example.invalid> 1700000000 +0000\tcommit: initial\n", 0, hash)
	for path, content := range map[string]string{
		filepath.Join(root, ".git", "HEAD"):         "ref: refs/heads/local-branch\n",
		filepath.Join(root, ".git", "logs", "HEAD"): log,
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if marker {
		if err := os.WriteFile(filepath.Join(root, ".firekeeper-ignore"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return cwd
}

func TestBackfillCloudDoesNotResolveLocalCWD(t *testing.T) {
	for _, tc := range []struct {
		name    string
		marker  bool
		exclude bool
	}{
		{name: "local git"},
		{name: "local ignore marker", marker: true},
		{name: "local exclusion", exclude: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cwd := cloudLocalRepo(t, tc.marker)
			f, c := newFakeCloud(t)
			item := cloudSession(sid, "2026-10-01T11:00:00Z")
			item["session_context"].(map[string]any)["cwd"] = cwd
			item["external_metadata"] = map[string]any{}
			item["session_context"].(map[string]any)["outcomes"] = []map[string]any{}
			f.sessions = []map[string]any{item}
			f.events[sid] = conversation(sid)

			home := t.TempDir()
			machineDir := filepath.Join(home, ".firekeeper")
			if err := os.MkdirAll(machineDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(machineDir, "machine-id"), []byte(testMachine+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(machineDir, "cloud", "claude")
			syncOnce(t, c, dir, time.Time{})
			api := fakeapi.New()
			dashboard := httptest.NewServer(api.Handler())
			defer dashboard.Close()
			src := &Source{Dir: dir}
			only := func(p transcript.Provider) bool { return p == transcript.ProviderClaude }
			cfg := reporter.BackfillConfig{
				Config: reporter.Config{
					Server: dashboard.URL, Providers: []transcript.Provider{transcript.ProviderClaude}, Home: home,
					Sources: func(p transcript.Provider) (reader.Source, bool) { return src, only(p) },
				},
				Enumerators: func(p transcript.Provider) (reader.Enumerator, bool) { return src, only(p) },
				Sleep:       func(context.Context, time.Duration) error { return nil },
			}
			if tc.exclude {
				cfg.Exclude = []string{cwd}
			}
			result, err := reporter.Backfill(context.Background(), cfg)
			if err != nil || result.Failed() || len(result.Sessions) != 1 || result.Sessions[0].Events != 5 {
				t.Fatalf("backfill: %v, %+v", err, result)
			}
			stored, ok := api.Session(testMachine, sid)
			if !ok || stored.CWD != cwd || stored.Project != "nested" || stored.Branch != "" || stored.Commit != "" || stored.FilesSource == "git" {
				t.Fatalf("stored cloud session has local metadata: %+v", stored)
			}
		})
	}
}

// TestBackfillUploadsCloudSessions runs the cached cloud sessions through the
// real reporter: they upload under their cloud ids as provider claude, and a
// second pass, including after the session grew, sends only what is new.
func TestBackfillUploadsCloudSessions(t *testing.T) {
	f, c := newFakeCloud(t)
	f.sessions = []map[string]any{cloudSession(sid, "2026-10-01T11:00:00Z")}
	f.events[sid] = conversation(sid)

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".firekeeper"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".firekeeper", "machine-id"), []byte(testMachine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".firekeeper", "cloud", "claude")
	syncOnce(t, c, dir, time.Time{})

	api := fakeapi.New()
	dashboard := httptest.NewServer(api.Handler())
	defer dashboard.Close()
	src := &Source{Dir: dir}
	only := func(p transcript.Provider) bool { return p == transcript.ProviderClaude }
	pass := func() reporter.BackfillResult {
		t.Helper()
		result, err := reporter.Backfill(context.Background(), reporter.BackfillConfig{
			Config: reporter.Config{
				Server:    dashboard.URL,
				Providers: []transcript.Provider{transcript.ProviderClaude},
				Home:      home,
				Discover:  func(context.Context, session.Options) ([]session.Meta, error) { return nil, nil },
				Sources:   func(p transcript.Provider) (reader.Source, bool) { return src, only(p) },
			},
			Enumerators: func(p transcript.Provider) (reader.Enumerator, bool) { return src, only(p) },
			Sleep:       func(context.Context, time.Duration) error { return nil },
		})
		if err != nil || result.Failed() {
			t.Fatalf("backfill: %v, %+v", err, result.Sessions)
		}
		return result
	}

	first := pass()
	if len(first.Sessions) != 1 || first.Sessions[0].Events != 5 {
		t.Fatalf("first pass = %+v", first.Sessions)
	}
	if got := api.Events(testMachine, sid); len(got) != 5 || got[0].Provider != transcript.ProviderClaude {
		t.Fatalf("server holds %d events", len(got))
	}
	stored, ok := api.Session(testMachine, sid)
	if !ok || stored.Provider != "claude" || stored.Project != "demo" || stored.Model != "claude-sonnet-5-5" || stored.Title != "Fix the thing" || stored.Branch != "feature/x" || stored.CWD != "/claude-cloud/octo/demo" || stored.Origin != "cloud" {
		t.Fatalf("stored session = %+v", stored)
	}

	if again := pass(); len(again.Sessions) != 0 {
		t.Fatalf("second pass uploaded %d sessions, want none", len(again.Sessions))
	}

	// The session continues in the cloud; only the new event uploads.
	f.sessions = []map[string]any{cloudSession(sid, "2026-10-01T12:00:00Z")}
	f.events[sid] = append(f.events[sid], map[string]any{"type": "assistant", "uuid": "e8", "session_id": sid,
		"created_at": "2026-10-01T11:30:00.000000Z", "timestamp": "2026-10-01T11:30:00.500Z",
		"message": map[string]any{"id": "m4", "role": "assistant", "model": "claude-sonnet-5-5", "content": []map[string]any{{"type": "text", "text": "more"}}}})
	syncOnce(t, c, dir, time.Time{})
	third := pass()
	if len(third.Sessions) != 1 || third.Sessions[0].Events != 1 {
		t.Fatalf("third pass = %+v, want one new event", third.Sessions)
	}
	if got := api.Events(testMachine, sid); len(got) != 6 {
		t.Fatalf("server holds %d events, want 6", len(got))
	}
}
