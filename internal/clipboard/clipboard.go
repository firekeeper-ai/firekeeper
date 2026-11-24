package clipboard

import (
	"github.com/charmbracelet/x/ansi"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Copy uses the platform clipboard tool when there is one and otherwise
// asks the terminal to set the clipboard (OSC 52), which also works over SSH.
func Copy(text string) error {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"pbcopy"}}
	case "linux":
		candidates = [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
	}
	for _, args := range candidates {
		path, err := exec.LookPath(args[0])
		if err != nil {
			continue
		}
		command := exec.Command(path, args[1:]...)
		command.Stdin = strings.NewReader(text)
		if command.Run() == nil {
			return nil
		}
	}
	_, err := os.Stdout.WriteString(ansi.SetSystemClipboard(text))
	return err
}
