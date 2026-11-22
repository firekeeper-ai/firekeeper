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
	"strconv"
	"strings"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/config"
	"github.com/firekeeper-ai/firekeeper/internal/parallel"
	"github.com/firekeeper-ai/firekeeper/internal/reporter"
	"github.com/firekeeper-ai/firekeeper/internal/session"
	"github.com/firekeeper-ai/firekeeper/internal/stats"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
	"github.com/firekeeper-ai/firekeeper/internal/transcript/reader"
)

var (
	statsEnumerate  = reader.EnumeratorFor
	statsSource     = reader.For
	statsSkipReason = reporter.SkipReason
	statsNow        = time.Now
)

type statsProviderResult struct {
	provider transcript.Provider
	metas    []session.Meta
	err      error
}

type statsSessionResult struct {
	events []transcript.Event
	err    error
}

func runStats(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("firekeeper stats", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sinceFlag := fs.String("since", "7d", "include events from this duration ending now (for example 7d or 168h)")
	providerName := fs.String("provider", "", "provider to include")
	allProviders := fs.Bool("all", false, "include all providers")
	project := fs.String("project", "", "include this exact project name")
	asJSON := fs.Bool("json", false, "print findings and aggregates as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "firekeeper stats: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if !*asJSON {
		fmt.Fprintln(stderr, "firekeeper stats: --json is required")
		return 2
	}
	if *allProviders && strings.TrimSpace(*providerName) != "" {
		fmt.Fprintln(stderr, "firekeeper stats: use --provider or --all, not both")
		return 2
	}
	duration, err := parseStatsDuration(*sinceFlag)
	if err != nil {
		fmt.Fprintf(stderr, "firekeeper stats: invalid --since: %v\n", err)
		return 2
	}

	providers := reader.Registered()
	if strings.TrimSpace(*providerName) != "" {
		provider, err := config.ParseProvider(*providerName)
		if err != nil {
			fmt.Fprintf(stderr, "firekeeper stats: %v\n", err)
			return 2
		}
		providers = []transcript.Provider{provider}
	}
	cfg, err := loadConfig(configFlags{})
	if err != nil {
		fmt.Fprintf(stderr, "firekeeper stats: load config: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	since := statsNow().Add(-duration)

	providerResults := make([]statsProviderResult, len(providers))
	parallel.Each(len(providers), func(i int) {
		provider := providers[i]
		result := statsProviderResult{provider: provider}
		enumerator, ok := statsEnumerate(provider)
		if !ok {
			providerResults[i] = result
			return
		}
		result.metas, result.err = enumerator.Enumerate(ctx, reader.EnumerateOptions{
			ModifiedAfter: since,
			Skip: func(meta session.Meta) bool {
				return statsSkipReason(meta, cfg.Exclude) != ""
			},
		})
		providerResults[i] = result
	})
	for _, result := range providerResults {
		if result.err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(stderr, "firekeeper stats: interrupted")
			} else {
				fmt.Fprintf(stderr, "firekeeper stats: enumerate %s: %v\n", result.provider, result.err)
			}
			return 1
		}
	}

	type sessionJob struct {
		provider transcript.Provider
		meta     session.Meta
		project  string
	}
	var jobs []sessionJob
	for _, result := range providerResults {
		for _, meta := range result.metas {
			projectName := meta.Project
			if projectName == "" {
				projectName = reader.ProjectName(meta.CWD)
			}
			if *project != "" && projectName != *project {
				continue
			}
			jobs = append(jobs, sessionJob{provider: result.provider, meta: meta, project: projectName})
		}
	}

	readResults := make([]statsSessionResult, len(jobs))
	parallel.Bounded(len(jobs), func(i int) {
		job := jobs[i]
		source, ok := statsSource(job.provider)
		if !ok {
			readResults[i].err = fmt.Errorf("no transcript reader")
			return
		}
		paths, err := source.Locate(job.meta)
		if err != nil {
			readResults[i].err = err
			return
		}
		for _, path := range paths {
			events, _, err := source.Read(path, 0)
			if err != nil {
				readResults[i].err = err
				return
			}
			readResults[i].events = append(readResults[i].events, events...)
		}
	})
	data := make([]stats.Session, 0, len(jobs))
	for i, result := range readResults {
		if result.err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(stderr, "firekeeper stats: interrupted")
			} else {
				fmt.Fprintf(stderr, "firekeeper stats: read session %q: %v\n", jobs[i].meta.ID, result.err)
			}
			return 1
		}
		data = append(data, stats.Session{
			ID: jobs[i].meta.ID, Provider: jobs[i].provider, Project: jobs[i].project, Events: result.events,
		})
	}

	report := stats.Build(data, since)
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(stderr, "firekeeper stats: write JSON: %v\n", err)
		return 1
	}
	return 0
}

func parseStatsDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "d") {
		days, err := strconv.ParseInt(strings.TrimSuffix(value, "d"), 10, 64)
		if err != nil {
			return 0, err
		}
		maxDays := int64((time.Duration(1<<63 - 1)) / (24 * time.Hour))
		if days > maxDays || days < -maxDays {
			return 0, fmt.Errorf("duration is too large")
		}
		duration := time.Duration(days) * 24 * time.Hour
		if duration <= 0 {
			return 0, fmt.Errorf("duration must be positive")
		}
		return duration, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if duration <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}
	return duration, nil
}
