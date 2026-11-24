package daemon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/firekeeper-ai/firekeeper/internal/providerdir"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
)

// DetectProviders finds supported transcript providers without reading their
// data or launching their CLIs. An existing provider data folder (see
// providerdir), configured OpenCode database, or executable on PATH is
// sufficient. A relative folder override that prevents resolution skips that
// provider. Its error names only the variable.
func DetectProviders(home string, getenv func(string) string, lookPath func(string) (string, error)) (providers []transcript.Provider, skipped []error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	dirs := providerdir.Lookup{Getenv: getenv, Home: func() (string, error) { return home, nil }}
	for _, candidate := range []struct {
		provider transcript.Provider
		command  string
	}{
		{transcript.ProviderCodex, "codex"},
		{transcript.ProviderCopilot, "copilot"},
		{transcript.ProviderKimi, "kimi"},
		{transcript.ProviderClaude, "claude"},
		{transcript.ProviderOpenCode, "opencode"},
		{transcript.ProviderGemini, "gemini"},
		{transcript.ProviderCursor, "cursor-agent"},
	} {
		path, kind, err := dirs.DataPath(candidate.provider)
		if errors.Is(err, providerdir.ErrRelativeOverride) {
			skipped = append(skipped, fmt.Errorf("%s: %w", candidate.provider, err))
			continue
		}
		if err == nil && path != "" {
			info, err := os.Stat(path)
			if err == nil && info.Mode().Type() == kind {
				providers = append(providers, candidate.provider)
				continue
			}
		}
		if _, err := lookPath(candidate.command); err == nil {
			providers = append(providers, candidate.provider)
		}
	}
	return providers, skipped
}
