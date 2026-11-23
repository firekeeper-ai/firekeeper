package kimi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/session"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
	"github.com/firekeeper-ai/firekeeper/internal/transcript/reader"
	"github.com/firekeeper-ai/firekeeper/internal/transcript/transcripttest"
)

const (
	fixtureHome = "testdata"
	fixtureID   = "session_0199c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d"
	fixtureDir  = "testdata/sessions/wd_demo_0123456789ab/" + fixtureID
	fixturePath = fixtureDir + "/agents/main/wire.jsonl"
	legacyID    = "session_legacy01"
	legacyPath  = "testdata/sessions/wd_legacy_ba9876543210/" + legacyID + "/agents/main/wire.jsonl"
	malformed   = "testdata/malformed.jsonl"
	model       = "kimi-k3-test"
)

func TestReadTranscript(t *testing.T) {
	events, end, err := (&Source{}).Read(fixturePath, 0)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(fixturePath)
	if end != info.Size() {
		t.Fatalf("end offset = %d, want file size %d", end, info.Size())
	}
	expected := []transcripttest.Want{
		0: {Role: transcript.RoleMeta},
		1: {Role: transcript.RoleMeta},
		2: {Role: transcript.RoleMeta},
		// turn.prompt repeats the user message that follows it.
		3: {Role: transcript.RoleMeta},
		4: {Role: transcript.RoleUser, Text: "List the files."},
		5: {Role: transcript.RoleMeta, Model: model},
		6: {Role: transcript.RoleMeta},
		7: {Role: transcript.RoleMeta, Text: "Planning.", Model: model},
		8: {Role: transcript.RoleAssistant, Text: "Listing files.", Model: model},
		9: {Role: transcript.RoleToolCall, Text: `{"command":"ls"}`, Tool: "Bash", Model: model},
		// step.end repeats usage.record's counts, so only the latter has them.
		10: {Role: transcript.RoleMeta},
		11: {Role: transcript.RoleMeta, Model: model, Tokens: transcript.Tokens{Input: 10, Output: 20, Cache: 105, CacheRead: 100, CacheWrite: 5}},
		12: {Role: transcript.RoleToolResult, Text: "README.md", Tool: "Bash"},
		13: {Role: transcript.RoleMeta, Model: model},
		14: {Role: transcript.RoleMeta},
		15: {Role: transcript.RoleToolCall, Text: `{"path":"README.md","old_string":"a","new_string":"b"}`, Tool: "Edit", Model: model},
		16: {Role: transcript.RoleToolResult, Text: "Replaced 1 occurrence in README.md", Tool: "Edit"},
		17: {Role: transcript.RoleAssistant, Text: "Done editing.", Model: model},
		18: {Role: transcript.RoleMeta},
		19: {Role: transcript.RoleMeta, Model: model, Tokens: transcript.Tokens{Input: 3, Output: 4, Cache: 200, CacheRead: 200}},
		20: {Role: transcript.RoleMeta},
		21: {Role: transcript.RoleSystem, Text: "<system-reminder>Todo list changed.</system-reminder>"},
		22: {Role: transcript.RoleSystem, Text: "Background task finished."},
		23: {Role: transcript.RoleUser, Text: "/review"},
		24: {Role: transcript.RoleSystem, Text: "Compacted summary."},
		// Session-scope usage (titles, compaction) still counts.
		25: {Role: transcript.RoleMeta, Model: model, Tokens: transcript.Tokens{Input: 1, Output: 2}},
		26: {Role: transcript.RoleMeta},
		27: {Role: transcript.RoleMeta, Model: model},
		28: {Role: transcript.RoleMeta},
	}
	transcripttest.CheckEvents(t, events, expected, transcript.ProviderKimi, fixtureID)
	for _, e := range events {
		if e.TS == nil {
			t.Errorf("seq %d: missing timestamp", e.Seq)
		}
	}
	if got := events[0].TS; got == nil || got.UnixMilli() != 1791460800000 {
		t.Errorf("metadata timestamp = %v, want created_at", got)
	}
	if got, want := events[28].TS, time.Date(2026, 10, 8, 0, 0, 0, 300e6, time.UTC); got == nil || !got.Equal(want) {
		t.Errorf("RFC 3339 timestamp = %v, want %v", got, want)
	}
}

func TestReadMigratedTranscript(t *testing.T) {
	events, _, err := (&Source{}).Read(legacyPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	transcripttest.CheckEvents(t, events, []transcripttest.Want{
		0: {Role: transcript.RoleMeta},
		1: {Role: transcript.RoleMeta},
		2: {Role: transcript.RoleSystem, Text: "You are Kimi."},
		3: {Role: transcript.RoleUser, Text: "Write the file."},
		4: {Role: transcript.RoleToolCall, Text: "Writing.\n" + `{"path": "notes.md", "content": "hi"}`, Tool: "Write"},
		5: {Role: transcript.RoleToolResult, Text: "Wrote 2 bytes to notes.md", Tool: "Write"},
		6: {Role: transcript.RoleAssistant, Text: "Done."},
		7: {Role: transcript.RoleMeta, Text: "Nothing left."},
		8: {Role: transcript.RoleSystem, Text: "Summary message."},
	}, transcript.ProviderKimi, legacyID)
}

func TestReadMalformed(t *testing.T) {
	events, _, err := (&Source{}).Read(malformed, 0)
	if err != nil {
		t.Fatal(err)
	}
	expected := []transcripttest.Want{
		0:  {Role: transcript.RoleMeta},
		1:  {Role: transcript.RoleMeta},
		2:  {Role: transcript.RoleMeta},
		3:  {Role: transcript.RoleMeta},
		4:  {Role: transcript.RoleMeta},
		5:  {Role: transcript.RoleMeta},
		6:  {Role: transcript.RoleToolResult},
		7:  {Role: transcript.RoleSystem},
		8:  {Role: transcript.RoleMeta},
		9:  {Role: transcript.RoleMeta, Tokens: transcript.Tokens{Output: 2, Cache: 3, CacheWrite: 3}},
		10: {Role: transcript.RoleMeta},
		11: {Role: transcript.RoleSystem},
	}
	// The path is not a session log, so no session id is known.
	transcripttest.CheckEvents(t, events, expected, transcript.ProviderKimi, "")
	// Only seq 5 carries a usable time; undecodable, bogus, negative,
	// overflowing, and missing times are null.
	for _, e := range events {
		if hasTime := e.Seq == 5; hasTime != (e.TS != nil) {
			t.Errorf("seq %d: timestamp present = %v", e.Seq, e.TS != nil)
		}
	}
	var quoted string
	if json.Unmarshal(events[0].Raw, &quoted) != nil {
		t.Error("seq 0: invalid JSON line should be kept as a JSON string in raw")
	}
}

func TestSeqStability(t *testing.T) {
	// Kimi derives the session ID from the directory, not the wire records.
	partial := func(path, id string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.SplitAfter(string(data), "\n")
		dir := filepath.Join(t.TempDir(), id, "agents", "main")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, "wire.jsonl")
		if err := os.WriteFile(out, []byte(strings.Join(lines[:len(lines)/2], "")), 0o600); err != nil {
			t.Fatal(err)
		}
		return out
	}
	transcripttest.RunSeqStability(t, &Source{}, []transcripttest.SeqCase{
		{Name: "transcript", Path: fixturePath, Partial: partial(fixturePath, fixtureID)},
		{Name: "migrated", Path: legacyPath, Partial: partial(legacyPath, legacyID)},
		{Name: "malformed", Path: malformed},
	})
}

// A split read must recover tool names and the requested model from before
// the offset, including boundaries beyond the stability helper's midpoint.
func TestReadFromOffsetRestoresState(t *testing.T) {
	for _, path := range []string{fixturePath, legacyPath} {
		source := &Source{}
		whole, _, err := source.Read(path, 0)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(path)
		for split := 1; split < len(whole); split++ {
			offset := 0
			for range split {
				offset += strings.IndexByte(string(data[offset:]), '\n') + 1
			}
			tail, _, err := source.Read(path, int64(offset))
			if err != nil {
				t.Fatal(err)
			}
			if len(tail) != len(whole)-split {
				t.Fatalf("split %d: got %d events, want %d", split, len(tail), len(whole)-split)
			}
			for i, e := range tail {
				w := whole[split+i]
				if e.Seq != w.Seq || e.Tokens != w.Tokens || transcripttest.Deref(e.Model) != transcripttest.Deref(w.Model) ||
					transcripttest.Deref(e.ToolName) != transcripttest.Deref(w.ToolName) || e.SessionID != w.SessionID {
					t.Errorf("split %d: seq %d differs between split and whole reads", split, w.Seq)
				}
			}
		}
	}
}

func TestReadIncompleteTrailingLine(t *testing.T) {
	data, _ := os.ReadFile(fixturePath)
	lines := strings.SplitAfter(string(data), "\n")
	path := filepath.Join(t.TempDir(), "wire.jsonl")
	complete := strings.Join(lines[:4], "")
	if err := os.WriteFile(path, []byte(complete+lines[4][:10]), 0o600); err != nil {
		t.Fatal(err)
	}
	events, offset, err := (&Source{}).Read(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || offset != int64(len(complete)) {
		t.Fatalf("got %d events at offset %d; want 4 at %d", len(events), offset, len(complete))
	}
	// The writer finishes the line; the next read picks it up as seq 4.
	if err := os.WriteFile(path, []byte(complete+lines[4]), 0o600); err != nil {
		t.Fatal(err)
	}
	events, _, err = (&Source{}).Read(path, offset)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Seq != 4 || events[0].Role != transcript.RoleUser {
		t.Fatalf("resumed read gave %d events", len(events))
	}
}

func TestReadBadOffsets(t *testing.T) {
	info, _ := os.Stat(fixturePath)
	for name, offset := range map[string]int64{
		"negative":      -1,
		"past end":      info.Size() + 1,
		"mid line":      5,
		"truncated end": info.Size() - 1,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := (&Source{}).Read(fixturePath, offset); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	_, _, err := (&Source{}).Read(filepath.Join(t.TempDir(), "missing.jsonl"), 0)
	if err == nil || strings.Contains(err.Error(), string(filepath.Separator)) {
		t.Fatalf("missing file error = %v; want an error without a path", err)
	}
}

func TestToolOutputTruncated(t *testing.T) {
	big := strings.Repeat("é", transcript.MaxToolText) // two bytes per rune
	line, _ := json.Marshal(map[string]any{
		"type": "context.append_loop_event",
		"time": 1791460800000,
		"event": map[string]any{"type": "tool.result", "toolCallId": "Bash:0",
			"result": map[string]any{"output": big}},
	})
	path := filepath.Join(t.TempDir(), "wire.jsonl")
	if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	events, _, err := (&Source{}).Read(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	text := events[0].Text
	kept, marker, ok := strings.Cut(text, "\n[truncated ")
	if !ok || len(kept) != transcript.MaxToolText || marker != "65536 bytes]" {
		t.Fatalf("kept %d bytes, marker %q", len(kept), marker)
	}
	if !strings.Contains(string(events[0].Raw), big) {
		t.Fatal("raw should keep the full output")
	}
}

func TestUserTyped(t *testing.T) {
	for raw, want := range map[string]bool{
		``:                              true,
		`null`:                          true,
		`"user"`:                        true,
		`{"kind":"user"}`:               true,
		`{}`:                            true,
		`"shell_command"`:               true,
		`"injection"`:                   false,
		`{"kind":"retry"}`:              false,
		`"cron_job"`:                    false,
		`{"kind":"compaction_summary"}`: false,
		`{"kind":"plugin_command","trigger":"user-slash"}`:     true,
		`{"kind":"skill_activation","trigger":"model"}`:        false,
		`{"kind":"system_trigger","name":"goal_continuation"}`: false,
		`7`:  false,
		`[]`: false,
	} {
		if got := userTyped(json.RawMessage(raw)); got != want {
			t.Errorf("userTyped(%s) = %v, want %v", raw, got, want)
		}
	}
}

func TestSessionIDFromPath(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{fixturePath, fixtureID},
		{filepath.Join(fixtureDir, "agents", "agent-0", "wire.jsonl"), ""},
		{filepath.Join(fixtureDir, "agents", "main", "context.jsonl"), ""},
		{"wire.jsonl", ""},
		{"/agents/main/wire.jsonl", ""},
		{"sessions/w/bad id/agents/main/wire.jsonl", ""},
	} {
		if got := sessionIDFromPath(tc.path); got != tc.want {
			t.Errorf("sessionIDFromPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestLocate(t *testing.T) {
	home := t.TempDir()
	live := session.KimiWirePath(filepath.Join(home, "sessions", "wd_a", "session_a"))
	stale := session.KimiWirePath(filepath.Join(home, "sessions", "wd_b", "session_a"))
	other := session.KimiWirePath(filepath.Join(home, "sessions", "wd_a", "session_b"))
	sub := filepath.Join(home, "sessions", "wd_a", "session_c", "agents", "agent-0", "wire.jsonl")
	for _, path := range []string{live, stale, other, sub} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	source := &Source{Home: home}
	tests := []struct {
		name string
		meta session.Meta
		want []string
	}{
		{"path from discovery", session.Meta{Provider: "kimi", ID: "session_a", RolloutPath: stale}, []string{stale}},
		{"search picks newest bucket", session.Meta{Provider: "kimi", ID: "session_a"}, []string{live}},
		{"stale path falls back to search", session.Meta{ID: "session_b", RolloutPath: filepath.Join(home, "gone", "session_b", "agents", "main", "wire.jsonl")}, []string{other}},
		{"path for another id is ignored", session.Meta{ID: "session_b", RolloutPath: live}, []string{other}},
		{"subagent logs are not transcripts", session.Meta{ID: "session_c"}, nil},
		{"no transcript yet", session.Meta{ID: "session_d"}, nil},
		{"glob characters", session.Meta{ID: "session_*"}, nil},
		{"path traversal", session.Meta{ID: ".."}, nil},
		{"no id", session.Meta{Provider: "kimi"}, nil},
		{"other provider", session.Meta{Provider: "codex", ID: "session_a"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := source.Locate(tt.meta)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("Locate = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLocateHonorsKimiCodeHome(t *testing.T) {
	home := t.TempDir()
	path := session.KimiWirePath(filepath.Join(home, "sessions", "wd_a", "session_a"))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KIMI_CODE_HOME", home)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir()) // the home directory on Windows
	got, err := (&Source{}).Locate(session.Meta{ID: "session_a"})
	if err != nil || len(got) != 1 || got[0] != path {
		t.Fatalf("Locate = %v, %v", got, err)
	}
}

func TestRegistered(t *testing.T) {
	if _, ok := reader.For(transcript.ProviderKimi); !ok {
		t.Fatal("kimi source not registered")
	}
	if _, ok := reader.EnumeratorFor(transcript.ProviderKimi); !ok {
		t.Fatal("kimi source cannot be enumerated")
	}
}
