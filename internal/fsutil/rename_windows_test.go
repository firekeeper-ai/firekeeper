//go:build windows

package fsutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A destination held open for a moment, as a virus scanner holds a file it
// is checking, is replaced once it is closed.
func TestWindowsRenameReplacingWaitsForAnOpenFile(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "new"), filepath.Join(dir, "old")
	if err := os.WriteFile(from, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(to) // os.Open does not share delete, so rename fails
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(from, to); err == nil {
		held.Close()
		t.Fatal("os.Rename replaced a file held open; the test no longer reproduces the lock")
	}
	time.AfterFunc(100*time.Millisecond, func() { held.Close() })

	if err := RenameReplacing(from, to); err != nil {
		t.Fatalf("RenameReplacing: %v", err)
	}
	if got, _ := os.ReadFile(to); string(got) != "new" {
		t.Fatalf("destination = %q, want new", got)
	}
}

// A file held open for longer than the retries still fails, with the error.
func TestWindowsRenameReplacingGivesUp(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "new"), filepath.Join(dir, "old")
	if err := os.WriteFile(from, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(to)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := RenameReplacing(from, to); err == nil {
		t.Fatal("RenameReplacing replaced a file that stayed open")
	}
	if got, _ := os.ReadFile(from); string(got) != "new" {
		t.Fatalf("source = %q, want it left in place", got)
	}
}
