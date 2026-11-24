package transcript

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateToolText(t *testing.T) {
	for _, test := range []struct {
		name     string
		text     string
		wantKept int
		wantCut  int
	}{
		{"empty", "", 0, 0},
		{"short", "short", 5, 0},
		{"exactly the limit", strings.Repeat("a", MaxToolText), MaxToolText, 0},
		{"one byte over", strings.Repeat("a", MaxToolText+1), MaxToolText, 1},
		// Two bytes per rune: the limit falls on a rune start, so nothing extra is cut.
		{"two-byte runes", strings.Repeat("é", MaxToolText), MaxToolText, MaxToolText},
		// Three bytes per rune: the cut moves back to the last whole rune.
		{"three-byte runes", strings.Repeat("€", MaxToolText/3+10), MaxToolText - MaxToolText%3, 3*(MaxToolText/3+10) - (MaxToolText - MaxToolText%3)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := TruncateToolText(test.text)
			if test.wantCut == 0 {
				if got != test.text {
					t.Fatalf("text that fits changed: %d bytes became %d", len(test.text), len(got))
				}
				return
			}
			kept, marker, ok := strings.Cut(got, "\n[truncated ")
			if !ok || marker != fmt.Sprintf("%d bytes]", test.wantCut) {
				t.Fatalf("marker = %q; want %d bytes cut", marker, test.wantCut)
			}
			if len(kept) != test.wantKept || !utf8.ValidString(kept) || !strings.HasPrefix(test.text, kept) {
				t.Fatalf("kept %d bytes (valid UTF-8 %v); want a %d-byte prefix", len(kept), utf8.ValidString(kept), test.wantKept)
			}
		})
	}
}

func FuzzTruncateToolText(f *testing.F) {
	f.Add("short")
	f.Add(strings.Repeat("é", MaxToolText))
	f.Add(strings.Repeat("a", MaxToolText+1))
	f.Add(string([]byte{0xff, 0xfe}))
	f.Fuzz(func(t *testing.T, text string) {
		got := TruncateToolText(text)
		if len(text) <= MaxToolText {
			if got != text {
				t.Fatal("text that fits changed")
			}
			return
		}
		kept, marker, ok := strings.Cut(got, "\n[truncated ")
		if !ok || !strings.HasSuffix(marker, " bytes]") {
			t.Fatal("long text has no truncation marker")
		}
		if len(kept) > MaxToolText || !strings.HasPrefix(text, kept) {
			t.Fatalf("kept %d bytes, not a prefix within the limit", len(kept))
		}
		if marker != fmt.Sprintf("%d bytes]", len(text)-len(kept)) {
			t.Fatalf("marker %q does not count the cut bytes", marker)
		}
		if utf8.ValidString(text) && !utf8.ValidString(kept) {
			t.Fatal("the cut split a rune")
		}
	})
}

func TestOptionalString(t *testing.T) {
	if got := OptionalString(""); got != nil {
		t.Errorf("OptionalString(\"\") = %q; want nil", *got)
	}
	if got := OptionalString("model"); got == nil || *got != "model" {
		t.Errorf("OptionalString(\"model\") = %v; want a pointer to model", got)
	}
	a, b := OptionalString("x"), OptionalString("x")
	if a == b {
		t.Error("two calls share one pointer")
	}
}

func TestCompactJSON(t *testing.T) {
	for raw, want := range map[string]string{
		`{ "a" : [ 1, 2 ] }`: `{"a":[1,2]}`,
		`"text"`:             `"text"`,
		`null`:               `null`,
		``:                   ``,
		`{"a":`:              `{"a":`, // not JSON: kept as it is
		`not json`:           `not json`,
	} {
		if got := CompactJSON(json.RawMessage(raw)); got != want {
			t.Errorf("CompactJSON(%q) = %q; want %q", raw, got, want)
		}
	}
}

func TestJSONString(t *testing.T) {
	for _, test := range []struct {
		raw    string
		want   string
		wantOK bool
	}{
		{`"plain"`, "plain", true},
		{`"line\nbreak é"`, "line\nbreak é", true},
		{`""`, "", true},
		{`null`, "", true},
		{``, "", false},
		{`42`, "", false},
		{`["a"]`, "", false},
		{`{"text":"a"}`, "", false},
		{`"unterminated`, "", false},
	} {
		got, ok := JSONString(json.RawMessage(test.raw))
		if got != test.want || ok != test.wantOK {
			t.Errorf("JSONString(%q) = %q, %v; want %q, %v", test.raw, got, ok, test.want, test.wantOK)
		}
	}
}

func TestJSONText(t *testing.T) {
	for raw, want := range map[string]string{
		``:                        "",
		`null`:                    "",
		`"ls -la"`:                "ls -la",
		`""`:                      "",
		`{ "command" : "ls" }`:    `{"command":"ls"}`,
		`[1, 2]`:                  `[1,2]`,
		`true`:                    `true`,
		`{"command":`:             `{"command":`, // not JSON: kept as it is
		`"streaming input so far`: `"streaming input so far`,
	} {
		if got := JSONText(json.RawMessage(raw)); got != want {
			t.Errorf("JSONText(%q) = %q; want %q", raw, got, want)
		}
	}
}

func FuzzJSONText(f *testing.F) {
	for _, seed := range []string{``, `null`, `"a"`, `{"a": 1}`, `[`, `"é"`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		got := JSONText(json.RawMessage(raw))
		if text, ok := JSONString(json.RawMessage(raw)); ok && raw != "null" && got != text {
			t.Fatalf("a JSON string's text is %q, JSONText gave %q", text, got)
		}
		if !json.Valid([]byte(raw)) && raw != "" && got != raw {
			t.Fatalf("invalid JSON %q changed to %q", raw, got)
		}
	})
}

func TestErrorCause(t *testing.T) {
	const private = "/synthetic/private/path"
	pathErr := &os.PathError{Op: "open", Path: private, Err: fs.ErrNotExist}
	execErr := &exec.Error{Name: private, Err: exec.ErrNotFound}
	plain := errors.New("plain failure")
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{"path error", pathErr, fs.ErrNotExist},
		{"wrapped path error", fmt.Errorf("read: %w", pathErr), fs.ErrNotExist},
		{"exec error", execErr, exec.ErrNotFound},
		{"plain error", plain, plain},
		{"nil", nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := ErrorCause(test.err)
			if got != test.want {
				t.Fatalf("ErrorCause = %v; want %v", got, test.want)
			}
			if got != nil && strings.Contains(got.Error(), private) {
				t.Fatal("the cause still names the private path")
			}
		})
	}
}

func TestJSONArray(t *testing.T) {
	type item struct {
		Type string `json:"type"`
	}
	if got := JSONArray[item](json.RawMessage(`[{"type":"text"},{"type":"tool_use"}]`)); len(got) != 2 || got[1].Type != "tool_use" {
		t.Errorf("array = %+v", got)
	}
	if got := JSONArray[item](json.RawMessage(`[]`)); got == nil || len(got) != 0 {
		t.Errorf("empty array = %#v; want an empty, non-nil slice", got)
	}
	for _, raw := range []string{``, `null`, `"text"`, `{"type":"text"}`, `[{"type":`, `[1, 2]`, ` [ ]`} {
		if got := JSONArray[item](json.RawMessage(raw)); got != nil {
			t.Errorf("JSONArray(%q) = %#v; want nil", raw, got)
		}
	}
}
