package tui

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/firekeeper-ai/firekeeper/pkg/event"
)

// weeklyChartsView uses small multiples so a provider with a large token count
// cannot hide variation in a smaller provider's history.
func weeklyChartsView(providers []overviewProvider, now time.Time, width, height int, loading bool) string {
	var charts []overviewProvider
	for _, p := range providers {
		if len(p.Days) == overviewWeekDays {
			charts = append(charts, p)
		}
	}
	if len(charts) == 0 {
		note := "No daily token history available."
		if loading {
			note = "Loading daily token history…"
		}
		return strings.Join(dashBox("Weekly tokens", []string{usageNote(note)}, width, height, dashBlue), "\n")
	}
	cols := 1
	if width >= 78 && len(charts) > 1 {
		cols = 2
	}
	rows := (len(charts) + cols - 1) / cols
	if height/rows < 5 || width < 25 {
		return strings.Join(dashBox("Weekly tokens", []string{usageNote("Enlarge terminal for charts.")}, width, height, dashBlue), "\n")
	}
	var lines []string
	for row := 0; row < rows; row++ {
		h := (row+1)*height/rows - row*height/rows
		var cards [][]string
		group := charts[row*cols : min((row+1)*cols, len(charts))]
		for col, p := range group {
			w := (col+1)*width/len(group) - col*width/len(group)
			cards = append(cards, weeklyTokenCard(p, now, w, h))
		}
		for y := 0; y < h; y++ {
			var line strings.Builder
			for _, card := range cards {
				line.WriteString(card[y])
			}
			lines = append(lines, line.String())
		}
	}
	return strings.Join(lines, "\n")
}

func sumTokenParts(a, b tokenBreakdown) tokenBreakdown {
	return tokenBreakdown{Input: a.Input + b.Input, Output: a.Output + b.Output, CacheRead: a.CacheRead + b.CacheRead, CacheWrite: a.CacheWrite + b.CacheWrite}
}

// Order is bottom to top. Each provider keeps its hue, with light input,
// saturated output, dark cache reads, and pale cache writes.
var weeklyShades = map[event.Provider][4]string{
	event.ProviderCodex:   {"102;211;196", "33;150;158", "47;88;99", "205;255;241"},
	event.ProviderClaude:  {"250;165;109", "210;99;63", "123;72;64", "255;225;177"},
	event.ProviderKimi:    {"232;199;100", "194;147;35", "112;94;53", "255;240;179"},
	event.ProviderCopilot: {"183;153;238", "146;98;207", "90;67;122", "235;216;255"},
}

func weeklyTokenCard(p overviewProvider, now time.Time, width, height int) []string {
	inner := width - 4
	palette := weeklyShades[p.Provider]
	colors := [5]string{palette[0], palette[1], palette[2], palette[3], usageMutedColor}
	labels := [5]string{"Input", "Output", "Cache read", "Cache write", "Unsplit"}
	var stacks [overviewWeekDays][5]int64
	var used [5]bool
	for day, n := range p.Days {
		parts := p.Parts[now.AddDate(0, 0, day-overviewWeekDays+1).Format(time.DateOnly)]
		if parts.Total() > n || parts.Input < 0 || parts.Output < 0 || parts.CacheRead < 0 || parts.CacheWrite < 0 {
			parts = tokenBreakdown{}
		}
		stacks[day] = [5]int64{parts.Input, parts.Output, parts.CacheRead, parts.CacheWrite, n - parts.Total()}
		for i, v := range stacks[day] {
			used[i] = used[i] || v > 0
		}
	}
	var body []string
	legend := ""
	for i, show := range used {
		if !show {
			continue
		}
		label := dashPaint("█", colors[i], false) + dashPaint(" "+labels[i], usageTextColor, false)
		if legend != "" && ansi.StringWidth(legend)+2+ansi.StringWidth(label) > inner {
			body = append(body, legend)
			legend = ""
		}
		if legend != "" {
			legend += dashPaint("  ", usageMutedColor, false)
		}
		legend += label
	}
	if legend != "" {
		body = append(body, legend)
	}
	plotRows := height - 4 - len(body)
	if plotRows < 1 {
		return dashBox(toolLabel(p.Provider), []string{usageNote("Enlarge terminal for chart.")}, width, height, providerColor(p.Provider))
	}
	peak := slices.Max(p.Days)
	// Two vertical samples per terminal cell give small categories more room.
	for row := plotRows; row > 0; row-- {
		var line strings.Builder
		for day, stack := range stacks {
			slot := (day+1)*inner/overviewWeekDays - day*inner/overviewWeekDays
			bar := max(1, slot*2/3)
			colorAt := func(level int) string {
				var total int64
				for i, v := range stack {
					total += v
					if total > 0 && level <= max(1, int(math.Round(float64(total)/float64(peak)*float64(plotRows*2)))) {
						return colors[i]
					}
				}
				return usagePanelColor
			}
			top, bottom := colorAt(row*2), colorAt(row*2-1)
			glyph := "▀"
			if top == usagePanelColor && bottom == usagePanelColor {
				glyph = " "
			} else if top == bottom {
				glyph = "█"
			}
			pad := slot - bar
			line.WriteString(dashPaint(strings.Repeat(" ", pad/2), usageMutedColor, false))
			line.WriteString(usagePaint(strings.Repeat(glyph, bar), top, false, bottom))
			line.WriteString(dashPaint(strings.Repeat(" ", pad-pad/2), usageMutedColor, false))
		}
		body = append(body, line.String())
	}
	accent := providerColor(p.Provider)
	var totals, weekdays strings.Builder
	for day, n := range p.Days {
		slot := (day+1)*inner/overviewWeekDays - day*inner/overviewWeekDays
		label := now.AddDate(0, 0, day-overviewWeekDays+1).Format("Mon")
		if day == overviewWeekDays-1 && slot >= 5 {
			label = "Today"
		}
		totals.WriteString(weeklyCenter(formatTokenCount(n), slot))
		weekdays.WriteString(weeklyCenter(label, slot))
	}
	body = append(body, dashPaint(totals.String(), usageTextColor, true), dashPaint(weekdays.String(), usageMutedColor, false))
	var total int64
	for _, n := range p.Days {
		total += n
	}
	title := fmt.Sprintf("%s · 7d %s tokens", toolLabel(p.Provider), formatTokenCount(total))
	return dashBox(title, body, width, height, accent)
}

func weeklyCenter(s string, width int) string {
	if ansi.StringWidth(s) > width {
		s = ansi.Truncate(s, width, "…")
	}
	pad := width - ansi.StringWidth(s)
	return strings.Repeat(" ", pad/2) + s + strings.Repeat(" ", pad-pad/2)
}
