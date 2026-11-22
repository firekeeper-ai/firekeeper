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
			transcript.Event{Provider: transcript.ProviderCodex, Role: transcript.RoleToolResult, Raw: raw, TS: &replyAt},
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
