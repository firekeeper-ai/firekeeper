package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/firekeeper-ai/firekeeper/internal/config"
	"github.com/firekeeper-ai/firekeeper/internal/daemon"
	"github.com/firekeeper-ai/firekeeper/internal/reporter"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
)

func inspectSetupDaemon(c config.Config) daemon.Reporting {
	svc, err := newService(nil, io.Discard)
	if err != nil {
		return daemon.Reporting{StatusUnavailable: true}
	}
	return svc.Inspect(context.Background(), c.Server, c.Providers)
}

func daemonStatusStyle(r daemon.Reporting, state string) string {
	if r.Running || r.Foreground {
		return setupGood
	}
	if state == "Starting…" || r.StatusUnavailable {
		return setupAccent
	}
	return setupBad
}

func daemonState(r daemon.Reporting) string {
	switch {
	case r.Foreground:
		return "Running in foreground"
	case r.Running:
		return "Running"
	case r.StatusUnavailable:
		return "Status unavailable"
	case r.Installed:
		return "Installed · stopped"
	default:
		return "Not installed"
	}
}

func (m *setupEditor) setDaemon(r daemon.Reporting) {
	m.service = r
	var b bytes.Buffer
	setupDetails(setupView{Writer: &b, color: m.color}, m.original, r, m.displayDaemonState(), m.displayPath, m.serverStatus)
	m.details = b.String()
}

type setupDaemonPlan struct {
	start     bool
	providers []transcript.Provider
	interval  time.Duration
	server    string
}

func installedDaemonPlan(c config.Config, r daemon.Reporting) setupDaemonPlan {
	interval := c.Interval
	if r.IntervalOverride > 0 {
		interval = r.IntervalOverride
	}
	return setupDaemonPlan{start: true, providers: r.Providers, server: r.Server, interval: interval}
}

func (p setupDaemonPlan) action() string {
	if p.start {
		return "Start daemon"
	}
	return "Install daemon"
}

type setupDaemonResult struct {
	status        daemon.Reporting
	plan          *setupDaemonPlan
	notice        string
	startAccepted bool
	installed     *setupDaemonPlan
}

func (m *setupEditor) daemonAction() tea.Cmd {
	if !m.startDeadline.IsZero() || m.service.Running || m.service.StatusUnavailable {
		return nil
	}
	if !m.acceptField() {
		return nil
	}
	c := m.original
	plan := setupDaemonPlan{start: m.service.Installed, providers: slices.Clone(m.selected), interval: m.interval, server: c.Server}
	if plan.start && (!slices.Equal(m.selected, m.service.Providers) || m.interval != installedDaemonPlan(c, m.service).interval) {
		m.notice = "Save settings to apply your selection before starting."
		return nil
	}
	m.statusGeneration++
	m.busy = "Checking daemon…"
	return func() tea.Msg {
		r := inspectSetupDaemon(c)
		msg := setupDaemonResult{status: r}
		if r.Running || r.StatusUnavailable || r.Installed != plan.start {
			msg.notice = "Daemon status changed. Review its status."
			return msg
		}
		if plan.start {
			plan = installedDaemonPlan(c, r)
		}
		if !plan.start && len(plan.providers) == 0 {
			msg.notice = "Select a reporting provider to install the daemon."
			return msg
		}
		if _, err := reporter.CleanServer(plan.server); err != nil {
			msg.notice = "Invalid destination. Check your reporting configuration."
			return msg
		}
		if !plan.start {
			svc, err := newService(nil, io.Discard)
			if err != nil {
				msg.notice = "Cannot locate the Firekeeper executable."
				return msg
			}
			if svc.TemporaryExecutable() {
				msg.notice = "Run fk setup from an installed binary (go run is temporary)."
				return msg
			}
		}
		msg.plan = &plan
		return msg
	}
}

func (m *setupEditor) confirmDaemon() tea.Cmd {
	p := *m.plan
	m.plan = nil
	m.statusGeneration++
	m.busy = "Starting daemon…"
	path := m.configPath
	c := m.original
	return func() tea.Msg {
		r := inspectSetupDaemon(c)
		accepted := false
		notice := "Daemon status changed. Review its status."
		if !r.Running && !r.StatusUnavailable && r.Installed == p.start {
			if p.start {
				current := installedDaemonPlan(c, r)
				if !slices.Equal(current.providers, p.providers) || current.server != p.server || current.interval != p.interval {
					return setupDaemonResult{status: r, notice: "Service settings changed. Review before starting."}
				}
				svc, err := newService(nil, io.Discard)
				if err == nil {
					err = svc.Start(context.Background())
				}
				accepted = err == nil
				if err != nil {
					var commandErr *daemon.CommandError
					switch {
					case errors.Is(err, daemon.ErrMissingBrewExecutable):
						notice = err.Error()
					case errors.As(err, &commandErr):
						notice = fmt.Sprintf("Start failed: %s exited %d. See fk daemon status.", commandErr.Name, commandErr.Code)
					case errors.Is(err, context.DeadlineExceeded):
						notice = "Start timed out. Refresh daemon status."
					default:
						notice = "Start failed. Check fk daemon status and fk daemon logs."
					}
				}
			} else {
				if err := saveSetupConfig(path, c, p.providers, p.interval); err != nil {
					return setupDaemonResult{status: r, notice: "Could not save reporting preferences."}
				}
				args := append([]string{"--server", p.server}, setupEnvironmentFlags(c, p.providers, p.interval)...)
				if runDaemonInstall(args, io.Discard, io.Discard) != 0 {
					notice = "Preferences saved; install failed. Check fk daemon status."
				} else {
					accepted = true
				}
			}
		}
		result := setupDaemonResult{notice: notice, startAccepted: accepted}
		if !p.start && accepted {
			result.installed = &p
			c.Providers = p.providers
			c.Interval = p.interval
		}
		result.status = inspectSetupDaemon(c)
		return result
	}
}

type setupPollTick struct{}
type setupStatusRead struct {
	status     daemon.Reporting
	generation uint64
	periodic   bool
}

func nextSetupPoll() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return setupPollTick{} })
}

// A periodic read schedules the next tick after it finishes. Manual refreshes
// share the in-flight guard and never start another polling loop.
func (m *setupEditor) readDaemonStatus(periodic bool) tea.Cmd {
	if m.statusPending {
		return nil
	}
	m.statusPending = true
	c, generation := m.original, m.statusGeneration
	return func() tea.Msg { return setupStatusRead{inspectSetupDaemon(c), generation, periodic} }
}

func (m *setupEditor) displayDaemonState() string {
	if !m.startDeadline.IsZero() && !m.service.Running {
		return "Starting…"
	}
	return daemonState(m.service)
}

const startupUnconfirmed = "Daemon startup unconfirmed. Check fk daemon logs."

func (m *setupEditor) applyObservedStatus(r daemon.Reporting) {
	if r.Running {
		m.startDeadline = time.Time{}
		if m.notice == startupUnconfirmed {
			m.notice = ""
		}
	} else if !m.startDeadline.IsZero() && !time.Now().Before(m.startDeadline) {
		m.startDeadline = time.Time{}
		m.notice = startupUnconfirmed
	}
	m.setDaemon(r)
}

func (m *setupEditor) daemonDialog() setupLayout {
	p := m.plan
	text := fmt.Sprintf("%s?\n\nProviders   %s\nInterval    %s\nDestination %s\n\nStarts background reporting of session metadata and redacted transcripts.", p.action(), providerSummary(p.providers), p.interval, setupDestination(p.server))
	if !p.start {
		text += " The service also starts at login."
	} else {
		text += " Uses installed service settings."
	}
	lines := strings.Split(ansi.Wrap(text, max(1, m.width-4), ""), "\n")
	layout := setupLayout{lines: []string{" " + m.paint(setupHeading, "Background reporting")}}
	for _, line := range lines {
		layout.lines = append(layout.lines, "  "+line)
	}
	layout.lines = layout.lines[:min(len(layout.lines), max(1, m.height-2))]
	layout.lines = append(layout.lines, "")
	confirm := "[ Y: " + strings.ToLower(p.action()) + " ]"
	layout.hits = []setupHit{{2, 2 + len(confirm), len(layout.lines), 0}, {4 + len(confirm), 12 + len(confirm), len(layout.lines), 1}}
	layout.lines = append(layout.lines, "  "+m.paint(setupAccent, confirm)+"  [ Back ]")
	return layout
}
