package providerdir

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/firekeeper-ai/firekeeper/pkg/event"
)

// lookup is a Lookup over env and a fixed home folder.
func lookup(home string, env map[string]string) Lookup {
	return Lookup{
		Getenv: func(name string) string { return env[name] },
		Home:   func() (string, error) { return home, nil },
	}
}

// relativeForms are override values every agent resolves against its own
// working directory, or does not expand.
var relativeForms = []string{"x", "./x", "../x", "~/x", "~", " /abs/with/leading/space", "."}

func TestDirDefaultsAndOverrides(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	custom := filepath.Join(t.TempDir(), "custom")
	for _, c := range []struct {
		provider event.Provider
		env      string
		def      string // default under home
		override string // with the override set to custom
	}{
		{event.ProviderClaude, EnvClaude, ".claude", custom},
		{event.ProviderCodex, EnvCodex, ".codex", custom},
		{event.ProviderCopilot, EnvCopilot, ".copilot", custom},
		{event.ProviderKimi, EnvKimi, ".kimi-code", custom},
		{event.ProviderCursor, EnvCursor, ".cursor", custom},
		// GEMINI_CLI_HOME replaces the home folder.
		{event.ProviderGemini, EnvGemini, ".gemini", filepath.Join(custom, ".gemini")},
		// XDG_DATA_HOME replaces ~/.local/share.
		{event.ProviderOpenCode, EnvXDGData, filepath.Join(".local", "share", "opencode"), filepath.Join(custom, "opencode")},
	} {
		t.Run(string(c.provider), func(t *testing.T) {
			got, err := lookup(home, nil).Dir(c.provider)
			if err != nil || got != filepath.Join(home, c.def) {
				t.Fatalf("default = %q, %v; want %q", got, err, filepath.Join(home, c.def))
			}
			got, err = lookup(home, map[string]string{c.env: ""}).Dir(c.provider)
			if err != nil || got != filepath.Join(home, c.def) {
				t.Fatalf("empty override = %q, %v; want the default", got, err)
			}
			got, err = lookup(home, map[string]string{c.env: custom}).Dir(c.provider)
			if err != nil || got != c.override {
				t.Fatalf("override = %q, %v; want %q", got, err, c.override)
			}
			// A trailing space is part of an absolute path, as for the agent.
			spaced := custom + " "
			if got, err := lookup(home, map[string]string{c.env: spaced}).Dir(c.provider); err != nil || !strings.HasPrefix(got, spaced) {
				t.Fatalf("override with a trailing space = %q, %v; want it kept", got, err)
			}
			for _, value := range relativeForms {
				got, err := lookup(home, map[string]string{c.env: value}).Dir(c.provider)
				var overrideErr *OverrideError
				if got != "" || !errors.Is(err, ErrRelativeOverride) || !errors.As(err, &overrideErr) || overrideErr.Name != c.env {
					t.Fatalf("%s=%q: got %q, %v; want an ErrRelativeOverride naming %s", c.env, value, got, err, c.env)
				}
				if err.Error() != c.env+" must be an absolute path" {
					t.Fatalf("message = %q", err.Error())
				}
				if err := lookup(home, map[string]string{c.env: value}).Check(c.provider); !errors.Is(err, ErrRelativeOverride) {
					t.Fatalf("Check with %s=%q = %v", c.env, value, err)
				}
			}
			if err := lookup(home, map[string]string{c.env: custom}).Check(c.provider); err != nil {
				t.Fatalf("Check with an absolute override = %v", err)
			}
		})
	}
}

func TestErrorsNameTheVariableNotTheValue(t *testing.T) {
	const private = "synthetic-private-folder"
	for _, name := range EnvNames() {
		if name == EnvOpenCode {
			continue // OPENCODE_DB may be relative
		}
		_, err := lookup("/home", map[string]string{name: private}).Override(name)
		if err == nil || strings.Contains(err.Error(), private) || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: error %v; want one naming the variable, not its value", name, err)
		}
	}
}

func TestEveryProviderHasADataFolder(t *testing.T) {
	for _, p := range event.Providers() {
		if _, err := lookup("/home", nil).Dir(p); err != nil {
			t.Errorf("Dir(%s) = %v", p, err)
		}
		if err := lookup("/home", nil).Check(p); err != nil {
			t.Errorf("Check(%s) = %v", p, err)
		}
	}
	for _, p := range []event.Provider{"", "unknown", "CODEX"} {
		if dir, err := lookup("/home", nil).Dir(p); err == nil || dir != "" {
			t.Errorf("Dir(%q) = %q, %v; want an error", p, dir, err)
		}
	}
}

func TestEnvNamesListsEveryOverride(t *testing.T) {
	names := EnvNames()
	for _, name := range []string{EnvClaude, EnvCodex, EnvCopilot, EnvKimi, EnvXDGData, EnvOpenCode, EnvGemini, EnvCursor} {
		if !slices.Contains(names, name) {
			t.Errorf("EnvNames lacks %s", name)
		}
	}
	if len(names) != 8 || slices.Contains(names, "KIMI_SHARE_DIR") {
		t.Errorf("EnvNames = %v", names)
	}
	// Each call returns a fresh slice, so a caller cannot change the next.
	names[0] = "changed"
	if EnvNames()[0] == "changed" {
		t.Error("EnvNames shares its slice")
	}
}

func TestKimiIgnoresShareDir(t *testing.T) {
	// KIMI_SHARE_DIR belongs to the older kimi-cli (~/.kimi); Kimi Code
	// never reads it.
	got, err := lookup("/home", map[string]string{"KIMI_SHARE_DIR": "/legacy"}).Dir(event.ProviderKimi)
	if err != nil || got != filepath.Join("/home", ".kimi-code") {
		t.Fatalf("Dir(kimi) = %q, %v; want ~/.kimi-code", got, err)
	}
}

func TestGeminiDirs(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "gemini-home")
	for name, c := range map[string]struct {
		env  map[string]string
		want []string
	}{
		"default":  {nil, []string{filepath.Join("/home", ".gemini"), filepath.Join("/home", ".cache", ".gemini")}},
		"override": {map[string]string{EnvGemini: custom}, []string{filepath.Join(custom, ".gemini"), filepath.Join(custom, ".cache", ".gemini")}},
	} {
		got, err := lookup("/home", c.env).GeminiDirs()
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%s: GeminiDirs = %v, %v; want %v", name, got, err, c.want)
		}
	}
	if dirs, err := lookup("/home", map[string]string{EnvGemini: "gemini"}).GeminiDirs(); dirs != nil || !errors.Is(err, ErrRelativeOverride) {
		t.Errorf("relative GEMINI_CLI_HOME: %v, %v", dirs, err)
	}
}

func TestOpenCodeDatabase(t *testing.T) {
	data := filepath.Join(t.TempDir(), "xdg")
	elsewhere := filepath.Join(t.TempDir(), "oc.db")
	for name, c := range map[string]struct {
		env     map[string]string
		want    string
		wantErr bool
	}{
		"default":      {nil, filepath.Join("/home", ".local", "share", "opencode", "opencode.db"), false},
		"xdg":          {map[string]string{EnvXDGData: data}, filepath.Join(data, "opencode", "opencode.db"), false},
		"absolute db":  {map[string]string{EnvXDGData: "relative", EnvOpenCode: elsewhere}, elsewhere, false},
		"relative db":  {map[string]string{EnvXDGData: data, EnvOpenCode: "dev.db"}, filepath.Join(data, "opencode", "dev.db"), false},
		"in-memory":    {map[string]string{EnvXDGData: "relative", EnvOpenCode: ":memory:"}, "", false},
		"relative xdg": {map[string]string{EnvXDGData: "relative"}, "", true},
	} {
		l := lookup("/home", c.env)
		if err := l.Check(event.ProviderOpenCode); (err != nil) != c.wantErr || (c.wantErr && !errors.Is(err, ErrRelativeOverride)) {
			t.Errorf("%s: Check = %v; want override error: %v", name, err, c.wantErr)
		}
		got, err := l.OpenCodeDatabase()
		if (err != nil) != c.wantErr || got != c.want || (c.wantErr && !errors.Is(err, ErrRelativeOverride)) {
			t.Errorf("%s: OpenCodeDatabase = %q, %v; want %q (error %v)", name, got, err, c.want, c.wantErr)
		}
	}
}

func TestHomeFailures(t *testing.T) {
	failing := Lookup{Getenv: func(string) string { return "" }, Home: func() (string, error) { return "", errors.New("no home") }}
	if dir, err := failing.Dir(event.ProviderClaude); err == nil || dir != "" || !strings.Contains(err.Error(), "find home directory") {
		t.Errorf("Dir without a home = %q, %v", dir, err)
	}
	// Not an override problem, so Check leaves it to the reader.
	if err := failing.Check(event.ProviderClaude); err != nil {
		t.Errorf("Check without a home = %v", err)
	}
	empty := Lookup{Getenv: func(string) string { return "" }, Home: func() (string, error) { return "", nil }}
	if dir, err := empty.Dir(event.ProviderCodex); err == nil || dir != "" {
		t.Errorf("Dir with an empty home = %q, %v", dir, err)
	}
	// An override needs no home folder.
	custom := filepath.Join(t.TempDir(), "claude")
	withOverride := Lookup{Getenv: func(n string) string {
		if n == EnvClaude {
			return custom
		}
		return ""
	}, Home: failing.Home}
	if dir, err := withOverride.Dir(event.ProviderClaude); err != nil || dir != custom {
		t.Errorf("override without a home = %q, %v", dir, err)
	}
}

func TestZeroLookupReadsTheEnvironment(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "codex")
	t.Setenv(EnvCodex, custom)
	if dir, err := (Lookup{}).Dir(event.ProviderCodex); err != nil || dir != custom {
		t.Fatalf("Dir = %q, %v; want %q", dir, err, custom)
	}
	t.Setenv(EnvCodex, "relative")
	if _, err := (Lookup{}).Dir(event.ProviderCodex); !errors.Is(err, ErrRelativeOverride) {
		t.Fatalf("relative: %v", err)
	}
}

// FuzzDir checks that, whatever an override holds, Dir never panics and
// returns either an absolute path or an error, never both or neither.
func FuzzDir(f *testing.F) {
	for _, seed := range append([]string{"", "/abs", "/abs/../x", ":memory:", "\x00"}, relativeForms...) {
		f.Add(seed)
	}
	home := filepath.Join(f.TempDir(), "home")
	f.Fuzz(func(t *testing.T, value string) {
		for _, p := range event.Providers() {
			for _, name := range EnvNames() {
				l := lookup(home, map[string]string{name: value})
				dir, err := l.Dir(p)
				if (err == nil) == (dir == "") || (err == nil && !filepath.IsAbs(dir)) {
					t.Fatalf("%s with %s=%q: Dir = %q, %v", p, name, value, dir, err)
				}
				var overrideErr *OverrideError
				if errors.As(err, &overrideErr) && err.Error() != overrideErr.Name+" must be an absolute path" {
					t.Fatalf("%s with %s=%q: error %q is not the fixed message", p, name, value, err)
				}
			}
			db, err := lookup(home, map[string]string{EnvOpenCode: value}).OpenCodeDatabase()
			if err == nil && db != "" && !filepath.IsAbs(db) {
				t.Fatalf("OPENCODE_DB=%q: database %q is not absolute", value, db)
			}
		}
	})
}
