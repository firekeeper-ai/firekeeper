// Package turns derives per-user-turn metrics from normalized transcript
// events.
package turns

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/firekeeper-ai/firekeeper/internal/transcript"
)

// Tokens contains the token counts observed during one user turn.
type Tokens struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

// Turn contains metrics for one user prompt. Prompt text is deliberately not
// retained or serialized.
type Turn struct {
	Index           int            `json:"index"`
	StartedAt       *time.Time     `json:"started_at"`
	EndedAt         *time.Time     `json:"ended_at"`
	DurationSeconds *float64       `json:"duration_seconds"`
	PromptChars     int            `json:"prompt_chars"`
	Tokens          Tokens         `json:"tokens"`
	ContextTokens   int64          `json:"context_tokens"`
	ToolCalls       int            `json:"tool_calls"`
	ToolErrors      int            `json:"tool_errors"`
	Tools           map[string]int `json:"tools"`
	Interrupted     bool           `json:"interrupted"`
	Model           string         `json:"model"`
}

// Parse groups events by user prompts and computes metrics without returning
// any event text. ContextTokens is the running sum of input and cached tokens
// through the end of each turn.
func Parse(events []transcript.Event) []Turn {
	var out []Turn
	var current *Turn
	var contextTokens int64
	for _, event := range events {
		if event.Role == transcript.RoleUser {
			if current != nil {
				finish(current)
				contextTokens += current.Tokens.Input + current.Tokens.CacheRead + current.Tokens.CacheWrite
				current.ContextTokens = contextTokens
				out = append(out, *current)
			}
			current = &Turn{
				Index:       len(out),
				StartedAt:   cloneTime(event.TS),
				EndedAt:     cloneTime(event.TS),
				PromptChars: utf8.RuneCountInString(event.Text),
				Tools:       map[string]int{},
			}
		}
		if current == nil {
			continue
		}
		if event.Role != transcript.RoleUser && event.TS != nil {
			current.EndedAt = cloneTime(event.TS)
		}
		current.Tokens.Input += nonnegative(event.Tokens.Input)
		current.Tokens.Output += nonnegative(event.Tokens.Output)
		current.Tokens.CacheRead += nonnegative(event.Tokens.CacheRead)
		current.Tokens.CacheWrite += nonnegative(event.Tokens.CacheWrite)
		if event.Model != nil && *event.Model != "" {
			current.Model = *event.Model
		}
		if event.Role == transcript.RoleToolCall {
			current.ToolCalls++
			name := "unknown"
			if event.ToolName != nil && *event.ToolName != "" {
				name = *event.ToolName
			}
			current.Tools[name]++
		}
		if event.Role == transcript.RoleToolResult {
			if toolError(event.Raw) {
				current.ToolErrors++
			}
		}
		if interrupted(event) {
			current.Interrupted = true
		}
	}
	if current != nil {
		finish(current)
		contextTokens += current.Tokens.Input + current.Tokens.CacheRead + current.Tokens.CacheWrite
		current.ContextTokens = contextTokens
		out = append(out, *current)
	}
	return out
}

func finish(turn *Turn) {
	if turn.StartedAt != nil && turn.EndedAt != nil {
		duration := turn.EndedAt.Sub(*turn.StartedAt).Seconds()
		if duration < 0 {
			duration = 0
		}
		turn.DurationSeconds = &duration
	}
}

func cloneTime(ts *time.Time) *time.Time {
	if ts == nil {
		return nil
	}
	copy := *ts
	return &copy
}

func nonnegative(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}

func interrupted(event transcript.Event) bool {
	if trueField(event.Raw, "interrupted", "cancelled", "canceled") || cancelledField(event.Raw) {
		return true
	}
	text := strings.ToLower(event.Text)
	if event.Role == transcript.RoleUser {
		return strings.Contains(text, "request interrupted by user") ||
			strings.Contains(text, "request canceled by user") ||
			strings.Contains(text, "request cancelled by user")
	}
	return event.Role == transcript.RoleSystem &&
		(strings.TrimSpace(text) == "request cancelled." || strings.TrimSpace(text) == "request canceled.")
}

func toolError(raw json.RawMessage) bool {
	if len(raw) == 0 || !json.Valid(raw) {
		return false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	return hasError(value)
}

func hasError(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			switch strings.ToLower(key) {
			case "is_error", "iserror":
				if yes, ok := child.(bool); ok && yes {
					return true
				}
			case "error":
				if meaningful(child) {
					return true
				}
			case "status":
				if status, ok := child.(string); ok &&
					(strings.EqualFold(status, "error") || strings.EqualFold(status, "failed")) {
					return true
				}
			}
			if hasError(child) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if hasError(child) {
				return true
			}
		}
	}
	return false
}

func meaningful(value any) bool {
	switch value := value.(type) {
	case nil:
		return false
	case bool:
		return value
	case string:
		return strings.TrimSpace(value) != ""
	case json.Number:
		return value.String() != "0"
	case float64:
		return value != 0
	case []any:
		return len(value) > 0
	case map[string]any:
		return len(value) > 0
	default:
		return true
	}
}

func trueField(raw json.RawMessage, keys ...string) bool {
	if len(raw) == 0 || !json.Valid(raw) {
		return false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	return findTrueField(value, keys)
}

func cancelledField(raw json.RawMessage) bool {
	if len(raw) == 0 || !json.Valid(raw) {
		return false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	return findCancelledField(value)
}

func findCancelledField(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			name := strings.ToLower(key)
			if name == "finishreason" || name == "finish_reason" || name == "status" {
				if status, ok := child.(string); ok &&
					(strings.Contains(strings.ToLower(status), "cancel") || strings.Contains(strings.ToLower(status), "interrupt")) {
					return true
				}
			}
			if findCancelledField(child) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if findCancelledField(child) {
				return true
			}
		}
	}
	return false
}

func findTrueField(value any, keys []string) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			for _, wanted := range keys {
				if strings.EqualFold(key, wanted) {
					if yes, ok := child.(bool); ok && yes {
						return true
					}
				}
			}
			if findTrueField(child, keys) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if findTrueField(child, keys) {
				return true
			}
		}
	}
	return false
}
