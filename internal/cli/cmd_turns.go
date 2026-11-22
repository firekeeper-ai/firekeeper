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
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
	"github.com/firekeeper-ai/firekeeper/internal/transcript/reader"
	"github.com/firekeeper-ai/firekeeper/internal/turns"
)

func runTurns(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("firekeeper turns", flag.ContinueOnError)
	fs.SetOutput(stderr)
	providerName := fs.String("provider", "", "provider to search (codex, claude, copilot, kimi, opencode, gemini, cursor)")
	asJSON := fs.Bool("json", false, "print per-turn metrics as JSON")
	if err := fs.Parse(flagsBeforePositionals(args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if !*asJSON {
		fmt.Fprintln(stderr, "firekeeper turns: --json is required")
		return 2
	}
	if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		fmt.Fprintln(stderr, "firekeeper turns: one session id is required")
		return 2
	}

	providers := reader.Registered()
	if strings.TrimSpace(*providerName) != "" {
		provider, err := config.ParseProvider(*providerName)
		if err != nil {
			fmt.Fprintf(stderr, "firekeeper turns: %v\n", err)
			return 2
		}
		providers = []transcript.Provider{provider}
	}
	c, err := loadConfig(configFlags{})
	if err != nil {
		fmt.Fprintf(stderr, "firekeeper turns: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	meta, provider, err := findTranscriptSession(ctx, strings.TrimSpace(fs.Arg(0)), providers, time.Time{}, c.Exclude)
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "firekeeper turns: interrupted")
		} else {
			fmt.Fprintf(stderr, "firekeeper turns: %v\n", err)
		}
		return 1
	}
	events, err := readTranscriptEvents(provider, meta)
	if err != nil {
		fmt.Fprintf(stderr, "firekeeper turns: %v\n", err)
		return 1
	}
	result := turns.Parse(events)
	if result == nil {
		result = []turns.Turn{}
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintf(stderr, "firekeeper turns: write JSON: %v\n", err)
		return 1
	}
	return 0
}

// flagsBeforePositionals lets the documented `turns SESSION_ID --json`
// spelling work with flag.FlagSet, which otherwise stops at the first
// positional argument.
func flagsBeforePositionals(args []string) []string {
	var flags, positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--provider" && i+1 < len(args) {
			flags = append(flags, arg, args[i+1])
			i++
			continue
		}
		if strings.HasPrefix(arg, "--provider=") || arg == "--json" || strings.HasPrefix(arg, "--json=") || strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			continue
		}
		positionals = append(positionals, arg)
	}
	return append(flags, positionals...)
}
