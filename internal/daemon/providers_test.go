package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/firekeeper-ai/firekeeper/internal/providerdir"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
)

func TestDetectProviders(t *testing.T) {
	all := []transcript.Provider{transcript.ProviderCodex, transcript.ProviderCopilot, transcript.ProviderKimi, transcript.ProviderClaude, transcript.ProviderOpenCode, transcript.ProviderGemini, transcript.ProviderCursor}
	for _, scenario := range []string{"none", "directories", "executables", "override", "missing override", "file", "relative override"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			env := map[string]string{}
			commands := map[string]bool{}
			var want []transcript.Provider
			mkdir := func(path string) {
				t.Helper()
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "directories":
				for _, name := range []string{".codex", ".copilot", ".claude", ".kimi-code", filepath.Join(".local", "share", "opencode"), ".gemini", ".cursor"} {
					mkdir(filepath.Join(home, name))
				}
				want = all
			case "executables":
				commands = map[string]bool{"codex": true, "copilot": true, "claude": true, "kimi": true, "opencode": true, "gemini": true, "cursor-agent": true}
				mkdir(filepath.Join(home, ".codex"))
				want = all
			case "override":
				for _, key := range []string{"CODEX_HOME", "COPILOT_HOME", "KIMI_CODE_HOME", "CLAUDE_CONFIG_DIR", "CURSOR_CONFIG_DIR"} {
					env[key] = filepath.Join(home, key)
					mkdir(env[key])
				}
				env["XDG_DATA_HOME"] = filepath.Join(home, "data")
				mkdir(filepath.Join(env["XDG_DATA_HOME"], "opencode"))
				// GEMINI_CLI_HOME replaces the home directory, so the data
				// is in its .gemini.
				env["GEMINI_CLI_HOME"] = filepath.Join(home, "gemini-home")
				mkdir(filepath.Join(env["GEMINI_CLI_HOME"], ".gemini"))
				want = all
			case "missing override":
				mkdir(filepath.Join(home, ".codex"))
				env["CODEX_HOME"] = filepath.Join(home, "missing")
				mkdir(filepath.Join(home, ".local", "share", "opencode"))
				env["XDG_DATA_HOME"] = filepath.Join(home, "missing")
				mkdir(filepath.Join(home, ".kimi-code"))
				env["KIMI_CODE_HOME"] = filepath.Join(home, "missing")
				mkdir(filepath.Join(home, ".gemini"))
				env["GEMINI_CLI_HOME"] = filepath.Join(home, "missing")
				mkdir(filepath.Join(home, ".cursor"))
				env["CURSOR_CONFIG_DIR"] = filepath.Join(home, "missing")
			case "relative override":
				// Codex is installed, but CODEX_HOME is relative: it is
				// skipped, with an error naming the variable, and Copilot
				// is still found.
				mkdir(filepath.Join(home, ".codex"))
				commands["codex"] = true
				env["CODEX_HOME"] = "synthetic-relative-codex"
				mkdir(filepath.Join(home, ".copilot"))
				want = []transcript.Provider{transcript.ProviderCopilot}
			case "file":
				if err := os.WriteFile(filepath.Join(home, ".codex"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, skipped := DetectProviders(home, func(key string) string { return env[key] }, func(command string) (string, error) {
				if commands[command] {
					return filepath.Join(home, command), nil
				}
				return "", errors.New("not installed")
			})
			if !slices.Equal(got, want) {
				t.Fatalf("providers = %v, want %v", got, want)
			}
			if scenario != "relative override" {
				if len(skipped) != 0 {
					t.Fatalf("skipped = %v, want none", skipped)
				}
				return
			}
			if len(skipped) != 1 || !errors.Is(skipped[0], providerdir.ErrRelativeOverride) {
				t.Fatalf("skipped = %v, want one ErrRelativeOverride", skipped)
			}
			if msg := skipped[0].Error(); msg != "codex: CODEX_HOME must be an absolute path" {
				t.Fatalf("skipped message = %q", msg)
			}
		})
	}
}
