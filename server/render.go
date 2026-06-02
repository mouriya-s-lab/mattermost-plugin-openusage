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

// renderSnapshot builds one provider card. Progress limits render as aligned
// Unicode bars in a monospace block (mirroring the OpenUsage menubar); text and
// badge lines render as a compact two-column field grid below.
func renderSnapshot(snap providerSnapshot) *model.SlackAttachment {
	title := statusDot(snap) + " " + displayName(snap)
	if plan := strings.TrimSpace(derefString(snap.Plan)); plan != "" {
		title += "  ·  " + plan
	}

	var progress, textual []metricLine
	for _, line := range snap.Lines {
		switch line.Type {
		case lineProgress:
			progress = append(progress, line)
		default:
			textual = append(textual, line)
		}
	}

	att := &model.SlackAttachment{
		Title:  title,
		Color:  snapshotColor(snap),
		Footer: snapshotFooter(snap),
	}
	att.Text = progressBlock(progress)
	att.Fields = textualFields(textual)
	if att.Text == "" && len(att.Fields) == 0 {
		att.Text = "_No usage lines returned._"
	}
	return att
}

// progressBlock renders the progress lines as an aligned monospace table. The
// bar row is `label  bar  value`; when a line has a reset window it goes on its
// own indented line *below* the bar (`↳ resets in …`) so the bar row stays
// narrow and the reset is visible without expanding the card.
func progressBlock(lines []metricLine) string {
	if len(lines) == 0 {
		return ""
	}

	type row struct{ label, bar, value, reset string }
	rows := make([]row, 0, len(lines))
	labelW := 0
	for _, line := range lines {
		r := row{
			label: emptyAs(strings.TrimSpace(line.Label), "—"),
			bar:   usageBar(lineUsedPercent(line)),
			value: progressValue(line),
			reset: relativeReset(line.ResetsAt),
		}
		labelW = max(labelW, len([]rune(r.label)))
		rows = append(rows, r)
	}

	// Reset lines indent to the bar's start column so they read as a caption.
	indent := strings.Repeat(" ", labelW+2)
	var b strings.Builder
	b.WriteString("```\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%s  %s  %s\n", padRune(r.label, labelW), r.bar, r.value)
		if r.reset != "" {
			b.WriteString(indent + "↳ " + r.reset + "\n")
		}
	}
	b.WriteString("```")
	return b.String()
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

// textualFields renders text/badge lines as a two-column field grid.
func textualFields(lines []metricLine) []*model.SlackAttachmentField {
	if len(lines) == 0 {
		return nil
	}
	fields := make([]*model.SlackAttachmentField, 0, len(lines))
	for _, line := range lines {
		var value string
		switch line.Type {
		case lineText:
			value = emptyAs(line.Value, "—")
		case lineBadge:
			value = emptyAs(line.Text, "—")
		default:
			value = "unsupported line type \"" + string(line.Type) + "\""
		}
		fields = append(fields, shortField(emptyAs(line.Label, "—"), withSubtitle(value, line.Subtitle)))
	}
	return fields
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
func snapshotColor(snap providerSnapshot) string {
	maxPct := math.NaN()
	for _, line := range snap.Lines {
		if line.Type != lineProgress {
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

// padRune right-pads s with spaces to w display runes (rune-aware so multibyte
// labels still align in the monospace block).
func padRune(s string, w int) string {
	n := len([]rune(s))
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
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
