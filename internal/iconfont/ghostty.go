package iconfont

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The logo glyphs sit at U+E9A0 to U+E9A6; scripts/icon-font/build.mjs
// assigns them.
const (
	FirstCodepoint = 0xE9A0
	LastCodepoint  = 0xE9A6
)

const (
	ghosttyMarker = "# Firekeeper logo font (firekeeper install-font --remove undoes this)"
	ghosttyLine   = "font-codepoint-map = U+E9A0-U+E9A6=FirekeeperIcons"
)

// GhosttyConfigPath returns the Ghostty config file to edit. Ghostty reads
// both $XDG_CONFIG_HOME/ghostty/config and, on macOS, the Application Support
// one; an existing file wins, otherwise the XDG path is used. A relative or
// unexpanded $XDG_CONFIG_HOME is ignored and a relative home is unusable, as
// in UserDir; with neither usable it returns "".
func GhosttyConfigPath(goos, home, xdgConfig string, exists func(string) bool) string {
	if !isAbs(goos, home) {
		home = ""
	}
	if !isAbs(goos, xdgConfig) {
		if home == "" {
			return ""
		}
		xdgConfig = filepath.Join(home, ".config")
	}
	xdg := filepath.Join(xdgConfig, "ghostty", "config")
	if goos == "darwin" && home != "" {
		support := filepath.Join(home, "Library", "Application Support", "com.mitchellh.ghostty", "config")
		if !exists(xdg) && exists(support) {
			return support
		}
	}
	return xdg
}

// ConfigureGhostty makes Ghostty draw the logo codepoints from FirekeeperIcons.
// It maps only those codepoints, so the user's own font stays the main one,
// and appends to the file so a symlinked or hand-written config is kept. A
// config that already has the mapping line, ours or the user's own copy, is
// left alone. It reports whether the file changed.
func ConfigureGhostty(path string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read Ghostty config: %w", err)
	}
	for _, line := range splitLines(string(existing)) {
		if strings.TrimSpace(line) == ghosttyLine {
			return false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create Ghostty config folder: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return false, fmt.Errorf("write Ghostty config: %w", err)
	}
	if _, err := file.WriteString(ghosttyBlock(existing)); err != nil {
		file.Close()
		return false, fmt.Errorf("write Ghostty config: %w", err)
	}
	if err := file.Close(); err != nil {
		return false, fmt.Errorf("write Ghostty config: %w", err)
	}
	return true, nil
}

// ghosttyBlock is what ConfigureGhostty appends to existing: a blank line to
// separate it from the user's lines, then the marker and the mapping line.
func ghosttyBlock(existing []byte) string {
	block := ghosttyMarker + "\n" + ghosttyLine + "\n"
	if len(existing) > 0 {
		if !bytes.HasSuffix(existing, []byte("\n")) {
			block = "\n" + block
		}
		block = "\n" + block
	}
	return block
}

// UnconfigureGhostty removes what ConfigureGhostty added, reporting whether
// the file changed. It removes only exact blocks, the marker line directly
// followed by the mapping line, and the blank line ConfigureGhostty put before
// one; a lone or indented copy of either line is the user's and stays. The
// file is replaced atomically, so a failed write never leaves it truncated. A
// missing file is not an error.
func UnconfigureGhostty(path string) (bool, error) {
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read Ghostty config: %w", err)
	}
	updated, changed := removeGhosttyBlocks(string(existing))
	if !changed {
		return false, nil
	}
	if err := replaceFile(path, []byte(updated)); err != nil {
		return false, fmt.Errorf("write Ghostty config: %w", err)
	}
	return true, nil
}

// removeGhosttyBlocks drops every exact block ConfigureGhostty wrote from
// config, keeping every other byte, line endings included.
func removeGhosttyBlocks(config string) (string, bool) {
	lines := strings.SplitAfter(config, "\n")
	kept := make([]string, 0, len(lines))
	changed := false
	for i := 0; i < len(lines); i++ {
		if i+1 < len(lines) && lineText(lines[i]) == ghosttyMarker && lineText(lines[i+1]) == ghosttyLine {
			if n := len(kept); n > 0 && lineText(kept[n-1]) == "" && strings.HasSuffix(kept[n-1], "\n") {
				kept = kept[:n-1]
			}
			i++
			changed = true
			continue
		}
		kept = append(kept, lines[i])
	}
	return strings.Join(kept, ""), changed
}

// lineText is line without its \n or \r\n ending.
func lineText(line string) string {
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
}

func splitLines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

// replaceFile writes data to path through a temporary file in the same
// folder as the real file, then renames it over the real file. A symlink at
// path is kept and its target replaced, and the target's permission bits are
// copied. A read-only file is refused, as writing it in place would be.
func replaceFile(path string, data []byte) error {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(real)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o200 == 0 {
		return fmt.Errorf("%s is read-only", filepath.Base(real))
	}
	temp, err := os.CreateTemp(filepath.Dir(real), "."+filepath.Base(real)+".firekeeper-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temp.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(temp.Name(), real)
}
