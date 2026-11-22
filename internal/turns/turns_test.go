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
		for _, turn := range got {
			sum := 0
			for _, n := range turn.ToolErrorsByTool {
				sum += n
			}
			if sum != turn.ToolErrors || turn.PeakContextTokens < 0 {
				t.Fatalf("errors %d, by tool %v, peak %d", turn.ToolErrors, turn.ToolErrorsByTool, turn.PeakContextTokens)
			}
		}
	})
}

func stringPointer(value string) *string { return &value }

// PeakContextTokens is the largest single event's prompt (input plus cache),
// the context size the model saw. ContextTokens instead sums every call, so
// a turn of many tool calls over a small context reported far more than any
// context window holds.
func TestPeakContextTokens(t *testing.T) {
	call := func(input, read, write, cache int64) transcript.Event {
		return transcript.Event{Role: transcript.RoleAssistant, Tokens: transcript.Tokens{Input: input, Cache: cache, CacheRead: read, CacheWrite: write}}
	}
	events := []transcript.Event{
		{Role: transcript.RoleUser, Text: "first"},
		call(100, 20_000, 0, 20_000),
		call(50, 20_100, 300, 20_400),
		call(10, 20_400, 0, 20_400),
		{Role: transcript.RoleUser, Text: "second"},
		// A provider that reports only the combined cache figure.
		{Role: transcript.RoleMeta, Tokens: transcript.Tokens{Input: 5, Cache: 30_000}},
		{Role: transcript.RoleUser, Text: "third, no usage"},
	}
	got := Parse(events)
	if len(got) != 3 {
		t.Fatalf("got %d turns", len(got))
	}
	for i, want := range []int64{20_450, 30_005, 0} {
		if got[i].PeakContextTokens != want {
			t.Errorf("turn %d peak = %d; want %d", i, got[i].PeakContextTokens, want)
		}
	}
	if got[0].ContextTokens <= got[0].PeakContextTokens {
		t.Errorf("running sum %d should exceed the peak %d for a multi-call turn", got[0].ContextTokens, got[0].PeakContextTokens)
	}
	data, err := json.Marshal(got[0])
	if err != nil || !strings.Contains(string(data), `"peak_context_tokens":20450`) {
		t.Fatalf("JSON = %s, %v", data, err)
	}
}

// Tool errors are counted per tool, so a high error rate can be traced to
// the tool that failed. A result without a name counts as "unknown".
func TestToolErrorsByTool(t *testing.T) {
	bash, edit := "Bash", "Edit"
	failed := json.RawMessage(`{"is_error":true}`)
	ok := json.RawMessage(`{"is_error":false}`)
	events := []transcript.Event{
		{Role: transcript.RoleUser},
		{Role: transcript.RoleToolCall, ToolName: &bash},
		{Role: transcript.RoleToolResult, ToolName: &bash, Raw: failed},
		{Role: transcript.RoleToolCall, ToolName: &bash},
		{Role: transcript.RoleToolResult, ToolName: &bash, Raw: failed},
		{Role: transcript.RoleToolCall, ToolName: &edit},
		{Role: transcript.RoleToolResult, ToolName: &edit, Raw: ok},
		{Role: transcript.RoleToolResult, Raw: failed},
		{Role: transcript.RoleToolResult, ToolName: &edit, Raw: json.RawMessage(`not json`)},
	}
	got := Parse(events)
	if len(got) != 1 {
		t.Fatalf("got %d turns", len(got))
	}
	turn := got[0]
	if turn.ToolErrors != 3 || turn.ToolErrorsByTool["Bash"] != 2 || turn.ToolErrorsByTool["unknown"] != 1 {
		t.Fatalf("errors=%d by tool=%v", turn.ToolErrors, turn.ToolErrorsByTool)
	}
	if _, listed := turn.ToolErrorsByTool["Edit"]; listed {
		t.Fatalf("a tool without errors is listed: %v", turn.ToolErrorsByTool)
	}
	clean := Parse([]transcript.Event{{Role: transcript.RoleUser}, {Role: transcript.RoleToolResult, ToolName: &edit, Raw: ok}})
	if data, _ := json.Marshal(clean[0]); strings.Contains(string(data), "tool_errors_by_tool") {
		t.Fatalf("turn without errors lists them: %s", data)
	}
}

func TestCacheTokensPrefersTheLargerFigure(t *testing.T) {
	for _, test := range []struct {
		tokens transcript.Tokens
		want   int64
	}{
		{transcript.Tokens{Cache: 10, CacheRead: 6, CacheWrite: 4}, 10},
		{transcript.Tokens{Cache: 10}, 10},
		{transcript.Tokens{CacheRead: 6, CacheWrite: 4}, 10},
		{transcript.Tokens{Cache: -3, CacheRead: -1}, 0},
		{transcript.Tokens{}, 0},
	} {
		if got := CacheTokens(test.tokens); got != test.want {
			t.Errorf("CacheTokens(%+v) = %d; want %d", test.tokens, got, test.want)
		}
	}
}
