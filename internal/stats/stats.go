// Package stats derives privacy-preserving findings from normalized local
// transcript events.
package stats

import (
	"sort"
	"time"

	"github.com/firekeeper-ai/firekeeper/internal/transcript"
	"github.com/firekeeper-ai/firekeeper/internal/turns"
)

// FindingKind is a stable machine-readable insight category.
type FindingKind string

const (
	FindingContextGrowth   FindingKind = "context_growth"
	FindingToolReliability FindingKind = "tool_reliability"
	FindingAttention       FindingKind = "attention"
	FindingRhythm          FindingKind = "rhythm"
	FindingSpend           FindingKind = "spend"
)

const (
	// ContextGrowthThresholdTokens is the peak context size past which a
	// session would likely gain from compacting or a fresh start.
	ContextGrowthThresholdTokens = 150_000.0
	ToolErrorRateThreshold       = 0.20
	// ToolErrorMinSamples is the fewest calls of one tool a rate is judged on.
	ToolErrorMinSamples         = 5
	AttentionThresholdSeconds   = 300.0
	RhythmThresholdTurnsPerHour = 4.0
	SpendThresholdTokens        = 1_000_000.0
)

// Session contains local metadata and normalized events for one session.
type Session struct {
	ID       string
	Provider transcript.Provider
	Project  string
	Events   []transcript.Event
}

// Finding is a threshold-based insight. It intentionally contains no event or
// prompt text.
type Finding struct {
	Kind      FindingKind         `json:"kind"`
	SessionID string              `json:"session_id"`
	Provider  transcript.Provider `json:"provider"`
	Project   string              `json:"project"`
	Metric    string              `json:"metric"`
	Value     float64             `json:"value"`
	Threshold float64             `json:"threshold"`
	Unit      string              `json:"unit"`
	// Tool and Samples are set on tool_reliability findings: the tool and
	// the number of its calls the rate covers.
	Tool    string `json:"tool,omitempty"`
	Samples int    `json:"samples,omitempty"`
}

// unknownModel labels spend from events that name no model, as window does.
const unknownModel = "unknown"

// Spend is the token total for one project, provider, and model.
type Spend struct {
	Project  string              `json:"project"`
	Provider transcript.Provider `json:"provider"`
	Model    string              `json:"model"`
	Tokens   int64               `json:"tokens"`
}

// Rhythm reports the number of user turns started in a weekday/hour bucket.
// Weekday is Sunday=0 through Saturday=6, and Hour is UTC.
type Rhythm struct {
	Weekday int `json:"weekday"`
	Hour    int `json:"hour"`
	Turns   int `json:"turns"`
}

// Report is the complete text-free stats result.
type Report struct {
	Since           time.Time `json:"since"`
	SessionsScanned int       `json:"sessions_scanned"`
	Findings        []Finding `json:"findings"`
	Spend           []Spend   `json:"spend"`
	Rhythm          []Rhythm  `json:"rhythm"`
	// RhythmTimezone is the timezone of the Rhythm hours and weekdays.
	RhythmTimezone string `json:"rhythm_timezone"`
	// Notes name providers or sessions that could not be read and are not
	// counted. Build leaves it empty; the caller fills it.
	Notes []string `json:"notes"`
}

// Build computes findings over events at or after since. Events without a
// timestamp are excluded because they cannot be assigned to the requested
// time range.
func Build(sessions []Session, since time.Time) Report {
	report := Report{
		Since:    since,
		Findings: []Finding{},
		Spend:    []Spend{},
		Rhythm:   []Rhythm{},
		Notes:    []string{},
		// Rhythm buckets are counted in UTC.
		RhythmTimezone: "UTC",
	}
	spend := make(map[spendKey]int64)
	sessionSpend := make(map[sessionSpendKey]int64)
	rhythm := make(map[int]int)
	for _, session := range sessions {
		events := inRange(session.Events, since)
		if len(events) == 0 {
			continue
		}
		report.SessionsScanned++
		sessionTurns := turns.Parse(events)
		base := Finding{
			SessionID: session.ID,
			Provider:  session.Provider,
			Project:   session.Project,
		}

		if peak := peakContext(sessionTurns); peakContextKnown(session.Provider) && float64(peak) >= ContextGrowthThresholdTokens {
			finding := base
			finding.Kind = FindingContextGrowth
			finding.Metric = "peak_context_tokens"
			finding.Value = float64(peak)
			finding.Threshold = ContextGrowthThresholdTokens
			finding.Unit = "tokens"
			report.Findings = append(report.Findings, finding)
		}
		for _, tool := range toolCounts(sessionTurns) {
			if tool.calls < ToolErrorMinSamples {
				continue
			}
			rate := float64(tool.errors) / float64(tool.calls)
			if rate >= ToolErrorRateThreshold {
				finding := base
				finding.Kind = FindingToolReliability
				finding.Metric = "tool_error_rate"
				finding.Value = rate
				finding.Threshold = ToolErrorRateThreshold
				finding.Unit = "ratio"
				finding.Tool = tool.name
				finding.Samples = tool.calls
				report.Findings = append(report.Findings, finding)
			}
		}
		if delay, ok := medianReplyDelay(sessionTurns); ok && delay >= AttentionThresholdSeconds {
			finding := base
			finding.Kind = FindingAttention
			finding.Metric = "median_reply_delay_seconds"
			finding.Value = delay
			finding.Threshold = AttentionThresholdSeconds
			finding.Unit = "seconds"
			report.Findings = append(report.Findings, finding)
		}
		if peak := peakTurnsPerHour(sessionTurns); peak >= int(RhythmThresholdTurnsPerHour) {
			finding := base
			finding.Kind = FindingRhythm
			finding.Metric = "peak_weekday_hour_turns"
			finding.Value = float64(peak)
			finding.Threshold = RhythmThresholdTurnsPerHour
			finding.Unit = "turns"
			report.Findings = append(report.Findings, finding)
		}

		for _, event := range events {
			model := unknownModel
			if event.Model != nil && *event.Model != "" {
				model = *event.Model
			}
			tokens := eventTokenCount(event.Tokens)
			if tokens > 0 {
				spend[spendKey{project: session.Project, provider: session.Provider, model: model}] += tokens
				sessionSpend[sessionSpendKey{project: session.Project, provider: session.Provider, sessionID: session.ID}] += tokens
			}
			if event.Role == transcript.RoleUser && event.TS != nil {
				utc := event.TS.UTC()
				rhythm[int(utc.Weekday())*24+utc.Hour()]++
			}
		}
	}

	for key, count := range spend {
		report.Spend = append(report.Spend, Spend{
			Project: key.project, Provider: key.provider, Model: key.model, Tokens: count,
		})
	}
	sort.Slice(report.Spend, func(i, j int) bool {
		a, b := report.Spend[i], report.Spend[j]
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		return a.Model < b.Model
	})
	for bucket, count := range rhythm {
		report.Rhythm = append(report.Rhythm, Rhythm{
			Weekday: bucket / 24,
			Hour:    bucket % 24,
			Turns:   count,
		})
	}
	sort.Slice(report.Rhythm, func(i, j int) bool {
		if report.Rhythm[i].Weekday != report.Rhythm[j].Weekday {
			return report.Rhythm[i].Weekday < report.Rhythm[j].Weekday
		}
		return report.Rhythm[i].Hour < report.Rhythm[j].Hour
	})
	for key, count := range sessionSpend {
		if count < int64(SpendThresholdTokens) {
			continue
		}
		report.Findings = append(report.Findings, Finding{
			Kind: FindingSpend, SessionID: key.sessionID, Provider: key.provider,
			Project: key.project, Metric: "session_tokens", Value: float64(count),
			Threshold: SpendThresholdTokens, Unit: "tokens",
		})
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		a, b := report.Findings[i], report.Findings[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.SessionID != b.SessionID {
			return a.SessionID < b.SessionID
		}
		if a.Metric != b.Metric {
			return a.Metric < b.Metric
		}
		return a.Tool < b.Tool
	})
	return report
}

type spendKey struct {
	project  string
	provider transcript.Provider
	model    string
}

type sessionSpendKey struct {
	project   string
	provider  transcript.Provider
	sessionID string
}

func inRange(events []transcript.Event, since time.Time) []transcript.Event {
	out := make([]transcript.Event, 0, len(events))
	for _, event := range events {
		if event.TS != nil && !event.TS.Before(since) {
			out = append(out, event)
		}
	}
	return out
}

// peakContext is the largest context size seen in any turn.
func peakContext(sessionTurns []turns.Turn) int64 {
	var peak int64
	for _, turn := range sessionTurns {
		peak = max(peak, turn.PeakContextTokens)
	}
	return peak
}

// peakContextKnown reports whether provider logs usage per model call, so a
// turn's peak is a context size. Copilot logs its main agent's usage only as
// run totals.
func peakContextKnown(provider transcript.Provider) bool {
	switch provider {
	case transcript.ProviderCopilot:
		return false
	case transcript.ProviderCodex, transcript.ProviderKimi, transcript.ProviderClaude,
		transcript.ProviderOpenCode, transcript.ProviderGemini, transcript.ProviderCursor:
		return true
	default:
		return false
	}
}

type toolCount struct {
	name   string
	calls  int
	errors int
}

// toolCounts sums calls and errors per tool over a session, sorted by name.
// Errors on results that name no tool have no call count and are left out.
func toolCounts(sessionTurns []turns.Turn) []toolCount {
	byName := map[string]*toolCount{}
	for _, turn := range sessionTurns {
		for name, calls := range turn.Tools {
			if byName[name] == nil {
				byName[name] = &toolCount{name: name}
			}
			byName[name].calls += calls
		}
	}
	for _, turn := range sessionTurns {
		for name, errors := range turn.ToolErrorsByTool {
			if byName[name] != nil {
				byName[name].errors += errors
			}
		}
	}
	out := make([]toolCount, 0, len(byName))
	for _, count := range byName {
		out = append(out, *count)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func medianReplyDelay(turns []turns.Turn) (float64, bool) {
	var delays []float64
	for i := 1; i < len(turns); i++ {
		if turns[i-1].EndedAt == nil || turns[i].StartedAt == nil {
			continue
		}
		delay := turns[i].StartedAt.Sub(*turns[i-1].EndedAt).Seconds()
		if delay < 0 {
			delay = 0
		}
		delays = append(delays, delay)
	}
	if len(delays) == 0 {
		return 0, false
	}
	sort.Float64s(delays)
	middle := len(delays) / 2
	if len(delays)%2 == 1 {
		return delays[middle], true
	}
	return (delays[middle-1] + delays[middle]) / 2, true
}

func peakTurnsPerHour(turns []turns.Turn) int {
	buckets := make(map[int]int)
	for _, turn := range turns {
		if turn.StartedAt == nil {
			continue
		}
		ts := turn.StartedAt.UTC()
		bucket := int(ts.Weekday())*24 + ts.Hour()
		buckets[bucket]++
	}
	peak := 0
	for _, count := range buckets {
		if count > peak {
			peak = count
		}
	}
	return peak
}

func eventTokenCount(tokens transcript.Tokens) int64 {
	return tokens.Input + tokens.Output + turns.CacheTokens(tokens)
}
