// Package fsutil holds file helpers whose behavior differs on Windows. It is
// standard library only.
package fsutil

import (
	"io"
)

// ReadFile is os.ReadFile, opened with Open.
func ReadFile(name string) ([]byte, error) {
	f, err := Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
