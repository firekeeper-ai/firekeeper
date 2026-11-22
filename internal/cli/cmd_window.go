package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/claudelimits"
	"github.com/firekeeper-ai/firekeeper/internal/config"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
	"github.com/firekeeper-ai/firekeeper/internal/window"
)

// windowLimits returns the Claude 5-hour or weekly limit when limits are on.
// It is a variable so tests never reach the config, Keychain or network.
var windowLimits = func(ctx context.Context, period window.Period) (*claudelimits.Window, error) {
	cfg, err := config.Load(config.Options{Getenv: os.Getenv})
	if err != nil || !cfg.ClaudeLimits {
		return nil, nil
	}
	limits, err := claudelimits.Client{}.Fetch(ctx)
	if period == window.PeriodWeek {
		return limits.SevenDay, err
	}
	return limits.FiveHour, err
}

var windowNow = time.Now

// windowBuild is a seam so tests can check the options each provider gets.
var windowBuild = window.Build

// windowCodexLimits returns the Codex 5-hour or weekly quota. It reports
// nothing until main installs the dashboard's Codex source with
// SetCodexLimits, since this package does not import the dashboard.
var windowCodexLimits = func(context.Context, window.Period) (*claudelimits.Window, error) {
	return nil, nil
}

// SetCodexLimits installs the source window uses for the Codex quota.
func SetCodexLimits(source func(context.Context, window.Period) (*claudelimits.Window, error)) {
	windowCodexLimits = source
}

// providerWindowLimits returns provider's own quota window for period, or
// nil when it has no limits source or the request failed.
func providerWindowLimits(ctx context.Context, provider transcript.Provider, period window.Period) *claudelimits.Window {
	var source func(context.Context, window.Period) (*claudelimits.Window, error)
	switch provider {
	case transcript.ProviderClaude:
		source = windowLimits
	case transcript.ProviderCodex:
		source = windowCodexLimits
	case transcript.ProviderCopilot, transcript.ProviderKimi, transcript.ProviderOpenCode,
		transcript.ProviderGemini, transcript.ProviderCursor:
		return nil // no 5-hour or weekly quota source
	default:
		return nil
	}
	w, err := source(ctx, period)
	if err != nil {
		return nil
	}
	return w
}

// runWindow breaks the current 5-hour or weekly window down by session, so a user near
// the limit can see what used it. Everything is read locally; the HTML page is
// a file on this machine, never uploaded.
func runWindow(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("firekeeper window", flag.ContinueOnError)
	fs.SetOutput(stderr)
	prov := transcript.ProviderClaude
	fs.Func("provider", "provider to review (default claude)", func(value string) error {
		p, err := config.ParseProvider(value)
		if err != nil {
			return err
		}
		prov = p
		return nil
	})
	weekly := fs.Bool("week", false, "review the weekly window instead of the 5-hour one")
	asJSON := fs.Bool("json", false, "print the breakdown as JSON")
	htmlPath := fs.String("html", "", "write the breakdown as a standalone HTML page to `file` and print its file:// URL")
	allProviders := fs.Bool("all", false, "combine all providers")
	since := fs.String("since", "", "review a duration ending now (for example 24h or 7d)")
	from := fs.String("from", "", "start of the review range (RFC3339)")
	to := fs.String("to", "", "end of the review range (RFC3339)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "firekeeper window: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *asJSON && *htmlPath != "" {
		fmt.Fprintln(stderr, "firekeeper window: use --json or --html, not both")
		return 2
	}
	var rangeStart, rangeEnd time.Time
	if *since != "" {
		if *from != "" || *to != "" {
			fmt.Fprintln(stderr, "firekeeper window: use --since or --from/--to, not both")
			return 2
		}
		d, err := time.ParseDuration(*since)
		if err != nil {
			fmt.Fprintf(stderr, "firekeeper window: invalid duration %q\n", *since)
			return 2
		}
		if d <= 0 {
			fmt.Fprintln(stderr, "firekeeper window: duration must be positive")
			return 2
		}
		rangeStart = windowNow().Add(-d)
		rangeEnd = windowNow()
	}
	if *from != "" {
		t, err := time.Parse(time.RFC3339, *from)
		if err != nil {
			fmt.Fprintf(stderr, "firekeeper window: invalid --from %q\n", *from)
			return 2
		}
		rangeStart = t
	}
	if *to != "" {
		t, err := time.Parse(time.RFC3339, *to)
		if err != nil {
			fmt.Fprintf(stderr, "firekeeper window: invalid --to %q\n", *to)
			return 2
		}
		rangeEnd = t
	}
	if !rangeStart.IsZero() && !rangeEnd.IsZero() && rangeStart.After(rangeEnd) {
		fmt.Fprintln(stderr, "firekeeper window: --from must be before --to")
		return 2
	}
	period := window.PeriodFiveHour
	timeout := time.Minute
	if *weekly {
		period = window.PeriodWeek
		timeout = 3 * time.Minute // a week of transcripts is a lot more to read
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	providers := []transcript.Provider{prov}
	if *allProviders {
		providers = transcript.Providers()
	}
	reports := make([]window.Report, 0, len(providers))
	for _, p := range providers {
		opts := window.Options{Provider: p, Period: period, Start: rangeStart, End: rangeEnd}
		// A failed limits request only costs the exact reset time.
		if w := providerWindowLimits(ctx, p, period); w != nil {
			opts.ResetsAt = w.ResetsAt
			used := w.UsedPercent
			opts.UsedPercent = &used
		}
		report, err := windowBuild(ctx, windowNow(), opts)
		if err == nil {
			reports = append(reports, report)
		} else if *allProviders && errors.Is(err, window.ErrNoReader) {
			continue
		} else if *allProviders {
			continue
		} else {
			fmt.Fprintf(stderr, "firekeeper window: %v\n", err)
			return 1
		}
	}

	switch {
	case *asJSON:
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if *allProviders {
			if err := enc.Encode(reports); err != nil {
				fmt.Fprintf(stderr, "firekeeper window: write JSON: %v\n", err)
				return 1
			}
		} else if len(reports) == 1 {
			if err := enc.Encode(reports[0]); err != nil {
				fmt.Fprintf(stderr, "firekeeper window: write JSON: %v\n", err)
				return 1
			}
		}
	case *htmlPath != "" && len(reports) == 1:
		report := reports[0]
		abs, err := filepath.Abs(*htmlPath)
		if err != nil {
			fmt.Fprintf(stderr, "firekeeper window: %v\n", err)
			return 1
		}
		f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			fmt.Fprintf(stderr, "firekeeper window: create page: %v\n", err)
			return 1
		}
		werr := window.WriteHTML(f, report)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			fmt.Fprintf(stderr, "firekeeper window: write page: %v\n", werr)
			return 1
		}
		fmt.Fprintln(stdout, (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String())
	case *htmlPath != "":
		fmt.Fprintln(stderr, "firekeeper window: --all does not support --html")
		return 2
	default:
		if len(reports) == 0 {
			fmt.Fprintln(stderr, "firekeeper window: no transcript readers available")
			return 1
		}
		for i, report := range reports {
			if *allProviders {
				if i > 0 {
					fmt.Fprintln(stdout)
				}
				fmt.Fprintf(stdout, "[%s]\n", report.Provider)
			}
			window.WriteText(stdout, report)
		}
	}
	return 0
}
