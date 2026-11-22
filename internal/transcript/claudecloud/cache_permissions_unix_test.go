//go:build !windows

package claudecloud

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncProtectsCacheDirectory(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new"
		if existing {
			name = "existing"
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "cache")
			if existing {
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			_, c := newFakeCloud(t)
			syncOnce(t, c, dir, time.Time{})
			info, err := os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o700 {
				t.Fatalf("cache directory mode = %04o, want 0700", got)
			}
		})
	}
}
