package homebrew

import (
	"path/filepath"
	"strings"
	"testing"
)

// The paths below do not exist in the test (or resolve to themselves), so
// only the path rules decide; homebrew_unix_test.go covers symlinks.

func TestInstalled(t *testing.T) {
	for path, want := range map[string]bool{
		// Kegs under the default and a custom prefix.
		"/opt/homebrew/Cellar/firekeeper/0.2.1/bin/firekeeper":                     true,
		"/usr/local/Cellar/firekeeper/0.2.1/bin/firekeeper":                        true,
		"/home/linuxbrew/.linuxbrew/Cellar/firekeeper/0.2.1/bin/firekeeper":        true,
		"/nonexistent/custom/Cellar/firekeeper-server/1.0.0/bin/firekeeper-server": true,
		"/nonexistent/Cellar/fk/1/bin/../bin/fk":                                   true,
		// A keg holding its own Cellar folder is still the outer keg.
		"/nonexistent/Cellar/fk/1/share/Cellar/x": true,

		// Not inside a keg.
		"/nonexistent/home/me/.local/bin/firekeeper": false,
		"/nonexistent/usr/local/bin/firekeeper":      false,
		"/nonexistent/Cellar/firekeeper/0.2.1":       false, // the keg itself, not a file in it
		"/nonexistent/Cellar/firekeeper":             false,
		"/nonexistent/Cellar":                        false,
		"/Cellar/firekeeper/0.2.1/bin/firekeeper":    false, // no prefix before Cellar
		"/nonexistent/Cellarium/fk/1/bin/fk":         false,
		"/nonexistent/MyCellar/fk/1/bin/fk":          false,
		"/nonexistent/cellar/fk/1/bin/fk":            false,
		"/nonexistent/Cellar/fk/1/../../../bin/fk":   false, // cleans to outside the Cellar
		// A hand-copied binary in a prefix is not Homebrew's install.
		"/nonexistent/homebrew/bin/firekeeper":  false,
		"/nonexistent/linuxbrew/bin/firekeeper": false,
		"":                                      false,
	} {
		if got := Installed(filepath.FromSlash(path)); got != want {
			t.Errorf("Installed(%q) = %v; want %v", path, got, want)
		}
	}
}

func TestInPrefix(t *testing.T) {
	t.Setenv(EnvPrefix, "") // only the defaults; TestInPrefixHonorsEnvPrefix covers the variable
	for dir, want := range map[string]bool{
		"/opt/homebrew":                                   true,
		"/opt/homebrew/bin":                               true,
		"/opt/homebrew/bin/":                              true,
		"/home/linuxbrew/.linuxbrew/bin":                  true,
		"/usr/local/Cellar/firekeeper/0.2.1/bin":          true,
		"/usr/local/Cellar/firekeeper/0.2.1":              true,
		"/nonexistent/custom/Cellar/firekeeper/1/libexec": true,

		// Intel's /usr/local is shared; only its Cellar is Homebrew's.
		"/nonexistent/usr/local/bin": false,
		// Whole folder names only.
		"/opt/homebrew-old/bin":               false,
		"/opt/homebrewx":                      false,
		"/nonexistent/src/homebrew/bin":       false,
		"/nonexistent/home/me/linuxbrew/bin":  false,
		"/nonexistent/home/linuxbrew/bin":     false,
		"/nonexistent/Cellar/firekeeper":      false,
		"/nonexistent/Cellarium/firekeeper/1": false,
		"/nonexistent/home/me/.local/bin":     false,
		"":                                    false,
	} {
		if got := InPrefix(filepath.FromSlash(dir)); got != want {
			t.Errorf("InPrefix(%q) = %v; want %v", dir, got, want)
		}
	}
}

// FuzzPathRules checks the rules on any clean slash path: no panic, and a
// file Homebrew installed always lies in a folder that is Homebrew's.
func FuzzPathRules(f *testing.F) {
	for _, seed := range []string{
		"/opt/homebrew/Cellar/fk/1/bin/fk", "/opt/homebrew/bin", "/usr/local/bin",
		"/Cellar/a/b/c", "Cellar/a/b", "", "/", "//Cellar//a", "/x/Cellar/../Cellar/a/b/c",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		p := filepath.ToSlash(filepath.Clean(raw))
		installed, kegDir := inKeg(p), inKegDir(p)
		if installed && !kegDir {
			t.Fatalf("%q is in a keg but its folder is not", p)
		}
		if installed && !inKegDir(p[:strings.LastIndex(p, "/")]) {
			t.Fatalf("%q is in a keg but its parent folder is not", p)
		}
		if d := cellarDepth(p); d < -1 || (d >= 0 && !strings.Contains(p, "/Cellar")) {
			t.Fatalf("cellarDepth(%q) = %d", p, d)
		}
		if underPrefix(p, nil) {
			t.Fatalf("%q is under a prefix from an empty list", p)
		}
		if !underPrefix(p, []string{p}) {
			t.Fatalf("%q is not under itself", p)
		}
	})
}

// setHome points the home folder at dir on every platform.
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func TestInPrefixHonorsEnvPrefix(t *testing.T) {
	setHome(t, t.TempDir())
	prefix := filepath.Join(t.TempDir(), "brew")
	bin := filepath.Join(prefix, "bin")

	t.Setenv(EnvPrefix, "")
	if InPrefix(bin) {
		t.Fatal("a custom prefix counts without HOMEBREW_PREFIX")
	}
	for name, value := range map[string]string{
		"plain":          prefix,
		"trailing slash": prefix + string(filepath.Separator),
		"spaces":         "  " + prefix + "\n",
		"dot segments":   filepath.Join(prefix, "bin", ".."),
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(EnvPrefix, value)
			for _, dir := range []string{prefix, bin, filepath.Join(prefix, "sbin")} {
				if !InPrefix(dir) {
					t.Errorf("InPrefix(%q) = false with %s=%q", dir, EnvPrefix, value)
				}
			}
			// Whole folders only, and the defaults still count.
			if InPrefix(prefix+"-old") || InPrefix(filepath.Dir(prefix)) {
				t.Errorf("a folder beside or above %s counts as Homebrew's", EnvPrefix)
			}
			if !InPrefix(filepath.FromSlash("/opt/homebrew/bin")) {
				t.Error("the default prefix stopped counting")
			}
			// The variable says where Homebrew is, not what it installed.
			if Installed(filepath.Join(bin, "firekeeper")) {
				t.Error("a file in the custom prefix counts as Homebrew's install")
			}
		})
	}
}

func TestInPrefixIgnoresUnusableEnvPrefix(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	for name, value := range map[string]string{
		"relative":   filepath.Join("brew"),
		"dot":        ".",
		"blank":      "   ",
		"root":       string(filepath.Separator),
		"home":       home,
		"usr":        filepath.FromSlash("/usr"),
		"usr local":  filepath.FromSlash("/usr/local"),
		"usr local/": filepath.FromSlash("/usr/local/"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(EnvPrefix, value)
			for _, dir := range []string{
				filepath.Join(home, "bin"),
				filepath.FromSlash("/usr/local/bin"),
				filepath.FromSlash("/usr/bin"),
				filepath.Join("brew", "bin"),
			} {
				if InPrefix(dir) {
					t.Errorf("InPrefix(%q) = true with %s=%q", dir, EnvPrefix, value)
				}
			}
			// Kegs still count whatever the variable says.
			if !InPrefix(filepath.FromSlash("/usr/local/Cellar/firekeeper/1/bin")) {
				t.Error("the Intel Cellar stopped counting")
			}
		})
	}
}
