package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/firekeeper-ai/firekeeper/internal/clipboard"
	"github.com/firekeeper-ai/firekeeper/internal/config"
	"github.com/firekeeper-ai/firekeeper/internal/daemon"
	"github.com/firekeeper-ai/firekeeper/internal/session"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
)

type setupEditor struct {
	setupView
	service                                     daemon.Reporting
	configPath, displayPath, serverStatus, busy string
	statusPending                               bool
	statusGeneration                            uint64
	startDeadline                               time.Time
	manageDaemon                                bool
	plan                                        *setupDaemonPlan
	options, selected                           []transcript.Provider
	original                                    config.Config
	interval                                    time.Duration
	details                                     string
	width, height, focus, scroll, confirmScroll int
	editing, replace, saved, confirmSave        bool
	field, notice                               string
}

func newSetupEditor(c config.Config, details string, color bool) *setupEditor {
	return &setupEditor{setupView: setupView{color: color}, original: c, options: transcript.Providers(), selected: slices.Clone(c.Providers), interval: c.Interval, details: details, width: 80, height: 24}
}

var openSetupEditor = func(m *setupEditor, in io.Reader, out io.Writer) (*setupEditor, error) {
	_, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return m, err
}

func (m *setupEditor) Init() tea.Cmd  { return nextSetupPoll() }
func (m *setupEditor) saveIndex() int { return len(m.options) + 1 }
func (m *setupEditor) acceptField() bool {
	if !m.editing {
		return true
	}
	d, err := time.ParseDuration(m.field)
	if err != nil || d < daemon.MinInterval {
		m.notice = "Enter a duration of at least " + daemon.MinInterval.String()
		return false
	}
	m.interval = d
	m.editing = false
	m.notice = ""
	return true
}

func (m *setupEditor) activate() tea.Cmd {
	switch m.focus {
	case m.saveIndex():
		if m.acceptField() {
			m.saved = true
			return tea.Quit
		}
	case m.saveIndex() + 1:
		return tea.Quit
	case len(m.options):
		if m.service.Installed && !m.manageDaemon {
			m.notice = "Open fk setup to edit daemon settings."
			return nil
		}
		if m.original.Sources["interval"] == "env" {
			m.notice = "Managed by " + config.EnvInterval
			return nil
		}
		if m.editing {
			m.acceptField()
		} else {
			m.editing = true
			m.replace = true
			m.field = m.interval.String()
		}
	default:
		if m.service.Installed && !m.manageDaemon {
			m.notice = "Open fk setup to edit daemon settings."
			return nil
		}
		if m.original.Sources["providers"] == "env" {
			m.notice = "Managed by " + config.EnvProviders
			return nil
		}
		p := m.options[m.focus]
		if i := slices.Index(m.selected, p); i >= 0 {
			m.selected = slices.Delete(m.selected, i, i+1)
		} else {
			m.selected = append(m.selected, p)
		}
	}
	return nil
}

const setupPromptHit = -3

type setupPromptCopied struct{ err error }

func copySetupPrompt() tea.Msg {
	return setupPromptCopied{clipboard.Copy(setupAgentPrompt)}
}

func (m *setupEditor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch event := msg.(type) {
	case setupPromptCopied:
		m.notice = "Prompt copied to clipboard."
		if event.err != nil {
			m.notice = "Could not copy prompt to clipboard."
		}
		return m, nil
	case setupPollTick:
		if m.statusPending || m.busy != "" || m.plan != nil {
			return m, nextSetupPoll()
		}
		return m, m.readDaemonStatus(true)
	case setupStatusRead:
		m.statusPending = false
		if event.generation == m.statusGeneration && m.busy == "" {
			m.applyObservedStatus(event.status)
		}
		if event.periodic {
			return m, nextSetupPoll()
		}
		return m, nil
	}
	if result, ok := msg.(setupDaemonResult); ok {
		if result.installed != nil {
			m.original.Providers = slices.Clone(result.installed.providers)
			m.original.Interval = result.installed.interval
		}
		m.busy = ""
		m.startDeadline = time.Time{}
		if result.startAccepted && !result.status.Running {
			m.startDeadline = time.Now().Add(15 * time.Second)
		}
		m.applyObservedStatus(result.status)
		m.plan = result.plan
		m.notice = result.notice
		if result.startAccepted {
			m.notice = ""
		}
		return m, nil
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		return m, nil
	}
	if m.busy != "" {
		return m, nil
	}
	if m.confirmSave {
		return m, m.updateSaveDialog(msg)
	}
	if m.plan != nil {
		switch event := msg.(type) {
		case tea.KeyMsg:
			switch event.String() {
			case "y", "Y":
				return m, m.confirmDaemon()
			case "esc", "n", "ctrl+c":
				m.plan = nil
			}
		case tea.MouseMsg:
			if event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress {
				for _, hit := range m.daemonDialog().hits {
					if event.Y == hit.y && event.X >= hit.x && event.X < hit.end {
						if hit.index == 0 {
							return m, m.confirmDaemon()
						}
						m.plan = nil
						break
					}
				}
			}
		}
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonWheelDown || msg.Button == tea.MouseButtonWheelUp {
			delta := 3
			if msg.Button == tea.MouseButtonWheelUp {
				delta = -3
			}
			m.scroll = max(0, min(m.layout().maxScroll, m.scroll+delta))
			break
		}
		if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
			break
		}
		for _, hit := range m.layout().hits {
			if msg.Y == hit.y && msg.X >= hit.x && msg.X < hit.end {
				if hit.index == setupPromptHit {
					return m, copySetupPrompt
				}
				if hit.index == -1 {
					return m, m.daemonAction()
				}
				if hit.index == -2 {
					return m, m.readDaemonStatus(false)
				}
				if hit.index != m.saveIndex()+1 && hit.index != m.focus && !m.acceptField() {
					break
				}
				m.focus = hit.index
				m.notice = ""
				return m, m.activate()
			}
		}
	case tea.KeyMsg:
		key := msg.String()
		switch {
		case !m.editing && key == "p":
			return m, copySetupPrompt
		case !m.editing && key == "d":
			return m, m.daemonAction()
		case !m.editing && key == "r":
			return m, m.readDaemonStatus(false)
		case key == "ctrl+c":
			return m, tea.Quit
		case key == "esc" && m.editing:
			m.editing = false
			m.notice = ""
		case key == "esc":
			if m.preferencesChanged() {
				m.confirmSave = true
				m.confirmScroll = 0
				break
			}
			return m, tea.Quit
		case key == "ctrl+s" || (!m.editing && key == "s"):
			if m.acceptField() {
				m.saved = true
				return m, tea.Quit
			}
		case key == "tab" || key == "shift+tab" || key == "up" || key == "down":
			if !m.acceptField() {
				break
			}
			step := 1
			if key == "up" || key == "shift+tab" {
				step = -1
			}
			count := m.saveIndex() + 2
			m.focus = (m.focus + step + count) % count
			m.notice = ""
			m.reveal()
		case key == "pgdown" || key == "pgup":
			delta := max(1, m.height-6)
			if key == "pgup" {
				delta = -delta
			}
			m.scroll = max(0, min(m.layout().maxScroll, m.scroll+delta))
		case key == "enter" || (!m.editing && key == " "):
			m.notice = ""
			return m, m.activate()
		case m.editing:
			switch {
			case key == "backspace":
				if m.replace {
					m.field = ""
				} else if r := []rune(m.field); len(r) > 0 {
					m.field = string(r[:len(r)-1])
				}
				m.replace = false
			case msg.Type == tea.KeyRunes:
				if m.replace {
					m.field = ""
					m.replace = false
				}
				if len(m.field)+len(string(msg.Runes)) <= 32 && strings.IndexFunc(string(msg.Runes), func(r rune) bool { return r < ' ' || r == 127 }) < 0 {
					m.field += string(msg.Runes)
				}
			}
		}
	}
	return m, nil
}

type setupHit struct{ x, end, y, index int }
type setupLayout struct {
	lines     []string
	hits      []setupHit
	maxScroll int
}

const setupHeaderRows = 4

func (m *setupEditor) reveal() {
	if m.focus > len(m.options) {
		return
	}
	row := 2 + m.focus
	if m.focus == len(m.options) {
		row += 2
	}
	m.scroll = min(m.scroll, row)
	m.scroll = max(m.scroll, row-max(1, m.height-setupHeaderRows-3)+1)
}

func setupFit(s string, width int) string {
	s = ansi.Truncate(s, width, "…")
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}

func (m *setupEditor) layout() setupLayout {
	w := max(1, m.width)
	wide := w >= 62
	left := w
	if wide {
		left = min(40, w/2)
	}
	content := max(1, left-4)
	line := func(s string) string { return "  " + setupFit(s, content) }
	section := func(s string) string { return line(m.paint(setupHeading, s)) }
	rows := []string{section("Reporting providers"), line(m.paint(setupMuted, fmt.Sprintf("%d of %d selected", len(m.selected), len(m.options))))}
	if m.original.Sources["providers"] == "env" {
		rows[1] = line("Managed by environment")
	}
	var hits []setupHit
	control := func(index int, value string) {
		hits = append(hits, setupHit{0, left, len(rows), index})
		if m.focus == index {
			value = m.paint("7", " › "+setupFit(value, max(1, content-4))+" ")
		} else {
			value = "   " + value
		}
		rows = append(rows, line(value))
	}
	for i, p := range m.options {
		mark := "[ ]"
		if slices.Contains(m.selected, p) {
			mark = "[✓]"
			if m.focus != i {
				mark = m.paint(setupGood, mark)
			}
		}
		control(i, mark+"  "+session.ToolName(p))
	}
	rows = append(rows, "", section("Reporting interval"))
	field := "[" + m.interval.String() + "]  Edit"
	if m.editing {
		field = "[" + m.field + "▏]"
	} else if m.original.Sources["interval"] == "env" {
		field = m.interval.String() + " · environment"
	}
	control(len(m.options), field)
	rows = append(rows, "")
	for _, s := range strings.Split(ansi.Wrap("Reports include redacted transcripts.", content, ""), "\n") {
		rows = append(rows, line(m.paint(setupMuted, s)))
	}
	detailX, detailY, detailWidth := 2, len(rows)+1, max(1, w-4)
	if wide {
		detailX, detailY, detailWidth = left+3, 0, max(1, w-left-5)
	}
	details := strings.Split(ansi.Wrap(strings.TrimSpace(m.details), detailWidth, ""), "\n")
	details = append(details, "", m.paint(setupHeading, "Ask your coding agent · p Copy"))
	for _, part := range strings.Split(ansi.Wrap(setupAgentPrompt, detailWidth, ""), "\n") {
		hits = append(hits, setupHit{detailX, detailX + ansi.StringWidth(part), detailY + len(details), setupPromptHit})
		details = append(details, m.paint("4", part))
	}
	if wide {
		for len(rows) < len(details) {
			rows = append(rows, "")
		}
		for i := range rows {
			right := ""
			if i < len(details) {
				right = details[i]
			}
			rows[i] = setupFit(rows[i], left) + "│  " + right
		}
	} else {
		rows = append(rows, "")
		for _, s := range details {
			rows = append(rows, "  "+s)
		}
	}
	visible := max(1, m.height-setupHeaderRows-3)
	layout := setupLayout{maxScroll: max(0, len(rows)-visible)}
	start := min(m.scroll, layout.maxScroll)
	title := "  " + m.paint(setupAccent, "FIREKEEPER") + "   /   Machine settings"
	state := m.displayDaemonState()
	badge := "  " + m.paint(setupHeading, "● Daemon: ") + m.paint(daemonStatusStyle(m.service, state), state)
	actions := "  "
	if !m.service.Running && !m.service.StatusUnavailable && m.startDeadline.IsZero() {
		if m.manageDaemon {
			label := "[d Install daemon]"
			if m.service.Installed {
				label = "[d Start daemon]"
			}
			layout.hits = append(layout.hits, setupHit{2, 2 + len(label), 2, -1})
			actions += m.paint(setupHeading, label) + "  "
		} else {
			actions += "Open fk setup to manage · "
		}
	}
	x := ansi.StringWidth(actions)
	refresh := "[r Refresh]"
	layout.hits = append(layout.hits, setupHit{x, x + len(refresh), 2, -2})
	actions += refresh
	layout.lines = []string{title, badge, actions, m.paint(setupMuted, strings.Repeat("─", w))}

	for _, hit := range hits {
		if hit.y >= start && hit.y < start+visible {
			hit.y += setupHeaderRows - start
			layout.hits = append(layout.hits, hit)
		}
	}
	layout.lines = append(layout.lines, rows[start:min(len(rows), start+visible)]...)
	for len(layout.lines) < setupHeaderRows+visible {
		layout.lines = append(layout.lines, "")
	}
	note := m.notice
	if m.busy != "" {
		note = m.busy
	}
	changed := m.preferencesChanged()
	if note == "" {
		note = "No changes to save"
		if changed {
			note = "Unsaved changes"
		}
	}
	layout.lines = append(layout.lines, "  "+m.paint(setupAccent, note))
	buttons := "  "
	saveLabel := " Save settings "
	if !changed {
		saveLabel = " Exit "
	} else if m.manageDaemon && m.service.Installed {
		switch {
		case len(m.selected) == 0 && !m.original.TelemetryEnabled:
			saveLabel = " Save & disable service "
		case m.service.Running && !m.service.Foreground:
			saveLabel = " Save & restart daemon "
		default:
			saveLabel = " Apply settings "
		}
	}
	for i, label := range []string{saveLabel, " Cancel "} {
		idx := m.saveIndex() + i
		x := ansi.StringWidth(buttons)
		style := "1;7;36"
		if i == 1 {
			style = setupMuted
		}
		if m.focus == idx {
			style = "7"
			label = "›" + label[1:]
		}
		buttons += m.paint(style, "["+label+"]") + "  "
		layout.hits = append(layout.hits, setupHit{x, x + len(label) + 2, len(layout.lines), idx})
	}
	hint := "↑↓ Move   Space Toggle   Enter Select   s Save   Esc Review"
	if !changed {
		hint = "↑↓ Move   Space Toggle   Enter Select   s Exit   Esc Exit"
	}
	if m.editing {
		hint = "Type duration   Enter Apply   Esc Revert"
	}
	if layout.maxScroll > 0 {
		hint += "   PgUp / PgDn Scroll"
	}
	layout.lines = append(layout.lines, buttons, "  "+m.paint(setupMuted, hint))
	return layout
}

func (m *setupEditor) View() string {
	var l setupLayout
	if m.confirmSave {
		l = m.saveDialog()
	} else if m.plan != nil {
		l = m.daemonDialog()
	} else {
		l = m.layout()
	}
	for i, s := range l.lines {
		l.lines[i] = ansi.Truncate(s, max(1, m.width), "…")
	}
	return strings.Join(l.lines[:min(len(l.lines), max(1, m.height))], "\n")
}

func (m *setupEditor) updateSaveDialog(msg tea.Msg) tea.Cmd {
	switch event := msg.(type) {
	case tea.KeyMsg:
		switch event.String() {
		case "y", "Y":
			m.saved = true
			return tea.Quit
		case "n", "N", "ctrl+c":
			return tea.Quit
		case "esc":
			m.confirmSave = false
		case "up":
			m.confirmScroll = max(0, min(m.confirmScroll, m.saveDialog().maxScroll)-1)
		case "down":
			m.confirmScroll = min(m.confirmScroll+1, m.saveDialog().maxScroll)
		}
	case tea.MouseMsg:
		if event.Action == tea.MouseActionPress && event.Button == tea.MouseButtonLeft {
			for _, hit := range m.saveDialog().hits {
				if event.Y == hit.y && event.X >= hit.x && event.X < hit.end {
					m.saved = hit.index == 0
					return tea.Quit
				}
			}
		}
	}
	return nil
}

func (m *setupEditor) saveDialog() setupLayout {
	var changes []string
	for _, provider := range m.options {
		before := slices.Contains(m.original.Providers, provider)
		after := slices.Contains(m.selected, provider)
		if before == after {
			continue
		}
		change := "Disabled -> Enabled"
		if before {
			change = "Enabled -> Disabled"
		}
		changes = append(changes, "- "+session.ToolName(provider)+" "+change)
	}
	if m.interval != m.original.Interval {
		changes = append(changes, fmt.Sprintf("- Reporting interval %s -> %s", m.original.Interval, m.interval))
	}

	preview := strings.Split(ansi.Wrap(strings.Join(changes, "\n"), max(1, m.width-4), ""), "\n")
	visible := max(0, m.height-3)
	layout := setupLayout{
		lines:     []string{"  " + m.paint(setupHeading, "Do you want to save the changes?")},
		maxScroll: max(0, len(preview)-visible),
	}
	start := min(m.confirmScroll, layout.maxScroll)
	for _, line := range preview[start:min(start+visible, len(preview))] {
		layout.lines = append(layout.lines, "  "+line)
	}
	const yes, no = "[ Y: Yes ]", "[ N: No ]"
	row := len(layout.lines)
	layout.hits = []setupHit{{2, 2 + len(yes), row, 0}, {4 + len(yes), 4 + len(yes) + len(no), row, 1}}
	layout.lines = append(layout.lines, "  "+m.paint(setupAccent, yes)+"  "+no)
	hint := "  Esc Back"
	if layout.maxScroll > 0 {
		hint += "   ↑↓ Scroll changes"
	}
	layout.lines = append(layout.lines, m.paint(setupMuted, hint))
	return layout
}
