package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/claudelimits"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
	"github.com/firekeeper-ai/firekeeper/internal/window"
)

func stubWindow(t *testing.T) {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // never read real transcripts
	prev := windowLimits
	windowLimits = func(context.Context, window.Period) (*claudelimits.Window, error) { return nil, nil }
	prevCodex := windowCodexLimits
	windowCodexLimits = func(context.Context, window.Period) (*claudelimits.Window, error) { return nil, nil }
	t.Cleanup(func() {
		windowLimits = prev
		windowCodexLimits = prevCodex
	})
}

func TestWindowRejectsBadInput(t *testing.T) {
	stubWindow(t)
	for _, test := range []struct {
		args   []string
		stderr string
	}{
		{[]string{"--provider", "nope"}, `unknown provider "nope"`},
		{[]string{"--provider", ""}, "no provider given"},
		{[]string{"--json", "--html", "x.html"}, "not both"},
		{[]string{"extra"}, "unexpected argument"},
		{[]string{"--bogus"}, "not defined"},
		{[]string{"--since", "nope"}, "invalid duration"},
		{[]string{"--since", "-1h"}, "duration must be positive"},
		{[]string{"--since", "1h", "--from", "2026-01-01T00:00:00Z"}, "use --since or"},
		{[]string{"--from", "bad"}, "invalid --from"},
		{[]string{"--to", "bad"}, "invalid --to"},
		{[]string{"--from", "2026-11-02T00:00:00Z", "--to", "2026-11-01T00:00:00Z"}, "from must be before"},
	} {
		var out, errb bytes.Buffer
		if code := runWindow(test.args, &out, &errb); code != 2 || !strings.Contains(errb.String(), test.stderr) {
			t.Errorf("%v: code %d, stderr %q; want 2 and %q", test.args, code, errb.String(), test.stderr)
		}
	}
}

// --provider accepts a name in any case and with surrounding spaces, like
// every other command's --provider, and reports the canonical id.
func TestWindowProviderIgnoresCaseAndSpaces(t *testing.T) {
	stubWindow(t)
	for _, name := range []string{"Claude", " claude ", "CLAUDE"} {
		var out, errb bytes.Buffer
		if code := runWindow([]string{"--provider", name, "--json"}, &out, &errb); code != 0 {
			t.Fatalf("--provider %q: code %d: %s", name, code, errb.String())
		}
		var got struct {
			Provider string `json:"provider"`
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Provider != "claude" {
			t.Fatalf("--provider %q: provider %q, %v; want claude", name, got.Provider, err)
		}
	}
}

func TestWindowJSONAndHTML(t *testing.T) {
	stubWindow(t)
	var out, errb bytes.Buffer
	if code := runWindow([]string{"--json"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	var got struct {
		Provider string            `json:"provider"`
		Sessions []json.RawMessage `json:"sessions"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Provider != "claude" || got.Sessions == nil {
		t.Fatalf("bad JSON %v: %s", err, out.String())
	}

	path := filepath.Join(t.TempDir(), "w.html")
	out.Reset()
	if code := runWindow([]string{"--html", path}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	u, err := url.Parse(strings.TrimSpace(out.String()))
	if err != nil || u.Scheme != "file" {
		t.Fatalf("url = %q", out.String())
	}
	if page, err := os.ReadFile(path); err != nil || !strings.Contains(string(page), "5-hour window") {
		t.Fatalf("page: %v", err)
	}
}

func TestWindowWeekUsesWeeklyLimit(t *testing.T) {
	stubWindow(t)
	var asked window.Period
	windowLimits = func(_ context.Context, p window.Period) (*claudelimits.Window, error) {
		asked = p
		return &claudelimits.Window{UsedPercent: 91, ResetsAt: windowNow().Add(48 * time.Hour)}, nil
	}
	var out, errb bytes.Buffer
	if code := runWindow([]string{"--week", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	var got struct {
		Period      string   `json:"period"`
		Basis       string   `json:"basis"`
		UsedPercent *float64 `json:"used_percent"`
		Daily       []any    `json:"daily"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if asked != window.PeriodWeek || got.Period != "week" || got.Basis != "provider_reset" ||
		got.UsedPercent == nil || *got.UsedPercent != 91 || len(got.Daily) != 7 {
		t.Fatalf("asked %q, got %+v", asked, got)
	}
}

func TestWindowAllAndDateRanges(t *testing.T) {
	stubWindow(t)
	var out, errb bytes.Buffer
	if code := runWindow([]string{"--all", "--since", "24h", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	var got []struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("all returned no provider reports")
	}

	out.Reset()
	errb.Reset()
	from := "2026-11-01T00:00:00Z"
	to := "2026-11-02T00:00:00Z"
	if code := runWindow([]string{"--from", from, "--to", to, "--json"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	var report struct {
		Start string `json:"window_start"`
		End   string `json:"window_end"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Start[:20] != from[:20] || report.End[:20] != to[:20] {
		t.Fatalf("range = %q - %q", report.Start, report.End)
	}
}

func TestWindowCodexUsesQuotaReset(t *testing.T) {
	stubWindow(t)
	windowCodexLimits = func(_ context.Context, p window.Period) (*claudelimits.Window, error) {
		if p != window.PeriodFiveHour {
			t.Fatalf("period = %q", p)
		}
		return &claudelimits.Window{UsedPercent: 73, ResetsAt: windowNow().Add(90 * time.Minute)}, nil
	}
	var out, errb bytes.Buffer
	if code := runWindow([]string{"--provider", "codex", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	var got struct {
		Basis       string   `json:"basis"`
		UsedPercent *float64 `json:"used_percent"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Basis != "provider_reset" || got.UsedPercent == nil || *got.UsedPercent != 73 {
		t.Fatalf("got %+v", got)
	}
}

// --all gives every provider its own quota: Claude's limits never appear on
// Codex or Copilot, and a provider without a limits source has none.
func TestWindowAllPassesEachProviderItsOwnLimits(t *testing.T) {
	stubWindow(t)
	claudeReset := time.Date(2026, 11, 22, 2, 30, 0, 0, time.UTC)
	codexReset := time.Date(2026, 11, 22, 4, 0, 0, 0, time.UTC)
	windowLimits = func(context.Context, window.Period) (*claudelimits.Window, error) {
		return &claudelimits.Window{UsedPercent: 3, ResetsAt: claudeReset}, nil
	}
	windowCodexLimits = func(context.Context, window.Period) (*claudelimits.Window, error) {
		return &claudelimits.Window{UsedPercent: 14, ResetsAt: codexReset}, nil
	}
	got := map[transcript.Provider]window.Options{}
	prevBuild := windowBuild
	windowBuild = func(_ context.Context, _ time.Time, opts window.Options) (window.Report, error) {
		got[opts.Provider] = opts
		return window.Report{Provider: opts.Provider, UsedPercent: opts.UsedPercent}, nil
	}
	t.Cleanup(func() { windowBuild = prevBuild })

	var out, errb bytes.Buffer
	if code := runWindow([]string{"--all", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	for provider, want := range map[transcript.Provider]struct {
		used  float64
		reset time.Time
	}{
		transcript.ProviderClaude: {3, claudeReset},
		transcript.ProviderCodex:  {14, codexReset},
	} {
		opts := got[provider]
		if opts.UsedPercent == nil || *opts.UsedPercent != want.used || !opts.ResetsAt.Equal(want.reset) {
			t.Errorf("%s: used=%v reset=%v; want %v %v", provider, opts.UsedPercent, opts.ResetsAt, want.used, want.reset)
		}
	}
	for _, provider := range []transcript.Provider{transcript.ProviderCopilot, transcript.ProviderKimi, transcript.ProviderGemini} {
		opts, ok := got[provider]
		if !ok {
			t.Fatalf("%s was not built", provider)
		}
		if opts.UsedPercent != nil || !opts.ResetsAt.IsZero() {
			t.Errorf("%s inherited another provider's limits: used=%v reset=%v", provider, *opts.UsedPercent, opts.ResetsAt)
		}
	}
}

// A failed or missing limits source leaves the window rolling instead of
// failing the command.
func TestWindowCodexLimitsFailureFallsBackToRolling(t *testing.T) {
	stubWindow(t)
	windowCodexLimits = func(context.Context, window.Period) (*claudelimits.Window, error) {
		return nil, context.DeadlineExceeded
	}
	var got window.Options
	prevBuild := windowBuild
	windowBuild = func(_ context.Context, _ time.Time, opts window.Options) (window.Report, error) {
		got = opts
		return window.Report{Provider: opts.Provider}, nil
	}
	t.Cleanup(func() { windowBuild = prevBuild })
	var out, errb bytes.Buffer
	if code := runWindow([]string{"--provider", "codex", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	if got.UsedPercent != nil || !got.ResetsAt.IsZero() {
		t.Fatalf("failed limits produced used=%v reset=%v", got.UsedPercent, got.ResetsAt)
	}
}

// SetCodexLimits installs the Codex quota source window uses; without it the
// Codex window never had a provider figure.
func TestSetCodexLimitsFeedsCodexWindow(t *testing.T) {
	stubWindow(t)
	reset := time.Date(2026, 11, 22, 4, 0, 0, 0, time.UTC)
	SetCodexLimits(func(_ context.Context, period window.Period) (*claudelimits.Window, error) {
		if period != window.PeriodWeek {
			return nil, nil
		}
		return &claudelimits.Window{UsedPercent: 17, ResetsAt: reset}, nil
	})
	var got window.Options
	prevBuild := windowBuild
	windowBuild = func(_ context.Context, _ time.Time, opts window.Options) (window.Report, error) {
		got = opts
		return window.Report{Provider: opts.Provider}, nil
	}
	t.Cleanup(func() { windowBuild = prevBuild })
	var out, errb bytes.Buffer
	if code := runWindow([]string{"--provider", "codex", "--week", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	if got.UsedPercent == nil || *got.UsedPercent != 17 || !got.ResetsAt.Equal(reset) {
		t.Fatalf("codex week opts: used=%v reset=%v", got.UsedPercent, got.ResetsAt)
	}
}
