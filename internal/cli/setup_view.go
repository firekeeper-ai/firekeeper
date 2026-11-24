package cli

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// setupView uses terminal palette colors, so the user's theme controls contrast.
// Labels and selection marks also carry meaning in plain-text output.
type setupView struct {
	io.Writer
	color bool
}

func newSetupView(out io.Writer) setupView {
	f, ok := out.(*os.File)
	return setupView{out, ok && term.IsTerminal(int(f.Fd())) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"}
}

const (
	setupAccent  = "1;33"
	setupHeading = "1;36"
	setupMuted   = "2"
	setupGood    = "32"
	setupBad     = "31"
)

func (v setupView) paint(style, text string) string {
	if !v.color {
		return text
	}
	return "\x1b[" + style + "m" + text + "\x1b[0m"
}
func (v setupView) shortcut(key, action string) string {
	return v.paint("1", key) + v.paint(setupMuted, " "+action)
}

func (v setupView) section(title string) {
	fmt.Fprintln(v, "\n"+v.paint(setupHeading, title))
}
func (v setupView) statusSection(title, status, style string) {
	fmt.Fprintln(v, "\n"+v.paint(setupHeading, title+" · ")+v.paint(style, status))
}

func (v setupView) row(label, value string) {
	fmt.Fprintf(v, "  %s  %s\n", v.paint(setupMuted, fmt.Sprintf("%-12s", label)), value)
}
