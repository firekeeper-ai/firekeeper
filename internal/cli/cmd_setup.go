package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/config"
	"github.com/firekeeper-ai/firekeeper/internal/daemon"
	"github.com/firekeeper-ai/firekeeper/internal/reporter"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
	"github.com/firekeeper-ai/firekeeper/internal/transcript/claudecloud"
	"golang.org/x/term"
)

func setupInteractive() bool {
	return os.Getenv("CI") == "" && os.Getenv(NoPromptEnv) == "" && defaultInstallEnv.interactive() && term.IsTerminal(int(os.Stdout.Fd()))
}

func setupMarker(home string) string { return filepath.Join(home, ".firekeeper", "setup-complete") }

// FirstRunSetup offers setup once before the interactive dashboard. A config
// or service failure must never prevent someone from using the local dashboard.
func FirstRunSetup(stdout, stderr io.Writer) {
	if !setupInteractive() {
		return
	}
	if err := firstRunSetup(os.Stdin, stdout); err != nil {
		fmt.Fprintf(stderr, "firekeeper setup: %v (retry with fk setup)\n", err)
	}
}

func firstRunSetup(input io.Reader, out io.Writer) error {
	home, err := configHome()
	if err != nil {
		return err
	}
	_, err = os.Stat(setupMarker(home))
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return machineSetup(input, out, false)
}

func runSetup(args []string, stdout, stderr io.Writer) int {
	const name = "firekeeper setup"
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	status := fs.Bool("status", false, "show settings and daemon status without prompting")
	if code, ok := parseNoArgs(name, fs, args, stderr); !ok {
		return code
	}
	var input io.Reader
	if !*status && setupInteractive() {
		input = os.Stdin
	}
	if err := machineSetup(input, stdout, true); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return 1
	}
	return 0
}

func machineSetup(input io.Reader, out io.Writer, manageDaemon bool) error {
	c, err := loadConfig(configFlags{})
	if err != nil {
		return err
	}
	home, err := configHome()
	if err != nil {
		return err
	}
	path, err := config.ResolvePath(home, nil)
	if err != nil {
		return err
	}
	r := inspectSetupDaemon(c)
	v := newSetupView(out)
	if input != nil {
		displayPath := path
		if rel, err := filepath.Rel(home, path); err == nil && filepath.IsLocal(rel) {
			displayPath = filepath.Join("~", rel)
		}
		model := newSetupEditor(c, "", v.color)
		model.manageDaemon = manageDaemon
		model.displayPath = displayPath
		model.configPath = path
		model.seedService(r)
		model.setDaemon(r)
		result, err := openSetupEditor(model, input, out)
		if err != nil || !result.saved {
			return err
		}
		if err := result.savePreferences(); err != nil {
			return err
		}
		if err := completeSetup(home); err != nil {
			return err
		}
		fmt.Fprintln(out, "Setup saved.")
		return nil
	}
	fmt.Fprintln(v, v.paint(setupAccent, "FIREKEEPER")+"  /  Machine setup")
	v.section("Reporting defaults")
	v.row("Providers", providerSummary(c.Providers)+" ("+c.Sources["providers"]+")")
	v.row("Interval", c.Interval.String()+" ("+c.Sources["interval"]+")")
	setupDetails(v, c, r, daemonState(r), path)
	return nil
}

// Adopt an existing service's settings when no preference has been saved.
func (m *setupEditor) seedService(r daemon.Reporting) {
	if r.ProvidersPinned && m.original.Sources["providers"] == "default" {
		m.selected = slices.Clone(r.Providers)
	}
	if r.IntervalOverride > 0 && m.original.Sources["interval"] == "default" {
		m.interval = r.IntervalOverride
	}
}

func (m *setupEditor) preferencesChanged() bool {
	return !slices.Equal(m.selected, m.original.Providers) || m.interval != m.original.Interval
}

func (m *setupEditor) savePreferences() error {
	if m.service.Installed && !m.manageDaemon {
		return nil
	}
	if !m.service.Installed && !m.preferencesChanged() && !m.providersTouched {
		return nil
	}
	if err := saveSetupConfig(m.configPath, m.original, m.selected, m.interval); err != nil {
		return err
	}
	if m.service.Installed {
		svc, err := newService(nil, io.Discard)
		if err != nil {
			return err
		}
		if len(m.selected) == 0 && !m.original.TelemetryEnabled {
			err = svc.Uninstall(context.Background(), false)
		} else {
			err = svc.ApplyPreferences(context.Background(), m.service.Running && !m.service.Foreground, setupEnvironmentFlags(m.original, m.selected, m.interval))
		}
		if err != nil {
			return fmt.Errorf("preferences saved; daemon update failed. Check fk daemon status and retry from setup: %w", safeServiceError(err))
		}
	}
	return nil
}

func safeServiceError(err error) error {
	var ce *daemon.CommandError
	if errors.As(err, &ce) {
		return fmt.Errorf("%s exited %d", ce.Name, ce.Code)
	}
	return err
}

func saveSetupConfig(path string, c config.Config, selected []transcript.Provider, interval time.Duration) error {
	var providers []transcript.Provider
	if c.Sources["providers"] != "env" {
		providers = append([]transcript.Provider{}, selected...)
	}
	if c.Sources["interval"] == "env" {
		interval = 0
	}
	if providers == nil && interval == 0 {
		return nil
	}
	return config.SaveReportingPreferences(path, providers, interval)
}

func setupEnvironmentFlags(c config.Config, providers []transcript.Provider, interval time.Duration) []string {
	var args []string
	if c.Sources["providers"] == "env" {
		for _, p := range providers {
			args = append(args, "--provider", string(p))
		}
	}
	if c.Sources["interval"] == "env" {
		args = append(args, "--interval", interval.String())
	}
	return args
}

func setupDetails(v setupView, c config.Config, r daemon.Reporting, state, path string) {
	v.section("Destination")
	fmt.Fprintln(v, setupDestination(c.Server))
	credential := "No credential configured"
	if c.Token != "" {
		credential = "Credential configured"
	}
	fmt.Fprintln(v, v.paint(setupMuted, credential))
	v.section("Background daemon")
	if r.Running {
		state = v.paint(setupGood, state)
	}
	fmt.Fprintln(v, state)
	if r.Installed {
		interval := c.Interval
		if r.IntervalOverride > 0 {
			interval = r.IntervalOverride
		}
		fmt.Fprintln(v, "Installed service · "+interval.String())
		if r.ProvidersPinned {
			fmt.Fprintf(v, "Installed providers: %s\n", providerSummary(r.Providers))
			fmt.Fprintln(v, v.paint(setupMuted, "Save settings to switch this service to your reporting selection."))
		} else {
			fmt.Fprintln(v, "Reporting defaults: "+providerSummary(r.Providers))
		}
		if r.Server != c.Server {
			fmt.Fprintln(v, setupDestination(r.Server))
		}
	}
	if c.ClaudeCloud {
		interval := c.ClaudeCloudInterval
		if interval == 0 {
			interval = claudecloud.DefaultPollInterval
		}
		fmt.Fprintf(v, "Claude cloud · %s with Claude reporting\n", interval)
	}
	v.section("Local dashboard")
	fmt.Fprintln(v, "All providers · 2s discovery")
	fmt.Fprintf(v, "%q\n", path)
	v.section("Reporting commands")
	fmt.Fprintln(v, "fk login [--server URL]\nfk daemon install")
}

func completeSetup(home string) error {
	marker := setupMarker(home)
	if err := os.MkdirAll(filepath.Dir(marker), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}

func providerSummary(providers []transcript.Provider) string {
	if len(providers) == 0 {
		return "none"
	}
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = string(p)
	}
	return strings.Join(names, ", ")
}

func setupDestination(raw string) string {
	server, err := reporter.CleanServer(raw)
	if err != nil {
		return "(invalid URL)"
	}
	return server
}
