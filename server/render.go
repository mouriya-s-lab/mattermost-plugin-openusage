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

	barWidth = 10
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
		out = append(out, renderSnapshotCards(snap)...)
	}
	return out
}

func renderSnapshotCards(snap providerSnapshot) []*model.SlackAttachment {
	return []*model.SlackAttachment{renderSnapshot(snap)}
}

// renderSnapshot builds one provider card. Everything lives in attachment Text
// instead of Mattermost "short" fields or fenced code blocks: production showed
// those render paths clip columns and mangle block glyphs in narrow browser
// cards. Plain markdown lines wrap naturally and keep every OpenUsage line
// visible.
func renderSnapshot(snap providerSnapshot) *model.SlackAttachment {
	title := statusDot(snap) + " " + displayName(snap)
	if plan := strings.TrimSpace(derefString(snap.Plan)); plan != "" {
		title += "  ·  " + plan
	}

	main, _, modelMix := splitSnapshotLines(snap)
	att := &model.SlackAttachment{
		Title:  title,
		Color:  snapshotColor(snap),
		Footer: snapshotFooter(snap),
	}
	att.Fields = providerFields(main.progress, main.textual, modelMix)
	if len(att.Fields) == 0 {
		att.Text = "_No usage lines returned._"
	}
	return att
}

func providerFields(progress, spend, models []metricLine) []*model.SlackAttachmentField {
	fields := make([]*model.SlackAttachmentField, 0, 3)
	if s := limitFieldValue(progress); s != "" {
		fields = append(fields, fullField("Limits", s))
	}
	if s := spendFieldValue(spend); s != "" {
		fields = append(fields, fullField("Spend", s))
	}
	if s := modelsFieldValue(models); s != "" {
		fields = append(fields, fullField("Models", s))
	}
	return fields
}

func limitFieldValue(lines []metricLine) string {
	if len(lines) == 0 {
		return ""
	}
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		value := progressValue(line)
		if reset := shortReset(line.ResetsAt); reset != "" {
			value += " / " + reset
		}
		parts = append(parts, emptyAs(strings.TrimSpace(line.Label), "—")+" "+value)
	}
	return strings.Join(parts, " · ")
}

func spendFieldValue(lines []metricLine) string {
	if len(lines) == 0 {
		return ""
	}
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		if line.Type != lineText && line.Type != lineBadge {
			continue
		}
		parts = append(parts, compactMetricLabel(line.Label)+" "+lineDisplayValue(line))
	}
	return strings.Join(parts, " · ")
}

func modelsFieldValue(lines []metricLine) string {
	if len(lines) == 0 {
		return ""
	}
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		if line.Type != lineText && line.Type != lineBadge {
			continue
		}
		parts = append(parts, compactModelLabel(line.Label)+" "+lineDisplayValue(line))
	}
	return strings.Join(parts, " · ")
}

type mainSnapshotLines struct {
	progress []metricLine
	textual  []metricLine
}

func splitSnapshotLines(snap providerSnapshot) (mainSnapshotLines, []metricLine, []metricLine) {
	var main mainSnapshotLines
	var charts, modelMix []metricLine
	for _, line := range snap.Lines {
		switch line.Type {
		case lineProgress:
			main.progress = append(main.progress, line)
		case lineBarChart:
			charts = append(charts, line)
		case lineText:
			if isPercentValue(line.Value) {
				modelMix = append(modelMix, line)
			} else {
				main.textual = append(main.textual, line)
			}
		default:
			main.textual = append(main.textual, line)
		}
	}
	return main, charts, modelMix
}

func isPercentValue(value string) bool {
	v := strings.TrimSpace(value)
	return strings.HasSuffix(v, "%")
}

func snapshotText(progress, textual []metricLine) string {
	var sections []string
	if s := progressSection(progress); s != "" {
		sections = append(sections, "**Limits**\n"+s)
	}
	if s := textualSection(textual); s != "" {
		sections = append(sections, "**Details**\n"+s)
	}
	return strings.Join(sections, "\n\n")
}

// progressSection renders each limit as a single markdown bullet. Keeping the
// reset in the same bullet avoids the production clipping caused by the old
// fenced monospace table's continuation rows.
func progressSection(lines []metricLine) string {
	if len(lines) == 0 {
		return ""
	}

	rows := make([]string, 0, len(lines))
	for _, line := range lines {
		value := progressValue(line)
		if reset := relativeReset(line.ResetsAt); reset != "" {
			value += " · " + reset
		}
		rows = append(rows, "**"+emptyAs(strings.TrimSpace(line.Label), "—")+"**\n"+value+"\n"+usageBar(lineUsedPercent(line)))
	}
	return strings.Join(rows, "\n\n")
}

// usageBar draws a fixed-width inline bar from a 0..100 percentage. NaN renders
// empty. Simple geometric squares render reliably in Mattermost proportional
// text; the old block characters inside a code fence rendered as clipped columns in
// production.
func usageBar(pct float64) string {
	if math.IsNaN(pct) {
		return strings.Repeat("□", barWidth)
	}
	pct = math.Max(0, math.Min(100, pct))
	filled := int(math.Round(pct / 100 * barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	return strings.Repeat("■", filled) + strings.Repeat("□", barWidth-filled)
}

func textualSection(lines []metricLine) string {
	if len(lines) == 0 {
		return ""
	}
	rows := make([]string, 0, len(lines))
	for _, line := range lines {
		switch line.Type {
		case lineText:
			rows = append(rows, "• **"+emptyAs(line.Label, "—")+"** "+withSubtitle(emptyAs(line.Value, "—"), line.Subtitle))
		case lineBadge:
			rows = append(rows, "• **"+emptyAs(line.Label, "—")+"** "+withSubtitle(emptyAs(line.Text, "—"), line.Subtitle))
		}
	}
	return strings.Join(rows, "\n")
}

func compactTextualSection(lines []metricLine) string {
	if len(lines) == 0 {
		return ""
	}
	rows := make([]string, 0, len(lines))
	for _, line := range lines {
		switch line.Type {
		case lineText:
			rows = append(rows, "**"+emptyAs(line.Label, "—")+"**\n"+withSubtitle(emptyAs(line.Value, "—"), line.Subtitle))
		case lineBadge:
			rows = append(rows, "**"+emptyAs(line.Label, "—")+"**\n"+withSubtitle(emptyAs(line.Text, "—"), line.Subtitle))
		}
	}
	return strings.Join(rows, "\n\n")
}

func fullField(title, value string) *model.SlackAttachmentField {
	return &model.SlackAttachmentField{Title: title, Value: value, Short: false}
}

func lineDisplayValue(line metricLine) string {
	switch line.Type {
	case lineText:
		return plainWithSubtitle(emptyAs(line.Value, "—"), line.Subtitle)
	case lineBadge:
		return plainWithSubtitle(emptyAs(line.Text, "—"), line.Subtitle)
	default:
		return "—"
	}
}

func compactMetricLabel(label string) string {
	switch strings.TrimSpace(label) {
	case "Last 30 Days":
		return "30d"
	default:
		return emptyAs(label, "—")
	}
}

func compactModelLabel(label string) string {
	label = strings.TrimSpace(label)
	label = strings.TrimPrefix(label, "claude-")
	label = strings.TrimPrefix(label, "gpt-")
	return emptyAs(label, "—")
}

func shortReset(resetsAt *string) string {
	value := strings.TrimSpace(derefString(resetsAt))
	if value == "" {
		return ""
	}
	t, ok := parseISO(value)
	if !ok {
		return "reset " + value
	}
	d := time.Until(t)
	if d <= 0 {
		return "reset now"
	}
	return "reset " + humanizeDuration(d)
}

func plainWithSubtitle(value string, subtitle *string) string {
	sub := strings.TrimSpace(derefString(subtitle))
	if sub == "" {
		return value
	}
	return value + " (" + sub + ")"
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

func progressColor(line metricLine) string {
	pct := lineUsedPercent(line)
	if math.IsNaN(pct) {
		return colorAccent
	}
	switch {
	case pct >= 90:
		return colorError
	case pct >= 70:
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
