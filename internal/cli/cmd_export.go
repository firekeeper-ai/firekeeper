package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/config"
	"github.com/firekeeper-ai/firekeeper/internal/reporter"
	"github.com/firekeeper-ai/firekeeper/internal/session"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
	"github.com/firekeeper-ai/firekeeper/internal/transcript/reader"
)

var (
	exportEnumerate  = reader.EnumeratorFor
	exportSource     = reader.For
	exportMachineID  = session.MachineID
	exportSkipReason = reporter.SkipReason
)

type exportStructureOnly struct {
	SchemaVersion string              `json:"schema_version"`
	MachineID     string              `json:"machine_id"`
	SessionID     string              `json:"session_id"`
	Provider      transcript.Provider `json:"provider"`
	Seq           int64               `json:"seq"`
	TS            *time.Time          `json:"ts"`
	Role          transcript.Role     `json:"role"`
	ToolName      *string             `json:"tool_name"`
	Model         *string             `json:"model"`
	Tokens        transcript.Tokens   `json:"tokens"`
}

type exportVersionedEvent struct {
	SchemaVersion string `json:"schema_version"`
	transcript.Event
}

func runExport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("firekeeper export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sessionID := fs.String("session", "", "provider-native session id")
	providerName := fs.String("provider", "", "provider to search (codex, claude, copilot, kimi, opencode, gemini, cursor)")
	format := fs.String("format", "jsonl", "output format (jsonl)")
	noText := fs.Bool("no-text", false, "omit text and raw fields")
	since := fs.Duration("since", 0, "only search transcripts modified within this duration")
	asJSON := fs.Bool("json", false, "print events as a JSON array")
	ndjson := fs.Bool("ndjson", false, "print one event per JSON line (default)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "firekeeper export: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *asJSON && *ndjson {
		fmt.Fprintln(stderr, "firekeeper export: use --json or --ndjson, not both")
		return 2
	}
	if strings.TrimSpace(*sessionID) == "" {
		fmt.Fprintln(stderr, "firekeeper export: --session is required")
		return 2
	}
	if *format != "jsonl" {
		fmt.Fprintf(stderr, "firekeeper export: unsupported --format %q (want jsonl)\n", *format)
		return 2
	}
	if *since < 0 {
		fmt.Fprintln(stderr, "firekeeper export: --since must not be negative")
		return 2
	}

	var providers []transcript.Provider
	if strings.TrimSpace(*providerName) != "" {
		provider, err := config.ParseProvider(*providerName)
		if err != nil {
			fmt.Fprintf(stderr, "firekeeper export: %v\n", err)
			return 2
		}
		providers = []transcript.Provider{provider}
	} else {
		providers = reader.Registered()
	}

	c, err := loadConfig(configFlags{})
	if err != nil {
		fmt.Fprintf(stderr, "firekeeper export: %v\n", err)
		return 2
	}
	home, err := configHome()
	if err != nil {
		fmt.Fprintf(stderr, "firekeeper export: find home directory: %v\n", err)
		return 1
	}
	machineID, err := exportMachineID(home)
	if err != nil {
		fmt.Fprintf(stderr, "firekeeper export: machine id: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	cutoff := time.Time{}
	if *since > 0 {
		cutoff = time.Now().Add(-*since)
	}

	found, foundProvider, err := findTranscriptSession(ctx, strings.TrimSpace(*sessionID), providers, cutoff, c.Exclude)
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "firekeeper export: interrupted")
		} else {
			fmt.Fprintf(stderr, "firekeeper export: %v\n", err)
		}
		return 1
	}
	source, paths, err := locateTranscriptFiles(foundProvider, found)
	if err != nil {
		fmt.Fprintf(stderr, "firekeeper export: %v\n", err)
		return 1
	}

	if *asJSON {
		if _, err := io.WriteString(stdout, `{"schema_version":"`+schemaVersion+`","events":[`); err != nil {
			fmt.Fprintf(stderr, "firekeeper export: write JSON: %v\n", err)
			return 1
		}
		defer fmt.Fprintln(stdout, "]}")
	}
	wroteEvent := false
	enc := json.NewEncoder(stdout)
	for _, path := range paths {
		events, _, err := source.Read(path, 0)
		if err != nil {
			fmt.Fprintf(stderr, "firekeeper export: read %s: %v\n", found.ID, err)
			return 1
		}
		for _, event := range events {
			event.MachineID = machineID
			event.SessionID = found.ID
			event.Provider = foundProvider
			if err := event.Validate(); err != nil {
				fmt.Fprintf(stderr, "firekeeper export: invalid event: %v\n", err)
				return 1
			}
			var output any = exportVersionedEvent{SchemaVersion: schemaVersion, Event: event}
			if *noText {
				output = exportStructureOnly{
					SchemaVersion: schemaVersion,
					MachineID:     event.MachineID,
					SessionID:     event.SessionID,
					Provider:      event.Provider,
					Seq:           event.Seq,
					TS:            event.TS,
					Role:          event.Role,
					ToolName:      event.ToolName,
					Model:         event.Model,
					Tokens:        event.Tokens,
				}
			}
			if *asJSON {
				if wroteEvent {
					if _, err := io.WriteString(stdout, ","); err != nil {
						fmt.Fprintf(stderr, "firekeeper export: write JSON: %v\n", err)
						return 1
					}
				}
				data, err := json.Marshal(output)
				if err != nil {
					fmt.Fprintf(stderr, "firekeeper export: encode JSON: %v\n", err)
					return 1
				}
				if _, err := stdout.Write(data); err != nil {
					fmt.Fprintf(stderr, "firekeeper export: write JSON: %v\n", err)
					return 1
				}
				wroteEvent = true
			} else if err := enc.Encode(output); err != nil {
				fmt.Fprintf(stderr, "firekeeper export: write JSONL: %v\n", err)
				return 1
			}
		}
	}
	return 0
}

func findTranscriptSession(ctx context.Context, sessionID string, providers []transcript.Provider, cutoff time.Time, exclude []string) (session.Meta, transcript.Provider, error) {
	for _, provider := range providers {
		enumerator, ok := exportEnumerate(provider)
		if !ok {
			continue
		}
		metas, err := enumerator.Enumerate(ctx, reader.EnumerateOptions{
			ModifiedAfter: cutoff,
			Skip: func(meta session.Meta) bool {
				return exportSkipReason(meta, exclude) != ""
			},
		})
		if err != nil {
			return session.Meta{}, "", fmt.Errorf("enumerate %s: %w", provider, err)
		}
		for _, meta := range metas {
			if !strings.EqualFold(meta.ID, sessionID) {
				continue
			}
			if reason := exportSkipReason(meta, exclude); reason != "" {
				return session.Meta{}, "", fmt.Errorf("session %q is excluded: %s", meta.ID, reason)
			}
			return meta, provider, nil
		}
	}
	return session.Meta{}, "", fmt.Errorf("session %q not found", sessionID)
}

func locateTranscriptFiles(provider transcript.Provider, meta session.Meta) (reader.Source, []string, error) {
	source, ok := exportSource(provider)
	if !ok {
		return nil, nil, fmt.Errorf("no transcript reader for %s", provider)
	}
	paths, err := source.Locate(meta)
	if err != nil {
		return nil, nil, fmt.Errorf("locate %s: %w", meta.ID, err)
	}
	return source, paths, nil
}

func readTranscriptEvents(provider transcript.Provider, meta session.Meta) ([]transcript.Event, error) {
	source, paths, err := locateTranscriptFiles(provider, meta)
	if err != nil {
		return nil, err
	}
	var events []transcript.Event
	for _, path := range paths {
		part, _, err := source.Read(path, 0)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", meta.ID, err)
		}
		events = append(events, part...)
	}
	return events, nil
}
