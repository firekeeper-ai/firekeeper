package update

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/firekeeper-ai/firekeeper/internal/fsutil"
)

func TestWindowsShellUsesRealSystemDirectory(t *testing.T) {
	dir := systemDirectory()
	if dir == "" {
		t.Fatal("GetSystemDirectory returned nothing")
	}
	got, err := windowsShell(dir, fsutil.FileExists, func(string) (string, error) { return "", exec.ErrNotFound })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(filepath.Dir(filepath.Dir(filepath.Dir(got))), dir) {
		t.Fatalf("shell %q is not under %q", got, dir)
	}
}
