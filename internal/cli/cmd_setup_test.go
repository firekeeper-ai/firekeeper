package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/firekeeper-ai/firekeeper/internal/config"
	"github.com/firekeeper-ai/firekeeper/internal/daemon"
	"github.com/firekeeper-ai/firekeeper/internal/transcript"
)

func TestFirstRunSetup(t *testing.T) {
	home := t.TempDir()
	previous := configHome
	configHome = func() (string, error) { return home, nil }
	t.Cleanup(func() { configHome = previous })
	stubService(t, "linux")
	path := config.DefaultPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	const original = "# keep this comment\nproviders = [\"kimi\"]\ntoken = \"sensitive-fixture\"\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code, ok := RunSubcommand([]string{"setup", "--status"}, &out, &out); !ok || code != 0 {
		t.Fatalf("status failed: %d", code)
	}
	if strings.Contains(out.String(), "\x1b") || strings.Contains(out.String(), "sensitive-fixture") || !strings.Contains(out.String(), "kimi (file)") {
		t.Fatal("status omitted provider or exposed credential")
	}
	previousEditor := openSetupEditor
	t.Cleanup(func() { openSetupEditor = previousEditor })
	opens := 0
	var keys []tea.KeyMsg
	openSetupEditor = func(m *setupEditor, _ io.Reader, _ io.Writer) (*setupEditor, error) {
		opens++
		for _, k := range keys {
			_, cmd := m.Update(k)
			if cmd != nil {
				break
			}
		}
		return m, nil
	}
	key := func(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
	keys = []tea.KeyMsg{key(tea.KeySpace), key(tea.KeyEsc), {Type: tea.KeyRunes, Runes: []rune("n")}}
	if err := firstRunSetup(strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(setupMarker(home)); err != nil {
		t.Fatal("discard should finish onboarding", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != original {
		t.Fatal("cancel modified config")
	}
	if err := firstRunSetup(strings.NewReader(""), &out); err != nil || opens != 1 {
		t.Fatal("second launch should skip setup", err)
	}
	keys = []tea.KeyMsg{key(tea.KeySpace), key(tea.KeyDown), key(tea.KeyDown), key(tea.KeySpace), key(tea.KeyDown), key(tea.KeySpace)}
	for i := 0; i < 4; i++ {
		keys = append(keys, key(tea.KeyDown))
	}
	keys = append(keys, key(tea.KeyEnter), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1m")}, key(tea.KeyCtrlS))
	if err := machineSetup(strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(configFlags{})
	if err != nil || c.Interval != time.Minute || providerSummary(c.Providers) != "codex, claude" || c.Token != "sensitive-fixture" {
		t.Fatal("saved preferences did not round-trip")
	}
	out.Reset()
	if err := firstRunSetup(strings.NewReader(""), &out); err != nil || out.Len() != 0 {
		t.Fatal("completed setup prompted again")
	}
	keys = []tea.KeyMsg{key(tea.KeySpace), key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyDown), key(tea.KeySpace), key(tea.KeyCtrlS)}
	if err := machineSetup(strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	c, err = loadConfig(configFlags{})
	if err != nil || len(c.Providers) != 0 || c.Sources["providers"] != "file" {
		t.Fatal("explicit none failed")
	}
}

func TestSetupDestination(t *testing.T) {
	for _, raw := range []string{"https://user:secret@example.org", "https://example.org/?token=secret", "invalid"} {
		if got := setupDestination(raw); got != "(invalid URL)" {
			t.Fatal("unsafe destination displayed")
		}
	}
}

func TestSetupEditorControls(t *testing.T) {
	m := newSetupEditor(config.Config{Interval: 15 * time.Second}, "Destination\nhttps://example.org", true)
	for _, w := range []int{36, 84} {
		m.Update(tea.WindowSizeMsg{Width: w, Height: 22})
		lines := strings.Split(m.View(), "\n")
		if len(lines) != 22 {
			t.Fatalf("height %d", len(lines))
		}
		for _, l := range lines {
			if ansi.StringWidth(l) > w {
				t.Fatalf("overflow at width %d", w)
			}
		}
	}
	click := func(index int) {
		t.Helper()
		for _, h := range m.layout().hits {
			if h.index == index {
				m.Update(tea.MouseMsg{X: h.x + 1, Y: h.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
				return
			}
		}
		t.Fatalf("missing control %d", index)
	}
	click(1)
	if providerSummary(m.selected) != "copilot" {
		t.Fatal("click did not toggle provider")
	}
	click(len(m.options))
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1s")})
	click(m.saveIndex())
	if m.saved || !m.editing || m.notice == "" {
		t.Fatal("invalid interval accepted")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	click(m.saveIndex())
	if !m.saved || m.interval != 15*time.Second {
		t.Fatal("valid settings did not save")
	}
}

func TestSetupProviderSources(t *testing.T) {
	c := config.Config{Interval: time.Minute}
	r := daemon.Reporting{Installed: true, ProvidersPinned: true, Providers: []transcript.Provider{transcript.ProviderCodex}}
	var out bytes.Buffer
	v := setupView{Writer: &out}
	setupDetails(v, c, r, "Stopped · no background uploads", "config.toml", "Not installed")
	c.Sources = map[string]string{"providers": "default"}
	editor := newSetupEditor(c, out.String(), false)
	editor.seedService(r)
	if !strings.Contains(editor.View(), fmt.Sprintf("1 of %d selected", len(transcript.Providers()))) || !strings.Contains(out.String(), "Installed providers: codex") {
		t.Fatal("setup must distinguish reporting defaults from service providers")
	}
	out.Reset()
	r.ProvidersPinned = false
	r.Providers = nil
	setupDetails(v, c, r, "Stopped · no background uploads", "config.toml", "Not installed")
	if strings.Contains(out.String(), "Pinned at install") || !strings.Contains(out.String(), "Reporting defaults: none") {
		t.Fatal("inherited defaults shown as pinned")
	}
}

func TestSetupInstallStartRefresh(t *testing.T) {
	_, runner, _ := stubService(t, "linux")
	running := false
	runner.reply = func(line string) (string, error) {
		switch {
		case strings.Contains(line, "daemon-reload"), strings.Contains(line, " enable "):
			return "", nil
		case strings.Contains(line, " restart "), strings.Contains(line, " start "):
			running = true
		case strings.Contains(line, " show ") && running:
			return "LoadState=loaded\nActiveState=active\nSubState=running\n", nil
		}
		return "", nil
	}
	withConfigFile(t, "# fixture")
	c, err := loadConfig(configFlags{})
	if err != nil {
		t.Fatal(err)
	}
	m := newSetupEditor(c, "", false)
	m.configPath = config.DefaultPath(testConfigHome)
	m.selected = []transcript.Provider{transcript.ProviderCodex}
	key := func(value string) {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)})
		if cmd != nil {
			m.Update(cmd())
		}
	}
	if m.plan != nil || len(runner.calls) > 0 {
		t.Fatal("opening setup should not initiate installation")
	}
	key("d")
	if m.plan == nil || !strings.Contains(m.View(), "codex") || running {
		t.Fatal("missing install preview")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if running || m.service.Installed {
		t.Fatal("cancel installed service")
	}
	key("d")
	key("y")
	if !m.service.Installed || !m.service.Running || !strings.Contains(m.View(), "Daemon: Running") {
		t.Fatal("confirmed install failed: " + m.notice)
	}
	running = false
	key("r")
	if !strings.Contains(m.View(), "Start daemon") {
		t.Fatal("stopped service cannot be started")
	}
	key("d")
	key("y")
	if !running || !m.service.Running {
		t.Fatal("installed service did not start")
	}
}

func TestSetupObservesDelayedDaemonStart(t *testing.T) {
	_, runner, _ := stubService(t, "linux")
	runner.reply = func(line string) (string, error) {
		return "LoadState=loaded\nActiveState=active\nSubState=running\n", nil
	}
	m := newSetupEditor(config.Config{}, "", false)
	if m.Init() == nil {
		t.Fatal("no initial status timer")
	}
	m.Update(setupDaemonResult{status: daemon.Reporting{Installed: true}, startAccepted: true})
	if !strings.Contains(m.View(), "Starting…") || m.daemonAction() != nil {
		t.Fatal("accepted start must wait for process")
	}
	_, read := m.Update(setupPollTick{})
	if read == nil || m.readDaemonStatus(false) != nil {
		t.Fatal("expected one outstanding check")
	}
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if len(m.selected) != 1 {
		t.Fatal("status check blocked editing")
	}
	_, next := m.Update(read())
	if next == nil || !strings.Contains(m.View(), "Daemon: Running") || !m.startDeadline.IsZero() {
		t.Fatal("automatic check missed running daemon")
	}
	m.Update(setupStatusRead{generation: m.statusGeneration + 1})
	if !m.service.Running {
		t.Fatal("stale status overwrote running daemon")
	}
	m.startDeadline = time.Now().Add(-time.Second)
	m.Update(setupStatusRead{status: daemon.Reporting{Installed: true}, generation: m.statusGeneration})
	if m.notice != startupUnconfirmed || !strings.Contains(m.View(), "Installed · stopped") {
		t.Fatal("startup timeout hidden")
	}
	m.Update(setupStatusRead{status: daemon.Reporting{Running: true}, generation: m.statusGeneration})
	if m.notice != "" {
		t.Fatal("stale timeout after daemon started")
	}
}

func TestSetupSaveMigratesRunningService(t *testing.T) {
	withConfigFile(t, "# preserve\n")
	_, runner, _ := stubService(t, "linux")
	c, err := loadConfig(configFlags{})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := newService([]string{"--quiet", "--provider", "codex", "--provider", "claude", "--interval", "1m"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.reply = func(line string) (string, error) {
		return "LoadState=loaded\nActiveState=active\nSubState=running\n", nil
	}
	runner.calls = nil
	m := newSetupEditor(c, "", false)
	m.configPath = config.DefaultPath(testConfigHome)
	m.service = inspectSetupDaemon(c)
	m.seedService(m.service)
	if len(m.selected) != 2 || m.interval != time.Minute {
		t.Fatal("legacy selection missing")
	}
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if err := m.savePreferences(); err != nil {
		t.Fatal(err)
	}
	saved, err := loadConfig(configFlags{})
	if err != nil {
		t.Fatal(err)
	}
	actual := inspectSetupDaemon(saved)
	if providerSummary(saved.Providers) != "claude" || providerSummary(actual.Providers) != "claude" || actual.ProvidersPinned || actual.IntervalOverride != 0 || !slices.Contains(runner.calls, "systemctl --user restart firekeeper.service") {
		t.Fatal("daemon did not adopt saved reporting options")
	}
	m.selected = nil
	if err := m.savePreferences(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(svc.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("empty selection left reporting service installed")
	}
}

func TestSetupRestartRequiresChanges(t *testing.T) {
	m := newSetupEditor(config.Config{
		Providers: []transcript.Provider{transcript.ProviderCodex, transcript.ProviderClaude},
		Interval:  15 * time.Second,
	}, "", false)
	m.service = daemon.Reporting{Installed: true, Running: true}
	assertRestart := func(want bool) {
		t.Helper()
		view := m.View()
		if strings.Contains(view, "No changes to save") == want || strings.Contains(view, "[ Exit ]") || strings.Contains(view, "[ Cancel ]") != want {
			t.Fatalf("clean footer visibility should be %t", !want)
		}
		if strings.Contains(view, "Save & restart daemon") != want {
			t.Fatalf("restart visibility should be %t", want)
		}
	}
	assertRestart(false)
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	assertRestart(true)
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	assertRestart(false)
	if err := m.savePreferences(); err != nil {
		t.Fatalf("saving reverted preferences should skip config and daemon updates: %v", err)
	}
	m.focus = len(m.options)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("30s")})
	assertRestart(true)
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assertRestart(false)
}

func TestSetupEscapeReviewsChanges(t *testing.T) {
	for _, key := range []string{"y", "n"} {
		t.Run(key, func(t *testing.T) {
			m := newSetupEditor(config.Config{
				Providers: []transcript.Provider{transcript.ProviderCodex},
				Interval:  15 * time.Second,
			}, "", false)
			esc := tea.KeyMsg{Type: tea.KeyEsc}
			if _, cmd := m.Update(esc); cmd == nil || m.confirmSave {
				t.Fatal("unchanged settings should exit directly")
			}
			m.Update(tea.KeyMsg{Type: tea.KeySpace})
			m.interval = 30 * time.Second
			if _, cmd := m.Update(esc); cmd != nil || !m.confirmSave {
				t.Fatal("pending changes should open confirmation")
			}
			for _, want := range []string{"Do you want to save the changes?", "- Codex Enabled -> Disabled", "- Reporting interval 15s -> 30s"} {
				if !strings.Contains(m.View(), want) {
					t.Errorf("missing confirmation text %q", want)
				}
			}
			if _, cmd := m.Update(esc); cmd != nil || m.confirmSave || !m.preferencesChanged() {
				t.Fatal("Escape from confirmation should return to pending edits")
			}
			m.Update(esc)
			if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}); cmd == nil || m.saved != (key == "y") {
				t.Fatalf("unexpected confirmation result for %q", key)
			}
		})
	}
}

func TestSetupPromptClickAfterScrolling(t *testing.T) {
	for _, width := range []int{40, 90} {
		m := newSetupEditor(config.Config{}, "Details", true)
		m.width, m.height = width, 18
		m.scroll = m.layout().maxScroll
		clicked := 0
		for _, hit := range m.layout().hits {
			if hit.index != setupPromptHit {
				continue
			}
			row := strings.Split(m.View(), "\n")[hit.y]
			if !strings.Contains(row, "\x1b[4m") {
				t.Fatalf("missing underline at width %d", width)
			}
			_, cmd := m.Update(tea.MouseMsg{X: hit.end - 1, Y: hit.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if cmd == nil {
				t.Fatalf("click missed prompt at width %d", width)
			}
			clicked++
		}
		if clicked < 2 {
			t.Fatalf("expected wrapped clickable prompt at width %d", width)
		}
		m.Update(setupPromptCopied{})
		if !strings.Contains(m.View(), "Prompt copied to clipboard.") {
			t.Fatal("missing copy confirmation")
		}
	}
}
