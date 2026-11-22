//go:build unix

package iconfont

import (
	"os"
	"path/filepath"
	"testing"
)

// A failed write must leave the config as it was: the old in-place rewrite
// truncated it first.
func TestUnconfigureGhosttyFailureKeepsConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := filepath.Join(t.TempDir(), "ghostty")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config")
	original := "theme = dark\n\n" + ghosttyMarker + "\n" + ghosttyLine + "\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if changed, err := UnconfigureGhostty(path); err == nil || changed {
		t.Fatalf("unconfigure = %v, %v; want an error", changed, err)
	}
	if got := readConfig(t, path); got != original {
		t.Fatalf("config = %q", got)
	}
}
