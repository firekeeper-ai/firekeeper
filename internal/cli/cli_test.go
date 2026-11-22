package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/config"
	"github.com/firekeeper-ai/firekeeper/internal/reporter"
	"github.com/firekeeper-ai/firekeeper/internal/session"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
	"github.com/firekeeper-ai/firekeeper/internal/transcript/reader"
	"github.com/firekeeper-ai/firekeeper/internal/version"
)

func TestRunSubcommandDispatch(t *testing.T) {
	stubDiscover(t, nil, nil)
	tests := []struct {
		args       []string
		handled    bool
		code       int
		wantStderr string
	}{
		{args: nil, handled: false},
		{args: []string{"--unknown"}, handled: false},
		{args: []string{"--unknown", "snapshot"}, handled: false},
		{args: []string{"unknown"}, handled: false},
		{args: []string{"export"}, handled: true, code: 2, wantStderr: "firekeeper export: --session is required"},
		{args: []string{"report", "--bogus"}, handled: true, code: 2},
		{args: []string{"serve"}, handled: true, code: 2, wantStderr: "firekeeper-server"},
		{args: []string{"serve", "--listen", "127.0.0.1:0"}, handled: true, code: 2, wantStderr: "separate commercial binary"},
		{args: []string{"daemon"}, handled: true, code: 2, wantStderr: "firekeeper daemon: no --provider given"},
		{args: []string{"snapshot"}, handled: true, code: 2, wantStderr: "only --json"},
		{args: []string{"snapshot", "--bogus"}, handled: true, code: 2},
		{args: []string{"snapshot", "--json", "extra"}, handled: true, code: 2, wantStderr: "unexpected argument"},
		{args: []string{"snapshot", "--json"}, handled: true, code: 0},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code, handled := RunSubcommand(tt.args, &stdout, &stderr)
			if handled != tt.handled || code != tt.code {
				t.Fatalf("RunSubcommand = (%d, %v), want (%d, %v)", code, handled, tt.code, tt.handled)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tt.wantStderr)
			}
			if !handled && (stdout.Len() > 0 || stderr.Len() > 0) {
				t.Fatalf("unhandled args wrote output")
			}
		})
	}
}

func TestSnapshotJSON(t *testing.T) {
	stubDiscover(t, []session.Meta{{ID: "s1", Provider: "codex", Project: "demo", State: session.SessionStateNeedsInput}}, &session.Warning{Message: "Copilot: unavailable"})
	var stdout, stderr bytes.Buffer
	if code := runSnapshot([]string{"--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var got []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(got) != 1 || got[0]["session_id"] != "s1" || got[0]["state"] != "NEEDS_INPUT" || got[0]["provider"] != "codex" {
		t.Fatalf("output = %v", got)
	}
	if !strings.Contains(stderr.String(), "warning: Copilot: unavailable") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestSnapshotJSONEmptyAndFatal(t *testing.T) {
	stubDiscover(t, nil, nil)
	var stdout, stderr bytes.Buffer
	if code := runSnapshot([]string{"--json"}, &stdout, &stderr); code != 0 || strings.TrimSpace(stdout.String()) != "[]" {
		t.Fatalf("empty snapshot = %d %q", code, stdout.String())
	}

	stubDiscover(t, nil, errors.New("run ps: failed"))
	stdout.Reset()
	stderr.Reset()
	if code := runSnapshot([]string{"--json"}, &stdout, &stderr); code != 1 || stdout.Len() != 0 {
		t.Fatalf("fatal snapshot = %d %q", code, stdout.String())
	}
	if !strings.Contains(stderr.String(), "run ps: failed") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestUsageListsSubcommands(t *testing.T) {
	fs := flag.NewFlagSet("firekeeper", flag.ContinueOnError)
	var out bytes.Buffer
	fs.SetOutput(&out)
	PrintUsage(fs)
	for _, want := range []string{"snapshot", "export", "report", "backfill", "serve", "daemon", "otel-listen", "otel-reconcile", "telemetry"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("usage missing %q:\n%s", want, out.String())
		}
	}
}

func stubDiscover(t *testing.T, metas []session.Meta, err error) {
	t.Helper()
	previous := discoverSessions
	discoverSessions = func(context.Context, session.Options) ([]session.Meta, error) { return metas, err }
	t.Cleanup(func() { discoverSessions = previous })
}

type exportStubEnumerator struct {
	meta session.Meta
}

func (e exportStubEnumerator) Enumerate(context.Context, reader.EnumerateOptions) ([]session.Meta, error) {
	return []session.Meta{e.meta}, nil
}

type exportStubSource struct {
	events []transcript.Event
}

func (s exportStubSource) Locate(session.Meta) ([]string, error) {
	return []string{"synthetic.jsonl"}, nil
}

func (s exportStubSource) Read(string, int64) ([]transcript.Event, int64, error) {
	return s.events, int64(len(s.events)), nil
}

func stubExport(t *testing.T, provider transcript.Provider, meta session.Meta, events []transcript.Event) {
	t.Helper()
	previousEnumerate := exportEnumerate
	previousSource := exportSource
	previousMachineID := exportMachineID
	previousSkipReason := exportSkipReason
	previousHome := configHome
	exportEnumerate = func(got transcript.Provider) (reader.Enumerator, bool) {
		if got != provider {
			return nil, false
		}
		return exportStubEnumerator{meta: meta}, true
	}
	exportSource = func(got transcript.Provider) (reader.Source, bool) {
		if got != provider {
			return nil, false
		}
		return exportStubSource{events: events}, true
	}
	exportMachineID = func(string) (string, error) {
		return "0199c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d", nil
	}
	exportSkipReason = func(session.Meta, []string) string { return "" }
	configHome = func() (string, error) { return t.TempDir(), nil }
	t.Cleanup(func() {
		exportEnumerate = previousEnumerate
		exportSource = previousSource
		exportMachineID = previousMachineID
		exportSkipReason = previousSkipReason
		configHome = previousHome
	})
}

func TestExportNoTextEveryProvider(t *testing.T) {
	for _, provider := range transcript.Providers() {
		t.Run(string(provider), func(t *testing.T) {
			raw := json.RawMessage(`{"synthetic":true}`)
			text := "private prompt"
			tool := "shell"
			model := "test-model"
			ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			meta := session.Meta{ID: "session-1", Provider: provider, CWD: t.TempDir()}
			event := transcript.Event{
				Provider: provider,
				Seq:      7,
				TS:       &ts,
				Role:     transcript.RoleAssistant,
				Text:     text,
				ToolName: &tool,
				Model:    &model,
				Tokens:   transcript.Tokens{Input: 11, Output: 13, Cache: 17},
				Raw:      raw,
			}
			stubExport(t, provider, meta, []transcript.Event{event})

			var stdout, stderr bytes.Buffer
			code := runExport([]string{"--session", meta.ID, "--provider", string(provider), "--no-text"}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("code = %d, stderr = %q", code, stderr.String())
			}
			var got map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got); err != nil {
				t.Fatalf("decode JSONL: %v", err)
			}
			if got["seq"] != float64(7) || got["role"] != "assistant" || got["provider"] != string(provider) {
				t.Fatalf("structure = %v", got)
			}
			if _, ok := got["text"]; ok {
				t.Fatal("no-text output contains text")
			}
			if _, ok := got["raw"]; ok {
				t.Fatal("no-text output contains raw")
			}
		})
	}
}

func TestExportMissingAndIgnored(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		stubExport(t, transcript.ProviderCodex, session.Meta{ID: "other", Provider: transcript.ProviderCodex}, nil)
		var stdout, stderr bytes.Buffer
		code := runExport([]string{"--session", "missing", "--provider", "codex"}, &stdout, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), `session "missing" not found`) {
			t.Fatalf("code = %d stderr = %q", code, stderr.String())
		}
	})

	t.Run("ignored", func(t *testing.T) {
		meta := session.Meta{ID: "ignored", Provider: transcript.ProviderClaude, CWD: "/private/repo"}
		stubExport(t, transcript.ProviderClaude, meta, nil)
		exportSkipReason = func(got session.Meta, _ []string) string {
			if got.ID == meta.ID {
				return ".firekeeper-ignore"
			}
			return ""
		}
		var stdout, stderr bytes.Buffer
		code := runExport([]string{"--session", meta.ID, "--provider", "claude", "--no-text"}, &stdout, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "excluded: .firekeeper-ignore") {
			t.Fatalf("code = %d stderr = %q", code, stderr.String())
		}
	})
}

func TestVersionCommand(t *testing.T) {
	for _, arg := range []string{"version", "--version"} {
		var out, errb bytes.Buffer
		code, ok := RunSubcommand([]string{arg}, &out, &errb)
		if !ok || code != 0 {
			t.Fatalf("%s: ok=%v code=%d stderr=%q", arg, ok, code, errb.String())
		}
		if got := out.String(); !strings.HasPrefix(got, "firekeeper dev (commit dev, built dev") {
			t.Errorf("%s: output %q", arg, got)
		}
	}
	var out, errb bytes.Buffer
	if code, _ := RunSubcommand([]string{"version", "x"}, &out, &errb); code != 2 {
		t.Errorf("extra arg code = %d, want 2", code)
	}
}

func TestApplyReporterConfigSendsVersion(t *testing.T) {
	old := version.Version
	version.Version = "v1.2.3"
	defer func() { version.Version = old }()
	var cfg reporter.Config
	applyReporterConfig(&cfg, config.Config{})
	if cfg.Version != "v1.2.3" {
		t.Errorf("Version = %q", cfg.Version)
	}
}
