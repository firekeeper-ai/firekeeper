package iconfont

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readConfig(t *testing.T, path string) string {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

func TestGhosttyRoundTripKeepsUserBytes(t *testing.T) {
	for _, original := range []string{
		"",
		"keep = 1\n\n\n",
		"a = 1\r\nb = 2\r\n",
		"# TODO FirekeeperIcons later\n",
		"font-codepoint-map = U+E000-U+E001=Nerd\n",
		ghosttyMarker + "\n",
	} {
		t.Run(original, func(t *testing.T) {
			path := writeConfig(t, original)
			if changed, err := ConfigureGhostty(path); err != nil || !changed {
				t.Fatalf("configure = %v, %v", changed, err)
			}
			if changed, err := UnconfigureGhostty(path); err != nil || !changed {
				t.Fatalf("unconfigure = %v, %v", changed, err)
			}
			if got := readConfig(t, path); got != original {
				t.Fatalf("round trip = %q, want %q", got, original)
			}
		})
	}
}

func TestGhosttyLeavesUserCopiesAlone(t *testing.T) {
	for _, config := range []string{
		"  " + ghosttyLine + "\n",
		ghosttyLine + "\n",
		ghosttyMarker + "\n",
		ghosttyMarker + "\nother = 1\n" + ghosttyLine + "\n",
		"  " + ghosttyMarker + "\n" + ghosttyLine + "\n",
	} {
		t.Run(config, func(t *testing.T) {
			path := writeConfig(t, config)
			if changed, err := UnconfigureGhostty(path); err != nil || changed {
				t.Fatalf("unconfigure = %v, %v", changed, err)
			}
			if got := readConfig(t, path); got != config {
				t.Fatalf("config = %q", got)
			}
		})
	}
	path := writeConfig(t, "  "+ghosttyLine+"\n")
	if changed, err := ConfigureGhostty(path); err != nil || changed {
		t.Fatalf("configure with the user's own line = %v, %v", changed, err)
	}
}

func TestGhosttyRemovesEveryExactBlock(t *testing.T) {
	block := ghosttyMarker + "\n" + ghosttyLine + "\n"
	path := writeConfig(t, "a = 1\n\n"+block+"b = 2\n\n"+strings.ReplaceAll(block, "\n", "\r\n"))
	if changed, err := UnconfigureGhostty(path); err != nil || !changed {
		t.Fatalf("unconfigure = %v, %v", changed, err)
	}
	if got := readConfig(t, path); got != "a = 1\nb = 2\n" {
		t.Fatalf("config = %q", got)
	}
}

func TestUnconfigureGhosttyKeepsSymlinkAndMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles-config")
	link := filepath.Join(dir, "config")
	if err := os.WriteFile(target, []byte("a = b\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	before, _ := os.Stat(target)
	if _, err := ConfigureGhostty(link); err != nil {
		t.Fatal(err)
	}
	if changed, err := UnconfigureGhostty(link); err != nil || !changed {
		t.Fatalf("unconfigure = %v, %v", changed, err)
	}
	if info, _ := os.Lstat(link); info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced")
	}
	if got := readConfig(t, target); got != "a = b\n" {
		t.Fatalf("target = %q", got)
	}
	if after, _ := os.Stat(target); after.Mode().Perm() != before.Mode().Perm() {
		t.Fatalf("mode %v, want %v", after.Mode().Perm(), before.Mode().Perm())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("left %d files behind", len(entries))
	}
}

func TestUnconfigureGhosttyRefusesReadOnlyFile(t *testing.T) {
	original := "a = 1\n\n" + ghosttyMarker + "\n" + ghosttyLine + "\n"
	path := writeConfig(t, original)
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o644) })
	if changed, err := UnconfigureGhostty(path); err == nil || changed {
		t.Fatalf("unconfigure = %v, %v; want an error", changed, err)
	}
	if got := readConfig(t, path); got != original {
		t.Fatalf("config = %q", got)
	}
}

func FuzzRemoveGhosttyBlocks(f *testing.F) {
	f.Add("")
	f.Add("a = 1")
	f.Add("keep = 1\n\n\n")
	f.Add("a\r\n" + ghosttyMarker + "\r\n" + ghosttyLine)
	f.Fuzz(func(t *testing.T, config string) {
		if strings.Contains(config, ghosttyMarker) {
			return
		}
		if got, changed := removeGhosttyBlocks(config); changed || got != config {
			t.Fatalf("changed a config without our block: %q", got)
		}
		want := config
		if want != "" && !strings.HasSuffix(want, "\n") {
			want += "\n"
		}
		got, changed := removeGhosttyBlocks(config + ghosttyBlock([]byte(config)))
		if !changed || got != want {
			t.Fatalf("round trip of %q = %q, %v; want %q", config, got, changed, want)
		}
	})
}
