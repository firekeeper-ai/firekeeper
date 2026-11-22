package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/firekeeper-ai/firekeeper/internal/claudelimits"
	"github.com/firekeeper-ai/firekeeper/internal/config"
)

// newClaudeLimitsClient is a variable so tests never read a real Keychain.
var newClaudeLimitsClient = func() claudelimits.Client { return claudelimits.Client{} }

const claudeLimitsUsage = "Usage:\n  firekeeper claude-limits enable\n  firekeeper claude-limits disable\n  firekeeper claude-limits status"

// runClaudeLimits turns off, turns back on, or checks the dashboard's Claude
// limit meters. They are on by default: the dashboard reads the sign-in Claude
// Code already stores on this machine and asks Anthropic's usage endpoint, the
// way Claude Code's /usage does, and does nothing when there is no sign-in.
// Nothing is uploaded and nothing is written to Claude Code's files.
func runClaudeLimits(args []string, stdout, stderr io.Writer) int {
	if hasFlag(args, "-h") || hasFlag(args, "--help") {
		fmt.Fprintln(stdout, claudeLimitsUsage+" [--json]")
		return 0
	}
	asJSON := len(args) == 2 && args[0] == "status" && args[1] == "--json"
	if len(args) != 1 && !asJSON {
		fmt.Fprintln(stderr, claudeLimitsUsage)
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "firekeeper claude-limits: find home directory")
		return 1
	}
	cfgPath, err := config.ResolvePath(home, os.Getenv)
	if err != nil {
		fmt.Fprintf(stderr, "firekeeper claude-limits: %v\n", err)
		return 1
	}
	switch args[0] {
	case "enable":
		if err := config.SetClaudeLimits(cfgPath, true); err != nil {
			fmt.Fprintf(stderr, "firekeeper claude-limits: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Claude limits are on (this is the default). The dashboard reads the Claude Code sign-in stored")
		fmt.Fprintln(stdout, "on this machine and ask Anthropic's usage endpoint for your 5-hour and weekly limits.")
		fmt.Fprintln(stdout, "That endpoint is undocumented and may change. The token is used for that request")
		fmt.Fprintln(stdout, "only: it is not stored, refreshed, logged, or uploaded. On macOS the Keychain may")
		fmt.Fprintln(stdout, "ask you to allow access.")
		fmt.Fprintln(stdout, "Turn it off with: firekeeper claude-limits disable")
		return 0
	case "disable":
		if err := config.SetClaudeLimits(cfgPath, false); err != nil {
			fmt.Fprintf(stderr, "firekeeper claude-limits: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Claude limits are off. Firekeeper will not read the Claude Code sign-in.")
		fmt.Fprintln(stdout, "Turn them back on with: firekeeper claude-limits enable")
		return 0
	case "status":
		if asJSON {
			return claudeLimitsStatusJSON(stdout, stderr)
		}
		return claudeLimitsStatus(stdout)
	default:
		fmt.Fprintf(stderr, "firekeeper claude-limits: unknown command %q\n%s\n", args[0], claudeLimitsUsage)
		return 2
	}
}

func claudeLimitsStatusJSON(stdout, stderr io.Writer) int {
	c, err := config.Load(config.Options{Getenv: os.Getenv})
	if err != nil {
		fmt.Fprintf(stderr, "firekeeper claude-limits: config: %v\n", err)
		return 1
	}
	result := map[string]any{"enabled": c.ClaudeLimits, "signed_in": false, "windows": []any{}}
	if c.ClaudeLimits {
		client := newClaudeLimitsClient()
		result["signed_in"] = client.HasToken()
		if client.HasToken() {
			limits, fetchErr := client.Fetch(context.Background())
			if fetchErr != nil {
				result["usage_error"] = fetchErr.Error()
			} else {
				windows := make([]map[string]any, 0, 4)
				for _, item := range []struct {
					name string
					win  *claudelimits.Window
				}{
					{"5-hour", limits.FiveHour}, {"weekly", limits.SevenDay},
					{"weekly_opus", limits.SevenDayOpus}, {"weekly_sonnet", limits.SevenDaySonnet},
				} {
					if item.win != nil {
						windows = append(windows, map[string]any{"name": item.name, "used_percent": item.win.UsedPercent, "resets_at": item.win.ResetsAt})
					}
				}
				result["windows"] = windows
			}
		}
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintf(stderr, "firekeeper claude-limits: write JSON: %v\n", err)
		return 1
	}
	return 0
}

func claudeLimitsStatus(stdout io.Writer) int {
	c, err := config.Load(config.Options{Getenv: os.Getenv})
	if err != nil {
		fmt.Fprintf(stdout, "config: %v\n", err)
		return 1
	}
	if !c.ClaudeLimits {
		fmt.Fprintln(stdout, "Claude limits: off (firekeeper claude-limits enable)")
		return 0
	}
	fmt.Fprintln(stdout, "Claude limits: on")
	client := newClaudeLimitsClient()
	if !client.HasToken() {
		fmt.Fprintf(stdout, "Sign-in: %v\n", claudelimits.ErrNoToken)
		return 0
	}
	fmt.Fprintln(stdout, "Sign-in: found")
	limits, err := client.Fetch(context.Background())
	if err != nil {
		fmt.Fprintf(stdout, "Usage endpoint: %v\n", err)
		return 0
	}
	for _, w := range []struct {
		name   string
		window *claudelimits.Window
	}{{"5-hour", limits.FiveHour}, {"weekly", limits.SevenDay}, {"weekly Opus", limits.SevenDayOpus}, {"weekly Sonnet", limits.SevenDaySonnet}} {
		if w.window != nil {
			fmt.Fprintf(stdout, "%s: %.0f%% used\n", w.name, w.window.UsedPercent)
		}
	}
	return 0
}
