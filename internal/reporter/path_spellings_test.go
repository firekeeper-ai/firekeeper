package reporter

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestUploadedWindowsPathSpellings(t *testing.T) {
	for _, tt := range []struct{ name, path, in, want string }{
		{"encoded", `C:\Users\Dan`, `%43%3A%5C%55sers%5CD%61n%5Csrc`, `[REDACTED:path]%5Csrc`},
		{"encoded longer name", `C:\Users\Dan`, `%43%3A%5C%55sers%5CD%61n%6Fld`, `%43%3A%5C%55sers%5CD%61n%6Fld`},
		{"unc case and separators", `\\server\profiles\Dan`, `//SERVER/profiles/dan/src`, `[REDACTED:path]/src`},
		{"unc other server", `\\server\profiles\Dan`, `//other/profiles/dan/src`, `//other/profiles/dan/src`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if err := os.WriteFile(f.rollout, []byte(line(0, tt.in)), 0o600); err != nil {
				t.Fatal(err)
			}
			srv := newTestServer(t)
			cfg := f.config(srv.URL)
			cfg.RedactPaths = []string{tt.path}
			if _, err := RunOnce(context.Background(), cfg); err != nil {
				t.Fatal(err)
			}
			events := srv.store.Events(testMachine, testSession)
			if len(events) != 1 {
				t.Fatalf("uploaded %d events, want 1", len(events))
			}
			if events[0].Text != "message 0 "+tt.want || string(events[0].Raw) != strings.TrimSpace(line(0, tt.want)) {
				t.Fatal("uploaded event did not preserve expected path redaction in text and raw")
			}
		})
	}
}
