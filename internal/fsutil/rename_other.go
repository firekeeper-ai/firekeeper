//go:build !windows

package fsutil

import "os"

// RenameReplacing renames from over to. Unix replaces a file that is open, so
// this is os.Rename.
func RenameReplacing(from, to string) error { return os.Rename(from, to) }
