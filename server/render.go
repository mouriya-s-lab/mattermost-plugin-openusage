package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

const (
	colorGood    = "#2E7D32"
	colorAccent  = "#386FA4"
	colorWarning = "#B7791F"
	colorError   = "#C53030"

	barWidth = 12
)

// renderSnapshots renders one card per provider, in the order returned by the
// OpenUsage API (which honors the operator's plugin ordering).
func renderSnapshots(snaps []providerSnapshot) []*model.SlackAttachment {
	if len(snaps) == 0 {
		return []*model.SlackAttachment{{
			Title:  "OpenUsage",
			Text:   "No enabled providers returned usage data yet.",
			Color:  colorWarning,
			Footer: "OpenUsage · /v1/usage",
		}}
	}
	out := make([]*model.SlackAttachment, 0, len(snaps))
	for _, snap := range snaps {
		out = append(out, renderSnapshot(snap))
	}
	return out
}

// renderSnapshot builds one provider card. Progress limits render one per line
// as an inline-code `bar  value` pill (inline code wraps instead of clipping
// the way a fenced block does in the Mattermost webapp) followed by the bold
// label and the reset window. Every other line variant renders as a field
// below, in API order: text/badge as two-column short fields, barChart as a
// full-width sparkline field.
func renderSnapshot(snap providerSnapshot) *model.SlackAttachment {
	title := statusDot(snap) + " " + displayName(snap)
	if plan := strings.TrimSpace(derefString(snap.Plan)); plan != "" {
		title += "  ·  " + plan
	}

	var progress, rest []metricLine
	for _, line := range snap.Lines {
		switch line.Type {
		case lineProgress:
			progress = append(progress, line)
		default:
			rest = append(rest, line)
		}
	}

	att := &model.SlackAttachment{
		Title:  title,
		Color:  snapshotColor(snap),
		Footer: snapshotFooter(snap),
	}
	att.Text = progressLines(progress)
	att.Fields = lineFields(rest)
	if att.Text == "" && len(att.Fields) == 0 {
		att.Text = "_No usage lines returned._"
	}
	return att
}

// progressLines renders progress lines one per line:
//
//	`████░░░░░░░░   16%` **Session** · resets in 3h
//
// The bar and value share one inline-code span so they stay monospace-aligned
// across lines; percent values are right-padded to keep the pill width stable.
func progressLines(lines []metricLine) string {
	if len(lines) == 0 {
		return ""
	}
	rows := make([]string, 0, len(lines))
	for _, line := range lines {
		value := progressValue(line)
		if progressFormatKind(line) == formatPercent {
			value = fmt.Sprintf("%4s", value)
		}
		row := "`" + usageBar(lineUsedPercent(line)) + "  " + value + "`" +
			" **" + emptyAs(strings.TrimSpace(line.Label), "—") + "**"
		if reset := relativeReset(line.ResetsAt); reset != "" {
			row += " · " + reset
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

// progressFormatKind returns the effective format kind of a progress line
// (percent when absent, per the OpenUsage schema default).
func progressFormatKind(line metricLine) formatKind {
	if line.Format != nil && line.Format.Kind != "" {
		return line.Format.Kind
	}
	return formatPercent
}

// usageBar draws a fixed-width bar from a 0..100 percentage. NaN renders empty.
func usageBar(pct float64) string {
	if math.IsNaN(pct) {
		return strings.Repeat("░", barWidth)
	}
	pct = math.Max(0, math.Min(100, pct))
	filled := int(math.Round(pct / 100 * barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
}

// progressValue is the right-hand readout for a progress line, formatted by kind.
func progressValue(line metricLine) string {
	used := derefFloat(line.Used)
	kind := formatPercent
	suffix := ""
	if line.Format != nil {
		if line.Format.Kind != "" {
			kind = line.Format.Kind
		}
		suffix = strings.TrimSpace(line.Format.Suffix)
	}

	switch kind {
	case formatDollars:
		if line.Limit != nil {
			return money(used) + " / " + money(*line.Limit)
		}
		return money(used)
	case formatCount:
		unit := ""
		if suffix != "" {
			unit = " " + suffix
		}
		if line.Limit != nil {
			return trimFloat(used) + "/" + trimFloat(*line.Limit) + unit
		}
		return trimFloat(used) + unit
	case formatPercent:
		fallthrough
	default:
		pct := lineUsedPercent(line)
		if math.IsNaN(pct) {
			return "—"
		}
		return fmt.Sprintf("%d%%", int(math.Round(pct)))
	}
}

// lineFields renders the non-progress lines as attachment fields, preserving
// API order: text/badge become two-column short fields, barChart becomes a
// full-width sparkline field.
func lineFields(lines []metricLine) []*model.SlackAttachmentField {
	if len(lines) == 0 {
		return nil
	}
	fields := make([]*model.SlackAttachmentField, 0, len(lines))
	for _, line := range lines {
		switch line.Type {
		case lineText:
			fields = append(fields, shortField(emptyAs(line.Label, "—"), withSubtitle(emptyAs(line.Value, "—"), line.Subtitle)))
		case lineBadge:
			fields = append(fields, shortField(emptyAs(line.Label, "—"), withSubtitle(emptyAs(line.Text, "—"), line.Subtitle)))
		case lineBarChart:
			fields = append(fields, barChartField(line))
		default:
			fields = append(fields, shortField(emptyAs(line.Label, "—"), "unsupported line type \""+string(line.Type)+"\""))
		}
	}
	return fields
}

// barChartField renders a barChart line as a full-width field: a sparkline in
// an inline-code span, a latest/peak caption, and the chart note in italics.
func barChartField(line metricLine) *model.SlackAttachmentField {
	parts := make([]string, 0, 3)
	if spark := sparkline(line.Points); spark != "" {
		parts = append(parts, "`"+spark+"`")
	}
	if caption := chartCaption(line.Points); caption != "" {
		parts = append(parts, caption)
	}
	if note := strings.TrimSpace(derefString(line.Note)); note != "" {
		parts = append(parts, "_"+note+"_")
	}
	value := "—"
	if len(parts) > 0 {
		value = strings.Join(parts, "\n")
	}
	return &model.SlackAttachmentField{
		Title: emptyAs(line.Label, "—"),
		Value: value,
		Short: false,
	}
}

// sparkline maps chart points onto ▁..█ (8 levels) scaled to the peak value.
func sparkline(points []chartPoint) string {
	if len(points) == 0 {
		return ""
	}
	levels := []rune("▁▂▃▄▅▆▇█")
	peak := 0.0
	for _, p := range points {
		if p.Value > peak {
			peak = p.Value
		}
	}
	var b strings.Builder
	for _, p := range points {
		idx := 0
		if peak > 0 && p.Value > 0 {
			idx = int(math.Ceil(p.Value/peak*8)) - 1
			idx = max(0, min(idx, len(levels)-1))
		}
		b.WriteRune(levels[idx])
	}
	return b.String()
}

// chartCaption summarizes a chart as its latest and peak points.
func chartCaption(points []chartPoint) string {
	if len(points) == 0 {
		return ""
	}
	last := points[len(points)-1]
	peak := points[0]
	for _, p := range points {
		if p.Value > peak.Value {
			peak = p
		}
	}
	return "Latest " + pointLabel(last) + " · Peak " + pointLabel(peak)
}

func pointLabel(p chartPoint) string {
	value := strings.TrimSpace(p.ValueLabel)
	if value == "" {
		value = trimFloat(p.Value)
	}
	if label := strings.TrimSpace(p.Label); label != "" {
		return label + ": " + value
	}
	return value
}

// relativeReset turns an ISO reset timestamp into "resets in 2h" / "resets in
// 3d" relative to now. Empty when there is no timestamp.
func relativeReset(resetsAt *string) string {
	value := strings.TrimSpace(derefString(resetsAt))
	if value == "" {
		return ""
	}
	t, ok := parseISO(value)
	if !ok {
		return "resets " + value
	}
	d := time.Until(t)
	if d <= 0 {
		return "resets now"
	}
	return "resets in " + humanizeDuration(d)
}

// relativeAgo turns an ISO timestamp into "2m ago" / "3h ago" relative to now.
func relativeAgo(iso string) string {
	t, ok := parseISO(strings.TrimSpace(iso))
	if !ok {
		return ""
	}
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	return humanizeDuration(d) + " ago"
}

// humanizeDuration returns a bare magnitude ("45m", "2h", "3d"), rounded to the
// nearest unit. Callers add "in "/" ago".
func humanizeDuration(d time.Duration) string {
	switch {
	case d < time.Hour:
		m := int(math.Round(d.Minutes()))
		if m < 1 {
			m = 1
		}
		return fmt.Sprintf("%dm", m)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(math.Round(d.Hours())))
	default:
		return fmt.Sprintf("%dd", int(math.Round(d.Hours()/24)))
	}
}

// statusDot is a colored severity dot for the card title, matching the card color.
func statusDot(snap providerSnapshot) string {
	switch snapshotColor(snap) {
	case colorError:
		return "🔴"
	case colorWarning:
		return "🟡"
	case colorGood:
		return "🟢"
	default:
		return "🔵"
	}
}

// snapshotColor picks a card color from the worst progress usage in the card.
// Count-format lines are skipped: their direction is ambiguous (a credit
// balance reads "1000/1000" when full), so they must not drive severity.
func snapshotColor(snap providerSnapshot) string {
	maxPct := math.NaN()
	for _, line := range snap.Lines {
		if line.Type != lineProgress || progressFormatKind(line) == formatCount {
			continue
		}
		pct := lineUsedPercent(line)
		if math.IsNaN(pct) {
			continue
		}
		if math.IsNaN(maxPct) || pct > maxPct {
			maxPct = pct
		}
	}
	if math.IsNaN(maxPct) {
		return colorAccent
	}
	switch {
	case maxPct >= 90:
		return colorError
	case maxPct >= 70:
		return colorWarning
	default:
		return colorGood
	}
}

// lineUsedPercent returns the used percentage for a progress line, or NaN when
// it cannot be computed.
func lineUsedPercent(line metricLine) float64 {
	if line.Used == nil {
		return math.NaN()
	}
	used := *line.Used
	if line.Limit != nil && *line.Limit > 0 {
		return used / *line.Limit * 100
	}
	return used
}

func displayName(snap providerSnapshot) string {
	if name := strings.TrimSpace(snap.DisplayName); name != "" {
		return name
	}
	if id := strings.TrimSpace(snap.ProviderID); id != "" {
		return id
	}
	return "Unknown provider"
}

func snapshotFooter(snap providerSnapshot) string {
	footer := "OpenUsage"
	if id := strings.TrimSpace(snap.ProviderID); id != "" {
		footer += " · " + id
	}
	if ago := relativeAgo(snap.FetchedAt); ago != "" {
		footer += " · updated " + ago
	}
	return footer
}

func renderHelp() []*model.SlackAttachment {
	return []*model.SlackAttachment{{
		Title: "OpenUsage commands",
		Text: strings.Join([]string{
			"`/openusage` or `/openusage all` — usage cards for every enabled provider.",
			"`/openusage <provider>` — usage card for one provider (e.g. `claude`, `codex`).",
			"`/openusage help` — show this message.",
		}, "\n"),
		Color:  colorAccent,
		Footer: "Private bot DM only · data from the OpenUsage local HTTP API.",
	}}
}

func errorAttachment(title, text string) *model.SlackAttachment {
	return &model.SlackAttachment{
		Title:  title,
		Text:   text,
		Color:  colorError,
		Footer: "OpenUsage",
	}
}

// --- small formatting helpers ---

func parseISO(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func withSubtitle(value string, subtitle *string) string {
	sub := strings.TrimSpace(derefString(subtitle))
	if sub == "" {
		return value
	}
	return value + "\n_" + sub + "_"
}

func shortField(title, value string) *model.SlackAttachmentField {
	return &model.SlackAttachmentField{Title: title, Value: value, Short: true}
}

func emptyAs(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefFloat(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

// money formats a dollar amount with cent precision, trimming a trailing ".00"
// or zero cents so "$100.00" reads "$100" while "$12.34" keeps its cents.
func money(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return "$" + s
}

func trimFloat(v float64) string {
	if math.Abs(v) >= 100 {
		return fmt.Sprintf("%.0f", v)
	}
	if math.Abs(v) >= 10 {
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", v), "0"), ".")
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}
