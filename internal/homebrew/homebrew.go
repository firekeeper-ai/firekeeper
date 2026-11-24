// Package homebrew recognizes paths Homebrew owns. It is standard library
// only.
//
// Homebrew installs a formula into a keg, <prefix>/Cellar/<formula>/<version>,
// and symlinks its bin directory into <prefix>/bin. The default prefixes are
// /opt/homebrew (Apple Silicon), /usr/local (Intel), and
// /home/linuxbrew/.linuxbrew (Linux); the Cellar is <prefix>/Cellar, also for
// custom prefixes. A custom prefix is known from HOMEBREW_PREFIX, which
// `brew shellenv` exports. See https://docs.brew.sh/Installation and
// https://docs.brew.sh/Formula-Cookbook.
package homebrew

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// EnvPrefix is the variable `brew shellenv` exports with Homebrew's prefix,
// so a shell set up for Homebrew names a custom prefix such as ~/brew too.
const EnvPrefix = "HOMEBREW_PREFIX"

// prefixes are the default Homebrew prefixes that belong to Homebrew alone.
// /usr/local (Intel) is shared with other software, so only its Cellar counts.
var prefixes = []string{"/opt/homebrew", "/home/linuxbrew/.linuxbrew"}

// shared are folders other software installs into too. A HOMEBREW_PREFIX
// naming one of them, or the home folder, is ignored, so only its Cellar
// counts, as for Intel's /usr/local.
var shared = []string{"/", "/usr", "/usr/local"}

// Installed reports whether Homebrew installed the file at path: after
// symlinks are resolved, it lies inside a keg, <prefix>/Cellar/<formula>/
// <version>/. Such a file is updated with brew, never replaced in place. A
// file copied by hand into <prefix>/bin is not Homebrew's.
func Installed(path string) bool { return inKeg(resolve(path)) }

// InPrefix reports whether dir belongs to Homebrew: after symlinks are
// resolved, it is a keg or lies inside one, or lies in a default Homebrew
// prefix or the one HOMEBREW_PREFIX names. Nothing else writes files there.
func InPrefix(dir string) bool {
	p := resolve(dir)
	return inKegDir(p) || underPrefix(p, knownPrefixes())
}

// knownPrefixes are the default prefixes plus HOMEBREW_PREFIX when it names
// a prefix of Homebrew's own: an absolute path that is not a shared folder
// or the home folder. Anything else in the variable is ignored.
func knownPrefixes() []string {
	value := strings.TrimSpace(os.Getenv(EnvPrefix))
	if value == "" || !filepath.IsAbs(value) {
		return prefixes
	}
	prefix := resolve(value)
	if slices.Contains(shared, prefix) {
		return prefixes
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && prefix == resolve(home) {
		return prefixes
	}
	return append(slices.Clip(prefixes), prefix)
}

// resolve is path with symlinks resolved, cleaned and in slash form. A path
// that cannot be resolved (it does not exist, or a link is broken) is only
// cleaned.
func resolve(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return filepath.ToSlash(filepath.Clean(path))
}

// inKeg reports whether the slash path p names something inside a keg: a
// Cellar folder followed by a formula, a version, and at least one more name.
func inKeg(p string) bool { return cellarDepth(p) >= 3 }

// inKegDir reports whether the slash path p is a keg or lies inside one.
func inKegDir(p string) bool { return cellarDepth(p) >= 2 }

// cellarDepth is the number of names after the first Cellar folder in the
// clean slash path p, or -1 when there is none. The first one is the keg's,
// so a keg that holds its own Cellar folder still counts. Matching whole
// names keeps look-alikes such as /src/Cellarium/ out; a Cellar at the root,
// with no prefix before it, is not Homebrew's.
func cellarDepth(p string) int {
	names := strings.Split(p, "/")
	for i := 1; i < len(names); i++ {
		if names[i] == "Cellar" && names[i-1] != "" {
			return len(names) - 1 - i
		}
	}
	return -1
}

// underPrefix reports whether the clean slash path p is one of the clean
// slash prefixes or lies inside one, matched as whole folders so
// /opt/homebrew-old is not.
func underPrefix(p string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}
