package turns

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/transcript"
)

func TestParseSyntheticTurnsForEveryProvider(t *testing.T) {
	start := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	ts := func(offset time.Duration) *time.Time {
		value := start.Add(offset)
		return &value
	}
	model := "synthetic-model"
	tool := "shell"
	prompt := "private prompt 🧪"
	for _, provider := range transcript.Providers() {
		t.Run(string(provider), func(t *testing.T) {
			events := []transcript.Event{
				{Provider: provider, Role: transcript.RoleToolCall, ToolName: &tool},
				{Provider: provider, Role: transcript.RoleUser, Text: prompt, TS: ts(0)},
				{Provider: provider, Role: transcript.RoleAssistant, TS: ts(5 * time.Second), Model: &model, Tokens: transcript.Tokens{Input: 100, Output: 20, CacheRead: 30, CacheWrite: 5}},
				{Provider: provider, Role: transcript.RoleToolCall, TS: ts(10 * time.Second), ToolName: &tool},
				{Provider: provider, Role: transcript.RoleToolResult, TS: ts(15 * time.Second), Raw: json.RawMessage(`{"result":{"error":"synthetic failure"}}`)},
				// Unknown records become meta; they are part of the current turn.
				{Provider: provider, Role: transcript.RoleMeta, TS: ts(20 * time.Second), Tokens: transcript.Tokens{Input: 10}, Raw: json.RawMessage(`{"type":"future_record"}`)},
				// A malformed meta event must not split or drop a turn.
				{Provider: provider, Role: transcript.RoleMeta, TS: ts(25 * time.Second), Raw: json.RawMessage(`"not a provider record"`)},
				{Provider: provider, Role: transcript.RoleUser, Text: "another private prompt", TS: ts(30 * time.Second)},
				{Provider: provider, Role: transcript.RoleAssistant, TS: ts(35 * time.Second), Model: stringPointer("next-model"), Tokens: transcript.Tokens{Input: 40, Output: 7, CacheRead: 8}},
			}
			got := Parse(events)
			if len(got) != 2 {
				t.Fatalf("got %d turns, want 2: %+v", len(got), got)
			}
			first := got[0]
			if first.Index != 0 || first.PromptChars != len([]rune(prompt)) {
				t.Fatalf("first prompt metrics = index %d, chars %d", first.Index, first.PromptChars)
			}
			if first.StartedAt == nil || !first.StartedAt.Equal(start) ||
				first.EndedAt == nil || !first.EndedAt.Equal(start.Add(25*time.Second)) ||
				first.DurationSeconds == nil || *first.DurationSeconds != 25 {
				t.Fatalf("first boundaries = %+v", first)
			}
			if first.Tokens != (Tokens{Input: 110, Output: 20, CacheRead: 30, CacheWrite: 5}) ||
				first.ContextTokens != 145 || first.ToolCalls != 1 || first.ToolErrors != 1 ||
				first.Tools["shell"] != 1 || first.Model != model {
				t.Fatalf("first metrics = %+v", first)
			}
			if first.Interrupted {
				t.Fatal("ordinary tool failure marked interrupted")
			}
			second := got[1]
			if second.Index != 1 || second.PromptChars != len([]rune("another private prompt")) ||
				second.ContextTokens != 193 || second.Model != "next-model" {
				t.Fatalf("second metrics = %+v", second)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{prompt, "another private prompt", "synthetic failure"} {
				if strings.Contains(string(encoded), secret) {
					t.Fatalf("JSON contains private transcript text %q", secret)
				}
			}
		})
	}
}

func TestParseInterruptionAndPartialTurn(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	got := Parse([]transcript.Event{
		{Role: transcript.RoleUser, Text: "first", TS: &now},
		{Role: transcript.RoleMeta, Raw: json.RawMessage(`{"toolUseResult":{"interrupted":true}}`)},
		{Role: transcript.RoleUser, Text: "Request interrupted by user for tool use", TS: &now},
	})
	if len(got) != 2 || !got[0].Interrupted || !got[1].Interrupted {
		t.Fatalf("interrupted turns = %+v", got)
	}
	if got[0].EndedAt == nil || !got[0].EndedAt.Equal(now) ||
		got[0].DurationSeconds == nil || *got[0].DurationSeconds != 0 {
		t.Fatalf("partial turn should end at its last observed event: %+v", got[0])
	}
}

func FuzzParseNeverSerializesTranscriptText(f *testing.F) {
	f.Add("PROMPT_SECRET", `{"is_error":true}`, "tool_result")
	f.Add("日本語🧪", "not-json", "meta")
	f.Fuzz(func(t *testing.T, prompt, raw, role string) {
		now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
		got := Parse([]transcript.Event{
			{Role: transcript.RoleUser, Text: "PROMPT_SECRET" + prompt, TS: &now},
			{Role: transcript.Role(role), Raw: json.RawMessage(raw)},
		})
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "PROMPT_SECRET") {
			t.Fatal("serialized turn metrics contain prompt text")
		}
	})
}

func stringPointer(value string) *string { return &value }
