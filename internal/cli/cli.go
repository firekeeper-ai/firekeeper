// Package cli implements Firekeeper's subcommands, such as report, daemon,
// login, and version. Running firekeeper with no subcommand starts the
// terminal dashboard instead (see internal/tui).
package cli

import (
	"bytes"
	"flag"
	"fmt"
	"io"
)

// subcommand is an opt-in reporting entry point. Running firekeeper without
// one starts the TUI exactly as before.
type subcommand struct {
	name    string
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

var subcommands = []subcommand{
	{"snapshot", "print currently discovered sessions (--json, --ndjson)", runSnapshot},
	{"export", "export local session events (--session, --json or --ndjson, --no-text)", runExport},
	{"turns", "print per-turn transcript metrics without prompt text (SESSION_ID, --provider, --json)", runTurns},
	{"stats", "print local transcript insights without prompt text (--since, --provider or --all, --project, --json)", runStats},
	{"report", "upload session metadata and redacted transcripts once", runReport},
	{"backfill", "upload sessions already on this machine once (--all, --provider, or --claude-cloud)", runBackfill},
	{"serve", "run the dashboard server (separate firekeeper-server binary)", runServe},
	{"login", "link this machine to a dashboard server (--server URL)", runLogin},
	{"logout", "revoke and remove this machine's stored token", runLogout},
	{"whoami", "print the server, account and machine the stored token belongs to", runWhoami},
	{"config", "print the merged reporting configuration (show)", runConfig},
	{"daemon", "upload continuously (--provider required); install, uninstall, status, logs", runDaemon},
	{"otel-listen", "experimental: record local Claude Code OTLP usage on 127.0.0.1", runOtelListen},
	{"otel-reconcile", "experimental: compare an otel-listen file with transcript totals", runOtelReconcile},
	{"telemetry", "opt-in Claude Code usage listener: enable, disable, status", runTelemetry},
	{"window", "break the current 5-hour or weekly window down by session (--provider, --week, --json, --html FILE)", runWindow},
	{"claude-limits", "Claude 5-hour and weekly limits, on by default (enable, disable, status)", runClaudeLimits},
	{"install-font", "install the logo font the dashboard uses (--remove to undo)", runInstallFont},
	{"version", "print the version, commit and build date (also --version)", runVersion},
}

// Register adds a subcommand implemented outside this package, so the cli
// package does not import the dashboard. main calls it before RunSubcommand.
func Register(name, summary string, run func(args []string, stdout, stderr io.Writer) int) {
	subcommands = append(subcommands, subcommand{name, summary, run})
}

// RunSubcommand dispatches on the first argument. It reports false when the
// arguments do not name a subcommand, so the caller falls through to the TUI.
func RunSubcommand(args []string, stdout, stderr io.Writer) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	if args[0] == "--version" {
		if hasFlag(args[1:], "--schema") {
			return writeSchema("version", args[1:], stdout), true
		}
		if hasFlag(args[1:], "--json") {
			var output bytes.Buffer
			code := runVersion(args[1:], &output, stderr)
			if code != 0 {
				return code, true
			}
			converted, err := transformJSONOutput("version", args[1:], output.Bytes())
			if err != nil {
				fmt.Fprintf(stderr, "firekeeper version: add schema version: %v\n", err)
				return 1, true
			}
			_, _ = stdout.Write(converted)
			return 0, true
		}
		return runVersion(args[1:], stdout, stderr), true
	}
	for _, cmd := range subcommands {
		if cmd.name == args[0] {
			cmdArgs := args[1:]
			if len(cmdArgs) > 0 && (cmdArgs[0] == "-h" || cmdArgs[0] == "--help") {
				if cmd.name == "otel-listen" || cmd.name == "otel-reconcile" {
					return cmd.run(cmdArgs, stdout, stderr), true
				}
				fmt.Fprintf(stdout, "Usage: firekeeper %s [flags]\n\n%s\n", cmd.name, cmd.summary)
				return 0, true
			}
			if hasFlag(cmdArgs, "--schema") {
				return writeSchema(cmd.name, cmdArgs, stdout), true
			}
			if cmd.name == "export" {
				return cmd.run(cmdArgs, stdout, stderr), true
			}
			if !isMachineOutput(cmd.name, cmdArgs) {
				return cmd.run(cmdArgs, stdout, stderr), true
			}
			var output bytes.Buffer
			code := cmd.run(cmdArgs, &output, stderr)
			if output.Len() == 0 {
				_, _ = io.Copy(stdout, &output)
				return code, true
			}
			converted, err := transformJSONOutput(cmd.name, cmdArgs, output.Bytes())
			if err != nil {
				_, _ = io.Copy(stdout, &output)
				fmt.Fprintf(stderr, "firekeeper %s: add schema version: %v\n", cmd.name, err)
				return 1, true
			}
			_, _ = stdout.Write(converted)
			return code, true
		}
	}
	return 0, false
}

func isMachineOutput(command string, args []string) bool {
	return hasFlag(args, "--json") || hasFlag(args, "--ndjson") || command == "export"
}

// PrintUsage writes the top-level usage text and the subcommand list to fs's
// output.
func PrintUsage(fs *flag.FlagSet) {
	out := fs.Output()
	fmt.Fprintf(out, "Usage:\n  firekeeper\n  firekeeper <command> [args]\n\nCommands:\n")
	for _, cmd := range subcommands {
		fmt.Fprintf(out, "  %-16s %s\n", cmd.name, cmd.summary)
	}
}

func notImplemented(name string, stderr io.Writer) int {
	fmt.Fprintf(stderr, "firekeeper %s: not implemented\n", name)
	return 2
}
