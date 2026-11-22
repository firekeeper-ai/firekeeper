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
	MachineID string              `json:"machine_id"`
	SessionID string              `json:"session_id"`
	Provider  transcript.Provider `json:"provider"`
	Seq       int64               `json:"seq"`
	TS        *time.Time          `json:"ts"`
	Role      transcript.Role     `json:"role"`
	ToolName  *string             `json:"tool_name"`
	Model     *string             `json:"model"`
	Tokens    transcript.Tokens   `json:"tokens"`
}

func runExport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("firekeeper export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sessionID := fs.String("session", "", "provider-native session id")
	providerName := fs.String("provider", "", "provider to search (codex, claude, copilot, kimi, opencode, gemini, cursor)")
	format := fs.String("format", "jsonl", "output format (jsonl)")
	noText := fs.Bool("no-text", false, "omit text and raw fields")
	since := fs.Duration("since", 0, "only search transcripts modified within this duration")

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

	var found *session.Meta
	var foundProvider transcript.Provider
	for _, provider := range providers {
		enumerator, ok := exportEnumerate(provider)
		if !ok {
			continue
		}
		metas, err := enumerator.Enumerate(ctx, reader.EnumerateOptions{
			ModifiedAfter: cutoff,
			Skip: func(meta session.Meta) bool {
				return exportSkipReason(meta, c.Exclude) != ""
			},
		})
		if err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(stderr, "firekeeper export: interrupted")
				return 1
			}
			fmt.Fprintf(stderr, "firekeeper export: enumerate %s: %v\n", provider, err)
			return 1
		}
		for i := range metas {
			if strings.EqualFold(metas[i].ID, strings.TrimSpace(*sessionID)) {
				meta := metas[i]
				found = &meta
				foundProvider = provider
				break
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		fmt.Fprintf(stderr, "firekeeper export: session %q not found\n", strings.TrimSpace(*sessionID))
		return 1
	}
	if reason := exportSkipReason(*found, c.Exclude); reason != "" {
		fmt.Fprintf(stderr, "firekeeper export: session %q is excluded: %s\n", found.ID, reason)
		return 1
	}
	source, ok := exportSource(foundProvider)
	if !ok {
		fmt.Fprintf(stderr, "firekeeper export: no transcript reader for %s\n", foundProvider)
		return 1
	}
	paths, err := source.Locate(*found)
	if err != nil {
		fmt.Fprintf(stderr, "firekeeper export: locate %s: %v\n", found.ID, err)
		return 1
	}

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
			var output any = event
			if *noText {
				output = exportStructureOnly{
					MachineID: event.MachineID,
					SessionID: event.SessionID,
					Provider:  event.Provider,
					Seq:       event.Seq,
					TS:        event.TS,
					Role:      event.Role,
					ToolName:  event.ToolName,
					Model:     event.Model,
					Tokens:    event.Tokens,
				}
			}
			if err := enc.Encode(output); err != nil {
				fmt.Fprintf(stderr, "firekeeper export: write JSONL: %v\n", err)
				return 1
			}
		}
	}
	return 0
}
