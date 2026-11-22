//go:build !windows

package update

// systemDirectory has no meaning off Windows, where the installer never runs.
func systemDirectory() string { return "" }
