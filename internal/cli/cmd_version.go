package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/firekeeper-ai/firekeeper/internal/version"
)

func runVersion(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("firekeeper version", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print version metadata as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "firekeeper version: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *asJSON {
		if err := json.NewEncoder(stdout).Encode(map[string]string{
			"version": version.Version, "commit": version.Commit, "date": version.Date,
		}); err != nil {
			fmt.Fprintf(stderr, "firekeeper version: write JSON: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(stdout, version.String())
	return 0
}
