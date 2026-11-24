package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/firekeeper-ai/firekeeper/internal/daemon"
	"github.com/firekeeper-ai/firekeeper/internal/providerdir"
	"github.com/firekeeper-ai/firekeeper/internal/reporter"
	"github.com/firekeeper-ai/firekeeper/internal/session"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
)

const relativeCodexHome = "synthetic-relative-codex"

// TestRelativeOverrideFailsOnlyThatProvider is the regression test for a
// relative CODEX_HOME in report and backfill: Codex is dropped with a message
// naming the variable, Claude still runs, and the command fails only when
// Codex was the only provider requested.
func TestRelativeOverrideFailsOnlyThatProvider(t *testing.T) {
	t.Setenv(providerdir.EnvCodex, relativeCodexHome)
	t.Setenv(providerdir.EnvClaude, "")
	for _, command := range []struct {
		name string
		run  func(t *testing.T, args []string) (code int, ran bool, providers []transcript.Provider, stderr string)
	}{
		{"report", func(t *testing.T, args []string) (int, bool, []transcript.Provider, string) {
			cfg := stubReport(t, reporter.Summary{}, nil)
			var stdout, stderr bytes.Buffer
			code := runReport(append([]string{"--dry-run"}, args...), &stdout, &stderr)
			return code, cfg.Server != "", cfg.Providers, stderr.String()
		}},
		{"backfill", func(t *testing.T, args []string) (int, bool, []transcript.Provider, string) {
			cfg, _ := stubBackfill(t, "", false)
			var stdout, stderr bytes.Buffer
			code := runBackfill(append([]string{"--dry-run"}, args...), &stdout, &stderr)
			return code, cfg.Server != "", cfg.Providers, stderr.String()
		}},
	} {
		t.Run(command.name, func(t *testing.T) {
			withConfigFile(t, "server = \"https://saved.example\"\n")
			skip := "firekeeper " + command.name + ": skipping provider codex: CODEX_HOME must be an absolute path"

			code, ran, providers, stderr := command.run(t, []string{"--provider", "codex", "--provider", "claude"})
			if code != 0 || !ran || !slices.Equal(providers, []transcript.Provider{transcript.ProviderClaude}) {
				t.Fatalf("codex and claude: code %d, ran %v, providers %v; want claude to run\n%s", code, ran, providers, stderr)
			}
			if strings.Count(stderr, skip) != 1 || strings.Contains(stderr, relativeCodexHome) {
				t.Fatalf("stderr = %q; want the skip once, without the value", stderr)
			}

			code, ran, _, stderr = command.run(t, []string{"--provider", "codex"})
			if code == 0 || ran {
				t.Fatalf("codex only: code %d, ran %v; want a failure without a pass", code, ran)
			}
			if !strings.Contains(stderr, skip) {
				t.Fatalf("stderr = %q", stderr)
			}
		})
	}
}

func TestUsableProviders(t *testing.T) {
	t.Setenv(providerdir.EnvCodex, relativeCodexHome)
	t.Setenv(providerdir.EnvClaude, "")
	var stderr bytes.Buffer
	if usable, ok := usableProviders("x", nil, &stderr); !ok || usable != nil || stderr.Len() != 0 {
		t.Fatalf("no providers: %v, %v, %q", usable, ok, stderr.String())
	}
	if usable, ok := usableProviders("x", []transcript.Provider{transcript.ProviderClaude}, &stderr); !ok || !slices.Equal(usable, []transcript.Provider{transcript.ProviderClaude}) {
		t.Fatalf("claude: %v, %v", usable, ok)
	}
	if usable, ok := usableProviders("x", []transcript.Provider{transcript.ProviderCodex}, &stderr); ok || usable != nil {
		t.Fatalf("codex: %v, %v; want none usable", usable, ok)
	}
}

// TestDashboardAndDaemonAgree checks that the dashboard's folder lookups
// (internal/session) and the daemon's provider detection (internal/daemon)
// give the same answer for an unset, absolute, or relative override.
func TestDashboardAndDaemonAgree(t *testing.T) {
	dashboard := map[transcript.Provider]func() (string, error){
		transcript.ProviderClaude:  session.ClaudeConfigDir,
		transcript.ProviderCopilot: session.CopilotHome,
		transcript.ProviderKimi:    session.KimiHome,
		transcript.ProviderCursor:  session.CursorDir,
		transcript.ProviderGemini: func() (string, error) {
			dirs, err := session.GeminiDirs()
			if err != nil {
				return "", err
			}
			return dirs[0], nil
		},
		transcript.ProviderOpenCode: func() (string, error) {
			db, err := session.OpenCodeDatabase("")
			if err != nil {
				return "", err
			}
			return filepath.Dir(db), nil
		},
	}
	envOf := map[transcript.Provider]string{
		transcript.ProviderClaude: providerdir.EnvClaude, transcript.ProviderCopilot: providerdir.EnvCopilot,
		transcript.ProviderKimi: providerdir.EnvKimi, transcript.ProviderCursor: providerdir.EnvCursor,
		transcript.ProviderGemini: providerdir.EnvGemini, transcript.ProviderOpenCode: providerdir.EnvXDGData,
	}
	for provider, lookup := range dashboard {
		for _, value := range []string{"", "absolute", "relative", "./relative", "~/relative"} {
			t.Run(string(provider)+"/"+value, func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("USERPROFILE", home)
				for _, name := range providerdir.EnvNames() {
					t.Setenv(name, "")
				}
				if value == "absolute" {
					value = filepath.Join(t.TempDir(), "override")
				}
				t.Setenv(envOf[provider], value)

				dir, err := lookup()
				detected, skipped := daemon.DetectProviders(home, nil, func(string) (string, error) { return "", errors.New("not installed") })
				daemonSkipped := slices.ContainsFunc(skipped, func(err error) bool {
					return strings.HasPrefix(err.Error(), string(provider)+": ")
				})
				if errors.Is(err, providerdir.ErrRelativeOverride) != daemonSkipped {
					t.Fatalf("dashboard error %v, daemon skipped %v; want them to agree", err, daemonSkipped)
				}
				if err != nil {
					if !errors.Is(err, providerdir.ErrRelativeOverride) || slices.Contains(detected, provider) {
						t.Fatalf("dashboard error %v, daemon detected %v", err, detected)
					}
					return
				}
				// The daemon finds the provider exactly when the folder
				// the dashboard reads exists.
				if slices.Contains(detected, provider) {
					t.Fatalf("daemon detected %s before its folder %s existed", provider, dir)
				}
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if detected, _ := daemon.DetectProviders(home, nil, func(string) (string, error) { return "", errors.New("not installed") }); !slices.Contains(detected, provider) {
					t.Fatalf("daemon did not detect %s in the dashboard's folder %s", provider, dir)
				}
			})
		}
	}
}
