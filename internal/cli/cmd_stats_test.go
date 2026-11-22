package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/session"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
	"github.com/firekeeper-ai/firekeeper/internal/transcript/reader"
)

type statsTestEnumerator struct {
	metas []session.Meta
}

func (e statsTestEnumerator) Enumerate(context.Context, reader.EnumerateOptions) ([]session.Meta, error) {
	return e.metas, nil
}

type statsTestSource struct {
	events []transcript.Event
}

func (s statsTestSource) Locate(session.Meta) ([]string, error) {
	return []string{"fixture"}, nil
}

func (s statsTestSource) Read(string, int64) ([]transcript.Event, int64, error) {
	return s.events, int64(len(s.events)), nil
}

func TestRunStatsJSONAndSelection(t *testing.T) {
	oldHome, oldEnumerate, oldSource := configHome, statsEnumerate, statsSource
	oldSkip, oldNow := statsSkipReason, statsNow
	configHome = func() (string, error) { return t.TempDir(), nil }
	statsNow = func() time.Time { return time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC) }
	statsSkipReason = func(session.Meta, []string) string { return "" }
	meta := session.Meta{ID: "synthetic-session", Provider: transcript.ProviderCodex, Project: "demo"}
	statsEnumerate = func(provider transcript.Provider) (reader.Enumerator, bool) {
		if provider != transcript.ProviderCodex {
			return nil, false
		}
		return statsTestEnumerator{metas: []session.Meta{
			meta,
			{ID: "different-project", Provider: transcript.ProviderCodex, Project: "other"},
		}}, true
	}
	now := statsNow()
	promptAt, replyAt := now.Add(-time.Hour), now.Add(-59*time.Minute)
	model := "synthetic-model"
	statsSource = func(provider transcript.Provider) (reader.Source, bool) {
		if provider != transcript.ProviderCodex {
			return nil, false
		}
		return statsTestSource{events: []transcript.Event{
			{Provider: provider, Role: transcript.RoleUser, Text: "never output this prompt", TS: &promptAt},
			{Provider: provider, Role: transcript.RoleAssistant, Text: "private reply", Model: &model, TS: &replyAt, Tokens: transcript.Tokens{Input: 12, Output: 3}},
		}}, true
	}
	t.Cleanup(func() {
		configHome, statsEnumerate, statsSource = oldHome, oldEnumerate, oldSource
		statsSkipReason, statsNow = oldSkip, oldNow
	})

	var stdout, stderr bytes.Buffer
	if code := runStats([]string{"--provider", "codex", "--project", "demo", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runStats code = %d, stderr = %q", code, stderr.String())
	}
	var report struct {
		SessionsScanned int   `json:"sessions_scanned"`
		Findings        []any `json:"findings"`
		Spend           []struct {
			Tokens int64 `json:"tokens"`
		} `json:"spend"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if report.SessionsScanned != 1 || len(report.Spend) != 1 || report.Spend[0].Tokens != 15 {
		t.Fatalf("stats result = %+v", report)
	}
	if strings.Contains(stdout.String(), "never output this prompt") ||
		strings.Contains(stdout.String(), "private reply") {
		t.Fatal("stats output contains transcript text")
	}

	statsEnumerate = func(provider transcript.Provider) (reader.Enumerator, bool) {
		if provider == transcript.ProviderCodex {
			return statsTestEnumerator{}, true
		}
		return nil, false
	}
	stdout.Reset()
	stderr.Reset()
	if code := runStats([]string{"--provider", "codex", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("empty runStats code = %d, stderr = %q", code, stderr.String())
	}
	var empty struct {
		SessionsScanned int   `json:"sessions_scanned"`
		Findings        []any `json:"findings"`
		Spend           []any `json:"spend"`
		Rhythm          []any `json:"rhythm"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &empty); err != nil {
		t.Fatalf("decode empty stats: %v", err)
	}
	if empty.SessionsScanned != 0 || empty.Findings == nil || empty.Spend == nil || empty.Rhythm == nil ||
		len(empty.Findings)+len(empty.Spend)+len(empty.Rhythm) != 0 {
		t.Fatalf("empty stats result = %+v", empty)
	}
}

func TestRunStatsEmptyAndRequiredFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runStats(nil, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "--json is required") {
		t.Fatalf("missing JSON code=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runStats([]string{"--provider", "codex", "--all", "--json"}, &stdout, &stderr); code != 2 {
		t.Fatalf("conflicting selectors code=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runStats([]string{"--since", "0d", "--json"}, &stdout, &stderr); code != 2 {
		t.Fatalf("zero duration code=%d stderr=%q", code, stderr.String())
	}
}

func TestParseStatsDuration(t *testing.T) {
	for _, test := range []struct {
		input string
		want  time.Duration
	}{
		{input: "7d", want: 7 * 24 * time.Hour},
		{input: "168h", want: 7 * 24 * time.Hour},
	} {
		got, err := parseStatsDuration(test.input)
		if err != nil || got != test.want {
			t.Errorf("parseStatsDuration(%q) = %s, %v; want %s", test.input, got, err, test.want)
		}
	}
	if _, err := parseStatsDuration("1.5d"); err == nil {
		t.Error("fractional day should be rejected")
	}
}

func FuzzParseStatsDuration(f *testing.F) {
	for _, seed := range []string{"7d", "168h", "0d", "-1d", "not-a-duration", "999999999999999999999d"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		duration, err := parseStatsDuration(value)
		if err == nil && duration <= 0 {
			t.Fatalf("accepted non-positive duration %s", duration)
		}
	})
}
