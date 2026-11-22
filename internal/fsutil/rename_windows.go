//go:build windows

package fsutil

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// RenameReplacing renames from over to. Windows refuses to replace a file
// that another process has open, which a virus scanner or indexer does for a
// moment after every write or run, so it retries briefly while the error is
// a sharing or access violation.
func RenameReplacing(from, to string) error {
	var err error
	for attempt := 1; ; attempt++ {
		err = os.Rename(from, to)
		if err == nil || attempt == renameAttempts || !transientRenameError(err) {
			return err
		}
		time.Sleep(time.Duration(attempt) * renameBackoff)
	}
}

// renameAttempts and renameBackoff wait up to about two seconds in all.
const (
	renameAttempts = 20
	renameBackoff  = 10 * time.Millisecond
)

// errorSharingViolation is ERROR_SHARING_VIOLATION, which syscall does not
// name.
const errorSharingViolation syscall.Errno = 32

func transientRenameError(err error) bool {
	return errors.Is(err, errorSharingViolation) || errors.Is(err, syscall.ERROR_ACCESS_DENIED)
}
