package daemon

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// brewCellar returns the Cellar only for the Firekeeper formula's executable.
func brewCellar(exe string) string {
	bin := filepath.Dir(exe)
	version := filepath.Dir(bin)
	formula := filepath.Dir(version)
	cellar := filepath.Dir(formula)
	if !filepath.IsAbs(exe) || filepath.Base(exe) != "firekeeper" || filepath.Base(bin) != "bin" || filepath.Base(formula) != "firekeeper" || filepath.Base(cellar) != "Cellar" {
		return ""
	}
	return cellar
}

// The opt symlink survives brew upgrades. Only accept it when it resolves
// back into this prefix's Firekeeper formula. A new install must use the
// same binary; a repair may move to a newer version of the formula.
func brewExecutable(exe string, upgrade bool) string {
	cellar := brewCellar(exe)
	if cellar == "" {
		return ""
	}
	stable := filepath.Join(filepath.Dir(cellar), "opt", "firekeeper", "bin", "firekeeper")
	resolved, err := filepath.EvalSymlinks(stable)
	if err != nil || brewCellar(resolved) != cellar || (!upgrade && resolved != exe) {
		return ""
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return ""
	}
	return stable
}

// repairLaunchdExecutable migrates only a removed Homebrew executable in
// Firekeeper's installed plist. Preserve every other byte, including flags,
// provider-home overrides and any local launchd settings.
func (s *Service) repairLaunchdExecutable(ctx context.Context) (bool, error) {
	f, err := os.Open(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} // a loaded job can outlive its file
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() > 1<<20 {
		return false, errors.New("service definition exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return false, err
	}
	args, err := plistProgramArgs(data)
	if err != nil {
		return false, nil
	} // launchd also accepts binary plists; only migrate our XML format
	if len(args) < 2 || args[1] != "daemon" || brewCellar(args[0]) == "" {
		return false, nil
	}
	if _, err := os.Stat(args[0]); !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	stable := brewExecutable(args[0], true)
	if stable == "" {
		return false, ErrMissingBrewExecutable
	}
	old := []byte("<string>" + xmlEscape(args[0]) + "</string>")
	if bytes.Count(data, old) != 1 {
		return false, errors.New("cannot safely update LaunchAgent executable")
	}
	updated := bytes.Replace(data, old, []byte("<string>"+xmlEscape(stable)+"</string>"), 1)
	// Unload the old job before committing the replacement: if bootstrap
	// subsequently fails, Start can retry from the repaired definition.
	if s.launchdLoaded(ctx) {
		if err := s.run(ctx, "launchctl", "bootout", s.target()); err != nil {
			return false, err
		}
	}
	if err := writeFileAtomic(s.Path(), updated, info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

// ErrMissingBrewExecutable means an obsolete Cellar path has no usable replacement.
var ErrMissingBrewExecutable = errors.New("Firekeeper Homebrew executable missing; reinstall the formula")
