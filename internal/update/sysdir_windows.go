//go:build windows

package update

import "golang.org/x/sys/windows"

// systemDirectory is the Windows system directory, such as C:\Windows\System32,
// or "" when it cannot be read.
func systemDirectory() string {
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		return ""
	}
	return dir
}
