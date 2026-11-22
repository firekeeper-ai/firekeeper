package stats

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/transcript"
)

func TestBuildSyntheticFindingsWithoutTranscriptText(t *testing.T) {
	start := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	model := "synthetic-model"
	tool := "shell"
	events := make([]transcript.Event, 0, 20)
	for i := range 5 {
		promptAt := start.Add(time.Duration(i*10) * time.Minute)
		replyAt := promptAt.Add(time.Minute)
		events = append(events,
			transcript.Event{Provider: transcript.ProviderCodex, Role: transcript.RoleUser, Text: "private prompt", TS: &promptAt},
			transcript.Event{Provider: transcript.ProviderCodex, Role: transcript.RoleToolCall, ToolName: &tool, TS: &replyAt},
		)
		raw := json.RawMessage(`{"is_error":true}`)
		if i >= 2 {
			raw = json.RawMessage(`{"is_error":false}`)
		}
		events = append(events,
			transcript.Event{Provider: transcript.ProviderCodex, Role: transcript.RoleToolResult, ToolName: &tool, Raw: raw, TS: &replyAt},
			transcript.Event{
				Provider: transcript.ProviderCodex, Role: transcript.RoleAssistant, Text: "private response",
				Model: &model, TS: &replyAt,
				Tokens: transcript.Tokens{Input: 250_000, Output: 10_000, CacheRead: 70_000},
			},
		)
	}
	report := Build([]Session{{
		ID: "synthetic-session", Provider: transcript.ProviderCodex,
		Project: "synthetic-project", Events: events,
	}}, start)

	if report.SessionsScanned != 1 || len(report.Findings) != 5 {
		t.Fatalf("report session count/findings = %d/%+v", report.SessionsScanned, report.Findings)
	}
	wantKinds := map[FindingKind]bool{
		FindingContextGrowth: false, FindingToolReliability: false,
		FindingAttention: false, FindingRhythm: false, FindingSpend: false,
	}
	for _, finding := range report.Findings {
		if _, ok := wantKinds[finding.Kind]; !ok {
			t.Fatalf("unexpected finding kind %q", finding.Kind)
		}
		wantKinds[finding.Kind] = true
		if finding.SessionID != "synthetic-session" || finding.Value < finding.Threshold {
			t.Fatalf("finding lacks stable identity/threshold data: %+v", finding)
		}
	}
	for kind, found := range wantKinds {
		if !found {
			t.Errorf("missing finding kind %q", kind)
		}
	}
	if len(report.Spend) != 1 || report.Spend[0].Tokens != 1_650_000 ||
		report.Spend[0].Project != "synthetic-project" || report.Spend[0].Model != model {
		t.Fatalf("spend totals = %+v", report.Spend)
	}
	if len(report.Rhythm) != 1 || report.Rhythm[0].Weekday != int(time.Friday) ||
		report.Rhythm[0].Hour != 12 || report.Rhythm[0].Turns != 5 {
		t.Fatalf("rhythm = %+v", report.Rhythm)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private prompt", "private response"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("stats output contains transcript text %q", secret)
		}
	}
}

func TestBuildEmptyAndOutOfRangeEvents(t *testing.T) {
	since := time.Date(2026, time.November, 21, 0, 0, 0, 0, time.UTC)
	old := since.Add(-time.Hour)
	report := Build([]Session{{
		ID: "old-session", Provider: transcript.ProviderClaude,
		Events: []transcript.Event{{Provider: transcript.ProviderClaude, Role: transcript.RoleUser, TS: &old}},
	}}, since)
	if report.SessionsScanned != 0 || len(report.Findings) != 0 ||
		report.Findings == nil || report.Spend == nil || report.Rhythm == nil {
		t.Fatalf("empty range report = %+v", report)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"findings":[]`) ||
		!strings.Contains(string(encoded), `"spend":[]`) ||
		!strings.Contains(string(encoded), `"rhythm":[]`) {
		t.Fatalf("empty arrays not stable in JSON: %s", encoded)
	}
}

func statsAt(minutes int) *time.Time {
	at := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC).Add(time.Duration(minutes) * time.Minute)
	return &at
}

func findingsOf(report Report, kind FindingKind) []Finding {
	var out []Finding
	for _, finding := range report.Findings {
		if finding.Kind == kind {
			out = append(out, finding)
		}
	}
	return out
}

// Context growth is judged by the context size the model saw. A turn of many
// tool calls over a small context used to be flagged, because the old metric
// summed cache tokens over every call.
func TestContextGrowthUsesPeakContext(t *testing.T) {
	since := *statsAt(-60)
	turn := func(minute int, context int64, calls int) []transcript.Event {
		events := []transcript.Event{{Role: transcript.RoleUser, TS: statsAt(minute)}}
		for range calls {
			events = append(events, transcript.Event{Role: transcript.RoleAssistant, TS: statsAt(minute),
				Tokens: transcript.Tokens{Input: 100, Cache: context, CacheRead: context}})
		}
		return events
	}
	var small, large, copilot []transcript.Event
	for i := range 3 {
		small = append(small, turn(i, 20_000, 10)...)
		large = append(large, turn(i, int64(100_000+i*60_000), 2)...)
	}
	// Copilot logs main-agent usage only as one run total.
	copilot = append(turn(0, 0, 0), transcript.Event{Role: transcript.RoleMeta, TS: statsAt(1), Tokens: transcript.Tokens{Input: 5, Cache: 9_000_000}})
	report := Build([]Session{
		{ID: "small", Provider: transcript.ProviderClaude, Events: small},
		{ID: "large", Provider: transcript.ProviderClaude, Events: large},
		{ID: "copilot", Provider: transcript.ProviderCopilot, Events: copilot},
	}, since)
	got := findingsOf(report, FindingContextGrowth)
	if len(got) != 1 || got[0].SessionID != "large" {
		t.Fatalf("context growth findings = %+v; want only the large session", got)
	}
	if got[0].Metric != "peak_context_tokens" || got[0].Value != 220_100 || got[0].Unit != "tokens" ||
		got[0].Threshold != ContextGrowthThresholdTokens {
		t.Fatalf("finding = %+v", got[0])
	}
}

// Tool error findings name the tool and how many calls the rate covers, so
// a 100% rate from one call is distinguishable from a real problem.
func TestToolReliabilityNamesToolAndSamples(t *testing.T) {
	since := *statsAt(-60)
	bash, edit, read := "Bash", "Edit", "Read"
	failed, ok := json.RawMessage(`{"is_error":true}`), json.RawMessage(`{"is_error":false}`)
	events := []transcript.Event{{Role: transcript.RoleUser, TS: statsAt(0)}}
	add := func(tool *string, results ...json.RawMessage) {
		for _, raw := range results {
			events = append(events,
				transcript.Event{Role: transcript.RoleToolCall, ToolName: tool, TS: statsAt(1)},
				transcript.Event{Role: transcript.RoleToolResult, ToolName: tool, Raw: raw, TS: statsAt(1)})
		}
	}
	add(&bash, failed, failed, failed, ok, ok, ok) // 3 of 6: flagged
	add(&edit, failed, ok, ok, ok, ok, ok, ok)     // 1 of 7: under the threshold
	add(&read, failed, failed)                     // 2 of 2: too few calls to judge
	report := Build([]Session{{ID: "s", Provider: transcript.ProviderClaude, Events: events}}, since)
	got := findingsOf(report, FindingToolReliability)
	if len(got) != 1 {
		t.Fatalf("tool findings = %+v; want Bash only", got)
	}
	if got[0].Tool != "Bash" || got[0].Samples != 6 || got[0].Value != 0.5 {
		t.Fatalf("finding = %+v", got[0])
	}
	encoded, _ := json.Marshal(got[0])
	if !strings.Contains(string(encoded), `"tool":"Bash"`) || !strings.Contains(string(encoded), `"samples":6`) {
		t.Fatalf("JSON = %s", encoded)
	}
	other, _ := json.Marshal(Finding{Kind: FindingSpend})
	if strings.Contains(string(other), `"tool"`) || strings.Contains(string(other), `"samples"`) {
		t.Fatalf("non-tool finding carries tool fields: %s", other)
	}
}

// Spend from events without a model is labeled "unknown", as window does,
// rather than an empty name.
func TestSpendLabelsMissingModelUnknown(t *testing.T) {
	model := "m1"
	report := Build([]Session{{ID: "s", Provider: transcript.ProviderCopilot, Project: "p", Events: []transcript.Event{
		{Role: transcript.RoleUser, TS: statsAt(0)},
		{Role: transcript.RoleMeta, TS: statsAt(1), Tokens: transcript.Tokens{Input: 10}},
		{Role: transcript.RoleAssistant, TS: statsAt(1), Model: &model, Tokens: transcript.Tokens{Input: 5}},
		{Role: transcript.RoleMeta, TS: statsAt(2), Model: new(string), Tokens: transcript.Tokens{Input: 1}},
	}}}, *statsAt(-60))
	got := map[string]int64{}
	for _, spend := range report.Spend {
		got[spend.Model] = spend.Tokens
	}
	if len(got) != 2 || got["unknown"] != 11 || got["m1"] != 5 {
		t.Fatalf("spend = %+v", report.Spend)
	}
}

// The rhythm buckets say which timezone their hours are in.
func TestRhythmStatesTimezone(t *testing.T) {
	encoded, err := json.Marshal(Build(nil, *statsAt(0)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"rhythm_timezone":"UTC"`) {
		t.Fatalf("JSON = %s", encoded)
	}
}
