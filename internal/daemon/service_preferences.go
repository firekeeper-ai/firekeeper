package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// ApplyPreferences reloads an installed service after config changes. Remove
// old provider/interval flags but preserve the executable, destination, other
// arguments and service metadata. Stopped services stay stopped.
func (s *Service) ApplyPreferences(ctx context.Context, running bool, envFlags []string) error {
	path := s.Path()
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	args, err := s.serviceCommand(data)
	if err != nil {
		return err
	}
	kept := slices.Clone(args[:2])
	for i := 2; i < len(args); i++ {
		key, _, inline := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		switch key {
		case "provider", "interval":
			if !inline {
				i++
			}
		case "server":
			kept = append(kept, args[i])
			if !inline && i+1 < len(args) {
				i++
				kept = append(kept, args[i])
			}
		default:
			kept = append(kept, args[i])
		}
	}
	kept = append(kept, envFlags...)
	if !slices.Equal(args, kept) {
		before, after := s.commandFragment(args), s.commandFragment(kept)
		if bytes.Count(data, before) != 1 {
			return errors.New("cannot safely update installed service arguments")
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(path, bytes.Replace(data, before, after, 1), info.Mode().Perm()); err != nil {
			return err
		}
	}
	switch s.GOOS {
	case "darwin":
		if s.launchdLoaded(ctx) {
			if err := s.run(ctx, "launchctl", "bootout", s.target()); err != nil {
				return err
			}
		}
		if running {
			return s.bootstrapLaunchd(ctx)
		}
	case "linux":
		if err := s.run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		if running {
			return s.run(ctx, "systemctl", "--user", "restart", SystemdUnit)
		}
	case "windows":
		if running {
			return s.installTask(ctx)
		}
		return s.run(ctx, "schtasks", "/Create", "/TN", s.taskName(), "/XML", path, "/F")
	default:
		return fmt.Errorf("unsupported service manager on %s", s.GOOS)
	}
	return nil
}

func (s *Service) commandFragment(args []string) []byte {
	switch s.GOOS {
	case "windows":
		text := xmlEscape(joinWindowsArgs(args)) + "</Arguments>"
		return encodeUTF16([]byte(text))[2:] // omit byte order mark inside XML
	case "darwin":
		return []byte(plistArgLines(args))
	default:
		return []byte(systemdExecLine(args))
	}
}
