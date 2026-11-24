package config

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/transcript"
)

// SaveReportingPreferences updates the selected file keys together. Nil leaves
// a key alone, including any default or environment value inherited by it.
func SaveReportingPreferences(path string, providers []transcript.Provider, interval time.Duration) error {
	var quoted []string
	for _, p := range providers {
		if !p.Valid() {
			return ErrUnknownProvider
		}
		quoted = append(quoted, fmt.Sprintf("%q", p))
	}
	return edit(path, true, func(lines []string) []string {
		if providers != nil {
			lines = setLiteral(lines, "providers", "["+strings.Join(quoted, ", ")+"]")
		}
		if interval > 0 {
			lines = setKey(lines, "interval", interval.String())
		}
		return lines
	}, func(m map[string]any) bool {
		if interval > 0 && m["interval"] != interval.String() {
			return false
		}
		if providers == nil {
			return true
		}
		values, ok := m["providers"].([]any)
		if !ok || len(values) != len(providers) {
			return false
		}
		return slices.EqualFunc(values, providers, func(v any, p transcript.Provider) bool { return v == string(p) })
	})
}
