package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/firekeeper-ai/firekeeper/internal/otelrecv"
)

func runOtelListen(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("firekeeper otel-listen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), `Usage:
  firekeeper otel-listen [--port 4318] [--out file.jsonl]

Experimental. Listens on 127.0.0.1 only and appends Claude Code api_request
usage to a local JSONL file. It does not start with the dashboard, does not
upload, and does not record prompt or transcript text.

`)
		fs.PrintDefaults()
	}
	port := fs.Int("port", otelrecv.DefaultPort, "TCP port on 127.0.0.1")
	out := fs.String("out", "", "JSONL file to append (default ~/.firekeeper/telemetry.jsonl)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "firekeeper otel-listen: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *port < 0 || *port > 65535 {
		fmt.Fprintf(stderr, "firekeeper otel-listen: port %d is outside 0-65535\n", *port)
		return 2
	}
	path := *out
	if path == "" {
		var err error
		path, err = otelrecv.DefaultPath()
		if err != nil {
			fmt.Fprintf(stderr, "firekeeper otel-listen: %v\n", err)
			return 1
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := otelrecv.Listen(ctx, otelrecv.Options{Port: *port, Path: path, Stdout: stdout, Stderr: stderr}); err != nil {
		fmt.Fprintf(stderr, "firekeeper otel-listen: %v\n", err)
		return 1
	}
	return 0
}

func runOtelReconcile(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("firekeeper otel-reconcile", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the comparison as JSON")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), `Usage:
  firekeeper otel-reconcile <file.jsonl>

Compare an otel-listen JSONL file with Claude Code transcript totals for the
same session ids. Prints the gap. Does not upload, and does not print prompts.

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(flagsBeforePositionals(args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "Usage: firekeeper otel-reconcile <file.jsonl>")
		return 2
	}
	records, skipped, err := otelrecv.ReadFile(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "firekeeper otel-reconcile: %v\n", err)
		return 1
	}
	if skipped > 0 {
		fmt.Fprintf(stderr, "firekeeper otel-reconcile: skipped %d unreadable line(s)\n", skipped)
	}
	writer := stdout
	var report bytes.Buffer
	if *asJSON {
		writer = &report
	}
	if err := otelrecv.WriteReconcile(writer, records, func(id string) (otelrecv.SessionUsage, error) {
		return otelrecv.LoadTranscript(id, "")
	}); err != nil {
		fmt.Fprintf(stderr, "firekeeper otel-reconcile: %v\n", err)
		return 1
	}
	if *asJSON {
		if err := json.NewEncoder(stdout).Encode(map[string]string{"report": report.String()}); err != nil {
			fmt.Fprintf(stderr, "firekeeper otel-reconcile: write JSON: %v\n", err)
			return 1
		}
	}
	return 0
}
