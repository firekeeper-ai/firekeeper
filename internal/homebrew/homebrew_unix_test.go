//go:build !windows

package homebrew

import (
	"os"
	"path/filepath"
	"testing"
)

// brewTree builds <root>/prefix with a keg holding bin/fk and the
// <prefix>/bin/fk symlink into it, the way brew links a formula.
func brewTree(t *testing.T) (prefix, keg string) {
	t.Helper()
	prefix = filepath.Join(t.TempDir(), "prefix")
	keg = filepath.Join(prefix, "Cellar", "fk", "1.2.3")
	if err := os.MkdirAll(filepath.Join(keg, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keg, "bin", "fk"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "Cellar", "fk", "1.2.3", "bin", "fk"), filepath.Join(prefix, "bin", "fk")); err != nil {
		t.Fatal(err)
	}
	return prefix, keg
}

func TestInstalledFollowsBrewSymlink(t *testing.T) {
	prefix, keg := brewTree(t)
	if !Installed(filepath.Join(prefix, "bin", "fk")) {
		t.Error("the <prefix>/bin symlink into a keg is not Homebrew's")
	}
	if !Installed(filepath.Join(keg, "bin", "fk")) {
		t.Error("the file in the keg is not Homebrew's")
	}
	if !InPrefix(filepath.Dir(filepath.Join(keg, "bin", "fk"))) {
		t.Error("the keg's bin folder is not Homebrew's")
	}
}

func TestInstalledRejectsHandCopiedAndBrokenLinks(t *testing.T) {
	prefix, _ := brewTree(t)
	copied := filepath.Join(prefix, "bin", "copied")
	if err := os.WriteFile(copied, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if Installed(copied) {
		t.Error("a file copied into <prefix>/bin counts as Homebrew's install")
	}
	broken := filepath.Join(prefix, "bin", "broken")
	if err := os.Symlink(filepath.Join("..", "Cellar", "gone", "1", "bin", "gone"), broken); err != nil {
		t.Fatal(err)
	}
	if Installed(broken) {
		t.Error("a broken link counts as Homebrew's install")
	}
	// A link from outside into a keg is Homebrew's file; a link from a keg
	// path out to a plain file is not.
	plain := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(plain, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	escaped := filepath.Join(prefix, "Cellar", "fk", "1.2.3", "bin", "escaped")
	if err := os.Symlink(plain, escaped); err != nil {
		t.Fatal(err)
	}
	if Installed(escaped) {
		t.Error("a keg link out to a plain file counts as Homebrew's install")
	}
}

func TestInPrefixResolvesEnvPrefixLink(t *testing.T) {
	setHome(t, t.TempDir())
	real := filepath.Join(t.TempDir(), "brew")
	if err := os.MkdirAll(filepath.Join(real, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "brew-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvPrefix, link)
	if !InPrefix(filepath.Join(real, "bin")) || !InPrefix(filepath.Join(link, "bin")) {
		t.Error("a HOMEBREW_PREFIX link does not cover the folder it points to")
	}
}
