package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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
		{args: []string{"unknown"}, handled: true, code: 2, wantStderr: `firekeeper: unknown command "unknown"`},
		{args: []string{"stast", "--json"}, handled: true, code: 2, wantStderr: `unknown command "stast"`},
		{args: []string{"export"}, handled: true, code: 2, wantStderr: "firekeeper export: --session is required"},
		{args: []string{"turns"}, handled: true, code: 2, wantStderr: "--json is required"},
		{args: []string{"stats"}, handled: true, code: 2, wantStderr: "--json is required"},
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
	for _, want := range []string{"snapshot", "export", "turns", "stats", "report", "backfill", "serve", "daemon", "otel-listen", "otel-reconcile", "telemetry"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("usage missing %q:\n%s", want, out.String())
		}
	}
}

// Help lists each command's flags with their descriptions and defaults, not
// only the one-line summary, so a script or agent can discover them.
func TestSubcommandHelpListsFlags(t *testing.T) {
	for _, test := range []struct {
		command string
		want    []string
	}{
		{"stats", []string{"print local transcript insights", "-since", "(default \"7d\")", "-project"}},
		{"window", []string{"-provider", "-week", "-from", "-to"}},
		{"export", []string{"-session", "-no-text"}},
		{"turns", []string{"-provider", "-json"}},
		{"report", []string{"-dry-run", "-since"}},
		{"backfill", []string{"-limit", "-yes"}},
	} {
		for _, help := range []string{"-h", "--help"} {
			var stdout, stderr bytes.Buffer
			code, handled := RunSubcommand([]string{test.command, help}, &stdout, &stderr)
			if !handled || code != 0 {
				t.Fatalf("%s %s: code=%d handled=%v stderr=%q", test.command, help, code, handled, stderr.String())
			}
			for _, want := range test.want {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("%s %s: help missing %q:\n%s", test.command, help, want, stdout.String())
				}
			}
			if strings.Contains(stdout.String(), "Usage of firekeeper") {
				t.Errorf("%s %s: help repeats the flag package header:\n%s", test.command, help, stdout.String())
			}
		}
	}
}

// Commands without a flag set still answer help with their summary and
// never print an error for it.
func TestSubcommandHelpWithoutFlags(t *testing.T) {
	for _, command := range []string{"telemetry", "claude-limits"} {
		var stdout, stderr bytes.Buffer
		code, handled := RunSubcommand([]string{command, "-h"}, &stdout, &stderr)
		if !handled || code != 0 || stderr.Len() != 0 {
			t.Fatalf("%s -h: code=%d handled=%v stderr=%q", command, code, handled, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Usage: firekeeper "+command) || strings.Contains(stdout.String(), "unknown command") {
			t.Fatalf("%s -h: stdout=%q", command, stdout.String())
		}
	}
}

func TestEverySubcommandHelpAndSchema(t *testing.T) {
	for _, cmd := range subcommands {
		for _, flag := range []string{"-h", "--help"} {
			t.Run(cmd.name+"/help/"+flag, func(t *testing.T) {
				var stdout, stderr bytes.Buffer
				code, handled := RunSubcommand([]string{cmd.name, flag}, &stdout, &stderr)
				if !handled || code != 0 {
					t.Fatalf("help returned code=%d handled=%v stderr=%q", code, handled, stderr.String())
				}
			})
		}
		t.Run(cmd.name+"/schema", func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code, handled := RunSubcommand([]string{cmd.name, "--schema"}, &stdout, &stderr)
			if !handled || code != 0 {
				t.Fatalf("schema returned code=%d handled=%v stderr=%q", code, handled, stderr.String())
			}
			var schema map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &schema); err != nil {
				t.Fatalf("invalid schema JSON: %v", err)
			}
			sample := map[string]any{"schema_version": schemaVersion}
			if properties, ok := schema["properties"].(map[string]any); ok {
				for key, raw := range properties {
					if property, ok := raw.(map[string]any); ok && property["type"] == "array" {
						sample[key] = []any{}
					}
				}
			}
			if err := validateOutputSchema(schema, sample); err != nil {
				t.Fatalf("schema rejects versioned object: %v", err)
			}
		})
	}
}

func TestJSONOutputMatchesCommandSchema(t *testing.T) {
	cases := []struct {
		command string
		args    []string
		input   string
	}{
		{"snapshot", []string{"--json"}, `[{"session_id":"s1"}]`},
		{"snapshot", []string{"--ndjson"}, `{"session_id":"s1"}`},
		{"recent", []string{"--json"}, `{"generated_at":"2026-01-01T00:00:00Z","sessions":[]}`},
		{"recent", []string{"--ndjson"}, `{"session_id":"s1"}`},
		{"turns", []string{"--json"}, `[]`},
		{"stats", []string{"--json"}, `{"findings":[]}`},
		{"window", []string{"--json"}, `[]`},
		{"export", []string{"--json"}, `{"seq":1}` + "\n" + `{"seq":2}`},
		{"export", []string{"--ndjson"}, `{"seq":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.command+"/"+strings.Join(tc.args, "_"), func(t *testing.T) {
			got, err := transformJSONOutput(tc.command, tc.args, []byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			var output any
			if hasFlag(tc.args, "--ndjson") && !(tc.command == "export" && hasFlag(tc.args, "--json")) {
				var rows []any
				for _, line := range bytes.Split(bytes.TrimSpace(got), []byte{'\n'}) {
					var row any
					if err := json.Unmarshal(line, &row); err != nil {
						t.Fatal(err)
					}
					if err := validateOutputSchema(schemaFor(tc.command, true), row); err != nil {
						t.Fatalf("row fails schema: %v", err)
					}
					rows = append(rows, row)
				}
				if len(rows) == 0 {
					t.Fatal("expected NDJSON row")
				}
				return
			}
			if err := json.Unmarshal(got, &output); err != nil {
				t.Fatal(err)
			}
			if err := validateOutputSchema(schemaFor(tc.command, false), output); err != nil {
				t.Fatalf("output fails schema: %v\n%s", err, got)
			}
		})
	}
}

func TestSnapshotCommandOutputValidatesAgainstSchema(t *testing.T) {
	stubDiscover(t, []session.Meta{{ID: "s1", Provider: "codex"}}, nil)
	var output, errOutput bytes.Buffer
	if code, handled := RunSubcommand([]string{"snapshot", "--json"}, &output, &errOutput); !handled || code != 0 {
		t.Fatalf("snapshot = %d handled=%v stderr=%q", code, handled, errOutput.String())
	}
	var result map[string]any
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	errOutput.Reset()
	var schema map[string]any
	if code, handled := RunSubcommand([]string{"snapshot", "--schema"}, &output, &errOutput); !handled || code != 0 {
		t.Fatalf("schema = %d handled=%v stderr=%q", code, handled, errOutput.String())
	}
	if err := json.Unmarshal(output.Bytes(), &schema); err != nil {
		t.Fatal(err)
	}
	if err := validateOutputSchema(schema, result); err != nil {
		t.Fatalf("snapshot output fails its schema: %v", err)
	}
	sessions, ok := result["sessions"].([]any)
	if !ok || len(sessions) != 1 || result["schema_version"] != schemaVersion {
		t.Fatalf("snapshot output = %#v", result)
	}
	row, ok := sessions[0].(map[string]any)
	if !ok || row["session_id"] != "s1" || row["provider"] != "codex" {
		t.Fatalf("snapshot record lost fields: %#v", sessions[0])
	}
}

func TestExportJSONEnvelopeValidatesAndPreservesEventFields(t *testing.T) {
	provider := transcript.ProviderCodex
	meta := session.Meta{ID: "session-1", Provider: provider}
	stubExport(t, provider, meta, []transcript.Event{{
		Provider: provider, Seq: 9, Role: transcript.RoleAssistant, Text: "safe",
	}})
	args := []string{"export", "--session", meta.ID, "--provider", string(provider), "--json"}
	var output, errOutput bytes.Buffer
	if code, handled := RunSubcommand(args, &output, &errOutput); !handled || code != 0 {
		t.Fatalf("export = %d handled=%v stderr=%q", code, handled, errOutput.String())
	}
	var result map[string]any
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if code, handled := RunSubcommand([]string{"export", "--json", "--schema"}, &output, &errOutput); !handled || code != 0 {
		t.Fatalf("schema = %d handled=%v stderr=%q", code, handled, errOutput.String())
	}
	var schema map[string]any
	if err := json.Unmarshal(output.Bytes(), &schema); err != nil {
		t.Fatal(err)
	}
	if err := validateOutputSchema(schema, result); err != nil {
		t.Fatalf("export output fails its schema: %v", err)
	}
	events, ok := result["events"].([]any)
	if !ok || len(events) != 1 || result["schema_version"] != schemaVersion {
		t.Fatalf("export output = %#v", result)
	}
	event, ok := events[0].(map[string]any)
	if !ok || event["seq"] != float64(9) || event["role"] != "assistant" || event["text"] != "safe" {
		t.Fatalf("export event fields = %#v", events[0])
	}
}

func validateOutputSchema(schema map[string]any, output any) error {
	object, ok := output.(map[string]any)
	if !ok {
		return fmt.Errorf("expected object, got %T", output)
	}
	var required []string
	switch values := schema["required"].(type) {
	case []string:
		required = values
	case []any:
		for _, value := range values {
			if key, ok := value.(string); ok {
				required = append(required, key)
			}
		}
	}
	for _, key := range required {
		if _, ok := object[key]; !ok {
			return fmt.Errorf("missing required property %q", key)
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	for key, property := range properties {
		value, exists := object[key]
		if !exists {
			continue
		}
		constraint, _ := property.(map[string]any)
		if expected, hasConst := constraint["const"]; hasConst && value != expected {
			return fmt.Errorf("%s must equal %v", key, expected)
		}
		if constraint["type"] == "array" {
			if _, ok := value.([]any); !ok {
				return fmt.Errorf("%s must be an array", key)
			}
		}
	}
	return nil
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

func TestTurnsJSONContainsMetricsOnly(t *testing.T) {
	provider := transcript.ProviderCodex
	meta := session.Meta{ID: "session-1", Provider: provider}
	prompt := "private prompt text"
	model := "test-model"
	tool := "shell"
	start := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Second)
	stubExport(t, provider, meta, []transcript.Event{
		{Provider: provider, Role: transcript.RoleUser, Text: prompt, TS: &start},
		{Provider: provider, Role: transcript.RoleToolCall, ToolName: &tool, TS: &end},
		{Provider: provider, Role: transcript.RoleToolResult, Raw: json.RawMessage(`{"result":{"error":"private tool output"}}`), TS: &end},
		{Provider: provider, Role: transcript.RoleAssistant, Model: &model, TS: &end, Tokens: transcript.Tokens{Input: 12, Output: 5, CacheRead: 4}},
	})
	var stdout, stderr bytes.Buffer
	if code := runTurns([]string{"session-1", "--provider", "codex", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var got []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(got) != 1 || got[0]["prompt_chars"] != float64(len(prompt)) ||
		got[0]["tool_calls"] != float64(1) || got[0]["tool_errors"] != float64(1) ||
		got[0]["context_tokens"] != float64(16) {
		t.Fatalf("turn output = %v", got)
	}
	if strings.Contains(stdout.String(), prompt) || strings.Contains(stdout.String(), "private tool output") {
		t.Fatal("turn output contains transcript text")
	}
	if code := runTurns([]string{"session-1"}, &bytes.Buffer{}, &stderr); code != 2 || !strings.Contains(stderr.String(), "--json is required") {
		t.Fatalf("missing --json code = %d stderr = %q", code, stderr.String())
	}
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
