// Package iconfont embeds FirekeeperIcons.ttf, the font holding the harness
// logos the dashboard draws, and installs it into the user's font folder.
//
// The font is built from scripts/icon-font; rebuilding it there refreshes the
// embedded copy. Installing only ever writes the one file, inside the user's
// own font folder, and only when asked to.
package iconfont

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the font file's name in the user's font folder.
const FileName = "FirekeeperIcons.ttf"

//go:embed FirekeeperIcons.ttf
var data []byte

// Data returns a copy of the embedded font.
func Data() []byte { return bytes.Clone(data) }

// ErrUnsupported means the platform has no per-user font folder Firekeeper
// can write to without registry changes.
var ErrUnsupported = errors.New("installing fonts is not supported on this platform")

// UserDir returns the per-user font folder for goos. home is the user's home
// directory; xdgData is $XDG_DATA_HOME, which may be empty. A relative or
// unexpanded (~/x) $XDG_DATA_HOME is ignored, as the XDG Base Directory spec
// says, and a relative home is refused: either would put the font under the
// current directory.
func UserDir(goos, home, xdgData string) (string, error) {
	if home == "" {
		return "", errors.New("no home directory")
	}
	if !isAbs(goos, home) {
		return "", errors.New("home directory is not an absolute path")
	}
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Fonts"), nil
	case "linux", "freebsd", "openbsd", "netbsd":
		if !isAbs(goos, xdgData) {
			xdgData = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(xdgData, "fonts"), nil
	default:
		return "", fmt.Errorf("%w (%s)", ErrUnsupported, goos)
	}
}

// isAbs reports whether p is absolute on goos, whatever this program runs on:
// a drive or UNC path on Windows, a path from / elsewhere.
func isAbs(goos, p string) bool {
	if goos == "windows" {
		return len(p) >= 3 && isDriveLetter(p[0]) && p[1] == ':' && isSlash(p[2]) ||
			len(p) >= 3 && isSlash(p[0]) && isSlash(p[1]) && !isSlash(p[2])
	}
	return strings.HasPrefix(p, "/")
}

func isDriveLetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }
func isSlash(c byte) bool       { return c == '/' || c == '\\' }

// Installed reports whether dir holds the current font.
func Installed(dir string) bool {
	existing, err := os.ReadFile(filepath.Join(dir, FileName))
	return err == nil && bytes.Equal(existing, data)
}

// Install writes the font into dir, creating it when needed, and returns the
// file's path. It replaces an older copy and reports whether anything changed.
func Install(dir string) (path string, changed bool, err error) {
	path = filepath.Join(dir, FileName)
	if Installed(dir) {
		return path, false, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, fmt.Errorf("create font folder: %w", err)
	}
	temp, err := os.CreateTemp(dir, FileName+".tmp*")
	if err != nil {
		return "", false, fmt.Errorf("write font: %w", err)
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return "", false, fmt.Errorf("write font: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", false, fmt.Errorf("write font: %w", err)
	}
	if err := os.Chmod(temp.Name(), 0o644); err != nil {
		return "", false, fmt.Errorf("write font: %w", err)
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return "", false, fmt.Errorf("install font: %w", err)
	}
	return path, true, nil
}

// Remove deletes the installed font from dir, reporting whether one existed.
func Remove(dir string) (bool, error) {
	err := os.Remove(filepath.Join(dir, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("remove font: %w", err)
	}
	return true, nil
}
