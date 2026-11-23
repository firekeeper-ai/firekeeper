package transcripttest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/transcript"
)

// recorder collects CheckEvents failures instead of failing the test.
type recorder struct {
	testing.TB
	msgs  []string
	fatal bool
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	r.fatal = true
	runtime.Goexit()
}

// check runs CheckEvents against a recorder on its own goroutine, so Fatalf
// can stop it.
func check(t *testing.T, events []transcript.Event, expected []Want, provider transcript.Provider, sessionID string) *recorder {
	r := &recorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		CheckEvents(r, events, expected, provider, sessionID)
	}()
	<-done
	return r
}

const private = "synthetic-private-text"

func events() []transcript.Event {
	model, tool := "model-test", "Bash"
	return []transcript.Event{
		{Provider: transcript.ProviderCodex, SessionID: "s1", Seq: 0, Role: transcript.RoleUser, Text: private},
		{Provider: transcript.ProviderCodex, SessionID: "s1", Seq: 1, Role: transcript.RoleToolCall, Text: `{"command":"ls"}`,
			ToolName: &tool, Model: &model, Tokens: transcript.Tokens{Input: 3, Output: 4, Cache: 5, CacheRead: 5}},
	}
}

func wants() []Want {
	return []Want{
		{Role: transcript.RoleUser, Text: private},
		{Role: transcript.RoleToolCall, Text: `{"command":"ls"}`, Tool: "Bash", Model: "model-test",
			Tokens: transcript.Tokens{Input: 3, Output: 4, Cache: 5, CacheRead: 5}},
	}
}

func TestCheckEventsPasses(t *testing.T) {
	if r := check(t, events(), wants(), transcript.ProviderCodex, "s1"); len(r.msgs) != 0 {
		t.Fatalf("matching events failed: %v", r.msgs)
	}
	// A source with seq gaps names its seqs; zero still means the index.
	gapped := events()
	gapped[1].Seq = 7
	w := wants()
	w[1].Seq = 7
	if r := check(t, gapped, w, transcript.ProviderCodex, "s1"); len(r.msgs) != 0 {
		t.Fatalf("explicit seqs failed: %v", r.msgs)
	}
	if r := check(t, nil, nil, transcript.ProviderCodex, "s1"); len(r.msgs) != 0 {
		t.Fatalf("no events failed: %v", r.msgs)
	}
}

func TestCheckEventsReportsMismatches(t *testing.T) {
	for name, change := range map[string]func(e []transcript.Event, w []Want){
		"seq":          func(e []transcript.Event, _ []Want) { e[1].Seq = 5 },
		"explicit seq": func(_ []transcript.Event, w []Want) { w[1].Seq = 9 },
		"role":         func(e []transcript.Event, _ []Want) { e[1].Role = transcript.RoleAssistant },
		"text":         func(e []transcript.Event, _ []Want) { e[0].Text = private + "!" },
		"tool":         func(e []transcript.Event, _ []Want) { e[1].ToolName = nil },
		"model":        func(_ []transcript.Event, w []Want) { w[1].Model = "other" },
		"tokens":       func(e []transcript.Event, _ []Want) { e[1].Tokens.Output = 0 },
		"provider":     func(e []transcript.Event, _ []Want) { e[0].Provider = transcript.ProviderClaude },
		"session":      func(e []transcript.Event, _ []Want) { e[1].SessionID = "s2" },
		"invalid role": func(e []transcript.Event, w []Want) { e[0].Role = "bot"; w[0].Role = "bot" },
	} {
		t.Run(name, func(t *testing.T) {
			e, w := events(), wants()
			change(e, w)
			r := check(t, e, w, transcript.ProviderCodex, "s1")
			if len(r.msgs) == 0 {
				t.Fatal("mismatch not reported")
			}
			if r.fatal {
				t.Error("a field mismatch stopped the check; only a count mismatch should")
			}
			for _, msg := range r.msgs {
				if strings.Contains(msg, private) {
					t.Fatalf("failure message disclosed event text: %s", msg)
				}
			}
		})
	}
}

func TestCheckEventsStopsOnCountMismatch(t *testing.T) {
	r := check(t, events(), wants()[:1], transcript.ProviderCodex, "s1")
	if !r.fatal || len(r.msgs) != 1 || !strings.Contains(r.msgs[0], "got 2 events, want 1") {
		t.Fatalf("count mismatch: fatal %v, messages %v", r.fatal, r.msgs)
	}
}

func TestDeref(t *testing.T) {
	value := "x"
	if Deref(nil) != "" || Deref(&value) != "x" {
		t.Fatal("Deref")
	}
}

func TestWriteFileAndDay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "file.jsonl")
	WriteFile(t, path, "line\n", Day(3))
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "line\n" {
		t.Fatalf("contents %q, %v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.ModTime().Equal(Day(3)) {
		t.Fatalf("mtime %v, %v; want %v", info.ModTime(), err, Day(3))
	}
	if Day(3).Location() != time.UTC || Day(3).Day() != 3 || Day(4).Sub(Day(3)) != 24*time.Hour {
		t.Fatal("Day is not noon UTC on consecutive days")
	}
}
