package redact_test

import (
	"strings"
	"testing"

	"github.com/firekeeper-ai/firekeeper/pkg/event"
	"github.com/firekeeper-ai/firekeeper/pkg/redact"
)

func TestPublicEntryPoints(t *testing.T) {
	opts := redact.Options{HomeDir: "/home/dev", Paths: []string{"/srv/private"}}

	got, counts := redact.String("see /srv/private/x and /home/dev/src", opts)
	if got != "see [REDACTED:path]/x and ~/src" || counts.Total() != 2 {
		t.Fatalf("String = %q (%d replacements)", got, counts.Total())
	}

	in := event.Event{
		Provider: event.ProviderCodex, Role: event.RoleUser, Text: "/srv/private/plan.md",
		Raw: []byte(`{"cwd":"/home/dev/src"}`),
	}
	out, _ := redact.Event(in, opts)
	if strings.Contains(out.Text, "private") || string(out.Raw) != `{"cwd":"~/src"}` {
		t.Fatalf("Event = %q %s", out.Text, out.Raw)
	}
	if in.Text != "/srv/private/plan.md" {
		t.Fatal("Event modified its input")
	}
}

func TestEventEncodedWindowsPaths(t *testing.T) {
	opts := redact.Options{HomeDir: `C:\Users\Dan`}
	for _, tt := range []struct{ in, want string }{
		{`%43%3A%5C%55sers%5CD%61n%5Csrc`, `~%5Csrc`},
		{`%43%3A%5C%55sers%5CD%61n%6Fld`, `%43%3A%5C%55sers%5CD%61n%6Fld`},
	} {
		t.Run(tt.in, func(t *testing.T) {
			originalRaw := `{"cwd":"` + tt.in + `"}`
			in := event.Event{Provider: event.ProviderCodex, Role: event.RoleToolResult, Text: tt.in, Raw: []byte(originalRaw)}
			out, counts := redact.Event(in, opts)
			if out.Text != tt.want || string(out.Raw) != `{"cwd":"`+tt.want+`"}` {
				t.Fatal("public event redaction disagrees between text and raw")
			}
			wantCount := 0
			if tt.in != tt.want {
				wantCount = 2
			}
			if counts.Total() != wantCount || string(in.Raw) != originalRaw || in.Text != tt.in {
				t.Fatal("unexpected counts or input mutation")
			}
		})
	}
}
