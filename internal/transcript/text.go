package transcript

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"unicode/utf8"
)

// MaxToolText is the longest tool output kept in Event.Text. Raw keeps the
// original.
const MaxToolText = 64 << 10

// TruncateToolText is text cut to MaxToolText bytes at a rune boundary, with
// a trailing "[truncated N bytes]" marker, or text itself when it fits.
func TruncateToolText(text string) string {
	if len(text) <= MaxToolText {
		return text
	}
	cut := MaxToolText
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return fmt.Sprintf("%s\n[truncated %d bytes]", text[:cut], len(text)-cut)
}

// OptionalString is a pointer to value, or nil when value is empty, for the
// optional string fields of Event.
func OptionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// CompactJSON is raw with insignificant space removed, or raw itself when it
// is not valid JSON.
func CompactJSON(raw json.RawMessage) string {
	var out bytes.Buffer
	if json.Compact(&out, raw) != nil {
		return string(raw)
	}
	return out.String()
}

// JSONString is the value of the JSON string raw. ok is false when raw is
// empty or is not a string; a JSON null decodes to "" with ok true.
func JSONString(raw json.RawMessage) (text string, ok bool) {
	if len(raw) == 0 || json.Unmarshal(raw, &text) != nil {
		return "", false
	}
	return text, true
}

// JSONText is what a human would read of a JSON value such as tool
// arguments: "" for an empty value or null, a string's own text, and any
// other value compacted (see CompactJSON).
func JSONText(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	if text, ok := JSONString(raw); ok {
		return text
	}
	return CompactJSON(raw)
}

// ErrorCause is err without the file path or command name an *os.PathError
// or *exec.Error adds, so a reader's error names no private path. Other
// errors are returned as they are.
func ErrorCause(err error) error {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return execErr.Err
	}
	return err
}

// JSONArray decodes raw as a JSON array of T. It is nil when raw is not an
// array or does not decode, and empty, not nil, for an empty array.
func JSONArray[T any](raw json.RawMessage) []T {
	var items []T
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &items) != nil {
		return nil
	}
	if items == nil {
		items = []T{}
	}
	return items
}
