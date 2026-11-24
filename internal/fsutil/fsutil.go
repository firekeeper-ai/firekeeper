// Package fsutil holds file helpers whose behavior differs on Windows. It is
// standard library only.
package fsutil

import (
	"io"
	"os"
)

// FileExists reports whether path names a regular file (following
// symlinks). A folder, a broken link, or a path it cannot stat is not one.
func FileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// ReadFile is os.ReadFile, opened with Open.
func ReadFile(name string) ([]byte, error) {
	f, err := Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
