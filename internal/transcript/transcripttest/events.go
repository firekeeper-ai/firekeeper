package transcripttest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/transcript"
)

// Want is the expected shape of one event in CheckEvents.
type Want struct {
	// Seq is the event's seq. Zero means the row's index, as for JSONL
	// sources, whose seq is the line index. Seqs strictly increase, so only
	// the first event can have seq 0, and a row that needs another seq (a
	// SQLite source with gaps, such as OpenCode) sets it.
	Seq    int64
	Role   transcript.Role
	Text   string
	Tool   string
	Model  string
	Tokens transcript.Tokens
}

// CheckEvents checks events against expected, one row per event: seq, role,
// text, tool name, model, and tokens, and that every event has provider and
// sessionID and passes Event.Validate. Failure messages give text lengths,
// never text.
func CheckEvents(t testing.TB, events []transcript.Event, expected []Want, provider transcript.Provider, sessionID string) {
	t.Helper()
	if len(events) != len(expected) {
		t.Fatalf("got %d events, want %d", len(events), len(expected))
	}
	for i, e := range events {
		w := expected[i]
		seq := w.Seq
		if seq == 0 {
			seq = int64(i)
		}
		if err := e.Validate(); err != nil {
			t.Errorf("seq %d: %v", e.Seq, err)
		}
		if e.Seq != seq || e.Role != w.Role || e.Text != w.Text || Deref(e.ToolName) != w.Tool ||
			Deref(e.Model) != w.Model || e.Tokens != w.Tokens {
			t.Errorf("event %d: got seq %d role %s tool %q model %q tokens %+v text len %d (match %v); want seq %d role %s tool %q model %q tokens %+v text len %d",
				i, e.Seq, e.Role, Deref(e.ToolName), Deref(e.Model), e.Tokens, len(e.Text), e.Text == w.Text,
				seq, w.Role, w.Tool, w.Model, w.Tokens, len(w.Text))
		}
		if e.Provider != provider || e.SessionID != sessionID {
			t.Errorf("event %d: provider %q session %q; want provider %q session %q", i, e.Provider, e.SessionID, provider, sessionID)
		}
	}
}

// Deref is *s, or "" for nil, for comparing optional event fields.
func Deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// WriteFile writes contents to path, creating its directory, and sets its
// modification time to mtime, for enumeration fixtures.
func WriteFile(t testing.TB, path, contents string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// Day is noon UTC on day d of January 2026, a fixed time for fixtures.
func Day(d int) time.Time { return time.Date(2026, 1, d, 12, 0, 0, 0, time.UTC) }
