// Package providerdir finds the folders each coding agent keeps its data in,
// honoring the agent's own environment variable override. Every part of
// Firekeeper that reads provider data asks it, so the dashboard, the daemon,
// report, and backfill agree on where to look.
//
// An override is used exactly as the agent itself uses it: as given, with an
// empty value meaning unset. A relative override is an error
// (ErrRelativeOverride): the agent resolves it against its own working
// directory, which Firekeeper does not know. The error names the variable,
// never its value, and affects only that provider.
//
// It is standard library only, apart from the provider names in pkg/event.
package providerdir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/firekeeper-ai/firekeeper/pkg/event"
)

// The environment variables each agent reads for its data folder.
const (
	EnvClaude   = "CLAUDE_CONFIG_DIR" // replaces ~/.claude
	EnvCodex    = "CODEX_HOME"        // replaces ~/.codex
	EnvCopilot  = "COPILOT_HOME"      // replaces ~/.copilot
	EnvKimi     = "KIMI_CODE_HOME"    // replaces ~/.kimi-code
	EnvXDGData  = "XDG_DATA_HOME"     // OpenCode's data root, replacing ~/.local/share
	EnvOpenCode = "OPENCODE_DB"       // OpenCode's database file
	EnvGemini   = "GEMINI_CLI_HOME"   // replaces the home folder for Gemini CLI
	EnvCursor   = "CURSOR_CONFIG_DIR" // replaces ~/.cursor
)

// EnvNames lists every variable above, for passing provider overrides on to
// a process such as the daemon service.
func EnvNames() []string {
	return []string{EnvCodex, EnvCopilot, EnvKimi, EnvClaude, EnvXDGData, EnvOpenCode, EnvGemini, EnvCursor}
}

// ErrRelativeOverride means a provider override is set to a relative path.
var ErrRelativeOverride = errors.New("provider folder override must be an absolute path")

// OverrideError reports an override that is not an absolute path. Its
// message names the variable only; the value can hold a private path.
type OverrideError struct{ Name string }

func (e *OverrideError) Error() string { return e.Name + " must be an absolute path" }

// Unwrap lets errors.Is match ErrRelativeOverride.
func (e *OverrideError) Unwrap() error { return ErrRelativeOverride }

// Lookup resolves provider folders. Its zero value reads the process
// environment and the user's home folder; tests set Getenv and Home.
type Lookup struct {
	// Getenv reads an environment variable. Nil means os.Getenv.
	Getenv func(string) string
	// Home returns the user's home folder. It is called only when a default
	// folder is needed. Nil means os.UserHomeDir.
	Home func() (string, error)
}

// Override is the value of the variable name as a folder override: "" when
// it is unset or empty, the value unchanged when it is absolute, and an
// *OverrideError (errors.Is ErrRelativeOverride) otherwise, including "~/x",
// which the agents do not expand either.
func (l Lookup) Override(name string) (string, error) {
	value := l.getenv(name)
	if value == "" {
		return "", nil
	}
	if !filepath.IsAbs(value) {
		return "", &OverrideError{Name: name}
	}
	return value, nil
}

// Dir is provider p's data folder: its override when set, else the default
// under the home folder. For OpenCode it is the folder holding its database
// ($XDG_DATA_HOME/opencode); for Gemini CLI it is $GEMINI_CLI_HOME/.gemini
// (see GeminiDirs for the sandbox folder too).
func (l Lookup) Dir(p event.Provider) (string, error) {
	switch p {
	case event.ProviderClaude:
		return l.dir(EnvClaude, ".claude")
	case event.ProviderCodex:
		return l.dir(EnvCodex, ".codex")
	case event.ProviderCopilot:
		return l.dir(EnvCopilot, ".copilot")
	case event.ProviderKimi:
		return l.dir(EnvKimi, ".kimi-code")
	case event.ProviderCursor:
		return l.dir(EnvCursor, ".cursor")
	case event.ProviderGemini:
		home, err := l.geminiHome()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".gemini"), nil
	case event.ProviderOpenCode:
		data, err := l.dir(EnvXDGData, filepath.Join(".local", "share"))
		if err != nil {
			return "", err
		}
		return filepath.Join(data, "opencode"), nil
	default:
		return "", fmt.Errorf("no data folder known for provider %q", p)
	}
}

// DataPath resolves the location used to detect a provider: its data directory
// by default, or the database file when OPENCODE_DB is set. An in-memory
// database has no path. kind is the expected file type (zero for a regular file).
func (l Lookup) DataPath(p event.Provider) (path string, kind fs.FileMode, err error) {
	if p == event.ProviderOpenCode && l.getenv(EnvOpenCode) != "" {
		path, err = l.OpenCodeDatabase()
		return path, 0, err
	}
	path, err = l.Dir(p)
	return path, fs.ModeDir, err
}

// Check reports whether provider p's override can be used: nil, or the
// error (errors.Is ErrRelativeOverride) that makes p unusable. Other failures,
// such as an unknown home folder, are left to the code that reads the folder.
func (l Lookup) Check(p event.Provider) error {
	if _, _, err := l.DataPath(p); errors.Is(err, ErrRelativeOverride) {
		return err
	}
	return nil
}

// GeminiDirs are the folders Gemini CLI keeps session state in:
// $GEMINI_CLI_HOME/.gemini (default ~/.gemini), then
// $GEMINI_CLI_HOME/.cache/.gemini, where it writes under the macOS
// sandbox-exec sandbox. Whether a session ran sandboxed is not visible from
// outside, so both are searched. Either may be missing.
func (l Lookup) GeminiDirs() ([]string, error) {
	home, err := l.geminiHome()
	if err != nil {
		return nil, err
	}
	return []string{filepath.Join(home, ".gemini"), filepath.Join(home, ".cache", ".gemini")}, nil
}

// OpenCodeDatabase is the path of OpenCode's database, resolved as OpenCode
// does: OPENCODE_DB as given when absolute, under the data folder when
// relative (OpenCode resolves it there, not against a working directory),
// and opencode.db in the data folder when unset. It is "" for :memory:,
// which stores nothing.
func (l Lookup) OpenCodeDatabase() (string, error) {
	db := l.getenv(EnvOpenCode)
	if db == ":memory:" {
		return "", nil
	}
	if db != "" && filepath.IsAbs(db) {
		return db, nil
	}
	dir, err := l.Dir(event.ProviderOpenCode)
	if err != nil {
		return "", err
	}
	if db == "" {
		db = "opencode.db"
	}
	return filepath.Join(dir, db), nil
}

// geminiHome is GEMINI_CLI_HOME, which replaces the home folder, or the home
// folder.
func (l Lookup) geminiHome() (string, error) {
	home, err := l.Override(EnvGemini)
	if err != nil || home != "" {
		return home, err
	}
	return l.home()
}

// dir is the override in name, or rel under the home folder.
func (l Lookup) dir(name, rel string) (string, error) {
	dir, err := l.Override(name)
	if err != nil || dir != "" {
		return dir, err
	}
	home, err := l.home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, rel), nil
}

func (l Lookup) getenv(name string) string {
	if l.Getenv != nil {
		return l.Getenv(name)
	}
	return os.Getenv(name)
}

func (l Lookup) home() (string, error) {
	home := os.UserHomeDir
	if l.Home != nil {
		home = l.Home
	}
	dir, err := home()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	if dir == "" {
		return "", errors.New("find home directory: none set")
	}
	return dir, nil
}
