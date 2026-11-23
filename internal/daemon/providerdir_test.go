package daemon

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/providerdir"
	"github.com/firekeeper-ai/firekeeper/internal/reporter"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
)

const relativeCodexHome = "synthetic-relative-codex"

// readLog is the daemon log in dir.
func readLog(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestRelativeOverrideSkipsOnlyThatProvider is the regression test for a
// relative CODEX_HOME: the daemon drops Codex, logs it once, and keeps
// uploading Claude on every pass.
func TestRelativeOverrideSkipsOnlyThatProvider(t *testing.T) {
	t.Setenv(providerdir.EnvCodex, relativeCodexHome)
	t.Setenv(providerdir.EnvClaude, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	cfg := uploading()
	cfg.Home = t.TempDir()
	cfg.Providers = []transcript.Provider{transcript.ProviderCodex, transcript.ProviderClaude}
	var passes [][]transcript.Provider
	err := Run(ctx, Config{
		Reporter: cfg,
		Dir:      dir,
		Clock:    &fakeClock{limit: 3, cancel: cancel},
		RunOnce: func(_ context.Context, pass reporter.Config) (reporter.Summary, error) {
			passes = append(passes, slices.Clone(pass.Providers))
			return reporter.Summary{}, nil
		},
		Heartbeat: func(context.Context, reporter.Config, reporter.Summary) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(passes) < 2 {
		t.Fatalf("ran %d passes, want the daemon to keep running", len(passes))
	}
	for i, providers := range passes {
		if !slices.Equal(providers, []transcript.Provider{transcript.ProviderClaude}) {
			t.Fatalf("pass %d providers = %v, want claude only", i, providers)
		}
	}
	log := readLog(t, dir)
	if n := strings.Count(log, "skipping provider codex: CODEX_HOME must be an absolute path"); n != 1 {
		t.Fatalf("skip logged %d times, want once:\n%s", n, log)
	}
	if strings.Contains(log, relativeCodexHome) {
		t.Fatal("the log repeats the variable's value")
	}
}

func TestNoUsableProviderKeepsRunning(t *testing.T) {
	t.Setenv(providerdir.EnvCodex, relativeCodexHome)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	dir := t.TempDir()
	cfg := uploading()
	cfg.Home = t.TempDir()
	err := Run(ctx, Config{
		Reporter: cfg,
		Dir:      dir,
		RunOnce: func(context.Context, reporter.Config) (reporter.Summary, error) {
			t.Error("a pass ran with no usable provider")
			return reporter.Summary{}, nil
		},
		Heartbeat: func(context.Context, reporter.Config, reporter.Summary) error { return nil },
	})
	if err != nil {
		t.Fatalf("Run = %v; want it to keep running until stopped", err)
	}
	log := readLog(t, dir)
	if !strings.Contains(log, "skipping provider codex") || !strings.Contains(log, "no usable upload provider") || !strings.Contains(log, "stopped") {
		t.Fatalf("log:\n%s", log)
	}
}
