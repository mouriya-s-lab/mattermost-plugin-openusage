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

func renderSnapshot(snap providerSnapshot) *model.SlackAttachment {
	title := displayName(snap)
	if plan := strings.TrimSpace(derefString(snap.Plan)); plan != "" {
		title += " — " + plan
	}

	fields := make([]*model.SlackAttachmentField, 0, len(snap.Lines))
	for _, line := range snap.Lines {
		if f := renderLine(line); f != nil {
			fields = append(fields, f)
		}
	}

	return &model.SlackAttachment{
		Title:  title,
		Color:  snapshotColor(snap),
		Fields: fields,
		Footer: snapshotFooter(snap),
	}
}

func renderLine(line metricLine) *model.SlackAttachmentField {
	switch line.Type {
	case lineText:
		return shortField(line.Label, withSubtitle(emptyAs(line.Value, "—"), line.Subtitle))
	case lineBadge:
		return shortField(line.Label, withSubtitle(emptyAs(line.Text, "—"), line.Subtitle))
	case lineProgress:
		return shortField(line.Label, withSubtitle(formatProgress(line), line.Subtitle))
	default:
		// Unknown line type: surface its label rather than dropping it silently.
		return shortField(emptyAs(line.Label, "Line"), "unsupported line type \""+string(line.Type)+"\"")
	}
}

// formatProgress renders a progress line's used/limit plus a reset hint.
func formatProgress(line metricLine) string {
	parts := []string{}
	if metric := progressMetric(line); metric != "" {
		parts = append(parts, metric)
	}
	if reset := resetHint(line.ResetsAt); reset != "" {
		parts = append(parts, reset)
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " · ")
}

func progressMetric(line metricLine) string {
	used := derefFloat(line.Used)
	limit := derefFloat(line.Limit)
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
			return fmt.Sprintf("%s of %s", money(used), money(limit))
		}
		return money(used)
	case formatCount:
		unit := ""
		if suffix != "" {
			unit = " " + suffix
		}
		if line.Limit != nil {
			return fmt.Sprintf("%s / %s%s", trimFloat(used), trimFloat(limit), unit)
		}
		return trimFloat(used) + unit
	case formatPercent:
		fallthrough
	default:
		return percentUsed(used, limit, line.Limit != nil) + " used"
	}
}

// percentUsed converts used/limit into a percentage. OpenUsage percent lines use
// a 0..limit scale (limit is typically 100), so the percentage is used/limit*100.
func percentUsed(used, limit float64, hasLimit bool) string {
	if hasLimit && limit > 0 {
		return percent(used / limit * 100)
	}
	return percent(used)
}

func resetHint(resetsAt *string) string {
	value := strings.TrimSpace(derefString(resetsAt))
	if value == "" {
		return ""
	}
	return "resets " + formatTime(value)
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
	if ts := strings.TrimSpace(snap.FetchedAt); ts != "" {
		footer += " · fetched " + formatTime(ts)
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
// or zero cents so "$100.00" reads "$100" while "$12.34" keeps its cents. Unlike
// trimFloat it never sacrifices cents for larger magnitudes.
func money(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return "$" + s
}

func percent(v float64) string {
	return trimFloat(v) + "%"
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

func formatTime(value string) string {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC().Format("2006-01-02 15:04 UTC")
		}
	}
	return value
}
