//go:build windows

package fsutil

import (
	"os"
	"syscall"
)

// Open opens name for reading and lets other processes rename, replace, or
// delete it while it is open. os.Open on Windows does not, so a reader of a
// log or state file would make the writer's rotation or atomic replace fail
// with a sharing violation.
func Open(name string) (*os.File, error) {
	path, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	const share = syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE
	handle, err := syscall.CreateFile(path, syscall.GENERIC_READ, share, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	return os.NewFile(uintptr(handle), name), nil
}
