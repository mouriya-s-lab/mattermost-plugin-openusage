package main

import (
	"strings"
	"testing"
	"time"
)

// docSample is the exact response shape from docs/local-http-api.md.
const docSample = `{
  "providerId": "claude",
  "displayName": "Claude",
  "plan": "Team 5x",
  "lines": [
    {"type":"progress","label":"Session","used":42.0,"limit":100.0,"format":{"kind":"percent"},"resetsAt":"2026-03-26T13:00:00.161Z","periodDurationMs":18000000,"color":null},
    {"type":"text","label":"Today","value":"$5.17 · 9.2M tokens","color":null,"subtitle":null}
  ],
  "fetchedAt":"2026-03-26T11:16:29Z"
}`

func TestParseSnapshotDocSample(t *testing.T) {
	snap, err := parseSnapshot([]byte(docSample))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if snap.ProviderID != "claude" || snap.DisplayName != "Claude" {
		t.Fatalf("ids wrong: %+v", snap)
	}
	if snap.Plan == nil || *snap.Plan != "Team 5x" {
		t.Fatalf("plan wrong: %+v", snap.Plan)
	}
	if len(snap.Lines) != 2 || snap.Lines[0].Type != lineProgress || snap.Lines[1].Type != lineText {
		t.Fatalf("lines wrong: %+v", snap.Lines)
	}
	if snap.Lines[0].Used == nil || *snap.Lines[0].Used != 42 {
		t.Fatalf("progress used wrong: %+v", snap.Lines[0])
	}
}

func TestRenderSnapshotDocSample(t *testing.T) {
	snap, _ := parseSnapshot([]byte(docSample))
	att := renderSnapshot(snap)

	if !strings.Contains(att.Title, "Claude") || !strings.Contains(att.Title, "Team 5x") {
		t.Errorf("title = %q", att.Title)
	}
	if !strings.HasPrefix(att.Title, "🟢") { // 42% -> good
		t.Errorf("title should lead with green dot: %q", att.Title)
	}
	// Progress, text, and badges live in markdown Text, not Mattermost short
	// fields/code blocks, because those clipped in production.
	if strings.Contains(att.Text, "```") || len(att.Fields) != 0 {
		t.Errorf("text should avoid code blocks and fields: text=%q fields=%+v", att.Text, att.Fields)
	}
	if !strings.Contains(att.Text, "■") {
		t.Errorf("text missing inline bar: %q", att.Text)
	}
	if !strings.Contains(att.Text, "42%") {
		t.Errorf("text missing 42%%: %q", att.Text)
	}
	if !strings.Contains(att.Text, "Session") {
		t.Errorf("text missing label: %q", att.Text)
	}
	if !strings.Contains(att.Text, "Today") || !strings.Contains(att.Text, "$5.17") {
		t.Errorf("text missing Today detail: %q", att.Text)
	}
	if att.Color != colorGood {
		t.Errorf("color = %q, want good", att.Color)
	}
}

func TestUsageBar(t *testing.T) {
	tests := []struct {
		pct        float64
		wantFilled int
	}{
		{0, 0}, {100, barWidth}, {50, barWidth / 2}, {42, 4}, {200, barWidth}, {-5, 0},
	}
	for _, tt := range tests {
		bar := usageBar(tt.pct)
		if got := strings.Count(bar, "■"); got != tt.wantFilled {
			t.Errorf("usageBar(%v) filled=%d, want %d (bar=%q)", tt.pct, got, tt.wantFilled, bar)
		}
		if total := len([]rune(bar)); total != barWidth {
			t.Errorf("usageBar(%v) width=%d, want %d", tt.pct, total, barWidth)
		}
	}
}

func TestProgressValue(t *testing.T) {
	mk := func(kind formatKind, suffix string, used float64, limit *float64) metricLine {
		u := used
		return metricLine{Type: lineProgress, Used: &u, Limit: limit, Format: &lineFormat{Kind: kind, Suffix: suffix}}
	}
	hundred := 100.0
	credits := 1000.0
	if got := progressValue(mk(formatPercent, "", 20, &hundred)); got != "20%" {
		t.Errorf("percent = %q", got)
	}
	if got := progressValue(mk(formatDollars, "", 12.34, &hundred)); got != "$12.34 / $100" {
		t.Errorf("dollars = %q", got)
	}
	if got := progressValue(mk(formatCount, "credits", 1000, &credits)); got != "1000/1000 credits" {
		t.Errorf("count = %q", got)
	}
}

func TestRelativeReset(t *testing.T) {
	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	s := future
	if got := relativeReset(&s); got != "resets in 2h" {
		t.Errorf("relativeReset(+2h) = %q, want 'resets in 2h'", got)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if got := relativeReset(&past); got != "resets now" {
		t.Errorf("relativeReset(past) = %q, want 'resets now'", got)
	}
	if got := relativeReset(nil); got != "" {
		t.Errorf("relativeReset(nil) = %q, want empty", got)
	}
}

func TestSnapshotColorWorstWins(t *testing.T) {
	mk := func(used float64) metricLine {
		u, l := used, 100.0
		return metricLine{Type: lineProgress, Used: &u, Limit: &l, Format: &lineFormat{Kind: formatPercent}}
	}
	snap := providerSnapshot{Lines: []metricLine{mk(10), mk(95), mk(50)}}
	if got := snapshotColor(snap); got != colorError {
		t.Errorf("color = %q, want error (95%%)", got)
	}
	if dot := statusDot(snap); dot != "🔴" {
		t.Errorf("dot = %q, want red", dot)
	}
}

func TestRenderSnapshotsEmpty(t *testing.T) {
	atts := renderSnapshots(nil)
	if len(atts) != 1 || !strings.Contains(atts[0].Text, "No enabled providers") {
		t.Fatalf("unexpected empty rendering: %+v", atts)
	}
}

func TestBadgeAndSubtitleText(t *testing.T) {
	sub := "Last sync 5m ago"
	snap := providerSnapshot{
		ProviderID:  "x",
		DisplayName: "X",
		Lines:       []metricLine{{Type: lineBadge, Label: "Status", Text: "Connected", Subtitle: &sub}},
	}
	att := renderSnapshot(snap)
	if len(att.Fields) != 0 {
		t.Fatalf("fields should stay empty to avoid Mattermost clipping: %+v", att.Fields)
	}
	if !strings.Contains(att.Text, "Connected") || !strings.Contains(att.Text, "Last sync 5m ago") {
		t.Errorf("badge text = %q", att.Text)
	}
}

func TestProgressSectionRendersInlineRows(t *testing.T) {
	mk := func(label string, used float64) metricLine {
		u, l := used, 100.0
		return metricLine{Type: lineProgress, Label: label, Used: &u, Limit: &l, Format: &lineFormat{Kind: formatPercent}}
	}
	block := progressSection([]metricLine{mk("Session", 20), mk("Spark Weekly", 0)})
	lines := strings.Split(block, "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 rows, got %d: %q", len(lines), block)
	}
	for _, want := range []string{"• **Session**", "20%", "• **Spark Weekly**", "0%"} {
		if !strings.Contains(block, want) {
			t.Errorf("progress section missing %q: %q", want, block)
		}
	}
	if strings.Contains(block, "```") {
		t.Errorf("progress section should not use code fences: %q", block)
	}
}

func TestProgressResetInline(t *testing.T) {
	u, l := 42.0, 100.0
	reset := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	line := metricLine{
		Type: lineProgress, Label: "Session", Used: &u, Limit: &l,
		Format: &lineFormat{Kind: formatPercent}, ResetsAt: &reset,
	}
	block := progressSection([]metricLine{line})
	if strings.Count(block, "\n") != 0 {
		t.Fatalf("single progress line should stay on one markdown row: %q", block)
	}
	if !strings.Contains(block, "■") || !strings.Contains(block, "42%") || !strings.Contains(block, "resets in 2h") {
		t.Errorf("progress row missing data: %q", block)
	}
}

func TestBarChartRendersWithoutUnsupportedLine(t *testing.T) {
	snap := providerSnapshot{
		ProviderID:  "codex",
		DisplayName: "Codex",
		Lines: []metricLine{
			{
				Type:  lineBarChart,
				Label: "Usage Trend",
				Points: []chartPoint{
					{Label: "6/7", Value: 10, ValueLabel: "10M tokens"},
					{Label: "6/8", Value: 30, ValueLabel: "30M tokens"},
					{Label: "6/9", Value: 20, ValueLabel: "20M tokens"},
				},
				Note: strPtr("Estimated from logs."),
			},
		},
	}
	cards := renderSnapshotCards(snap)
	if len(cards) != 2 {
		t.Fatalf("want main + chart cards, got %d: %+v", len(cards), cards)
	}
	att := cards[1]
	for _, want := range []string{"Usage Trend", "▃█▆", "Latest 6/9: 20M tokens", "Peak 6/8: 30M tokens", "Estimated from logs."} {
		haystack := att.Title + "\n" + att.Text
		if !strings.Contains(haystack, want) {
			t.Errorf("chart render missing %q: title=%q text=%q", want, att.Title, att.Text)
		}
	}
	if strings.Contains(att.Text, "unsupported line type") {
		t.Errorf("chart should not render unsupported marker: %q", att.Text)
	}
}

func TestRenderSnapshotCardsSplitsModelMix(t *testing.T) {
	snap := providerSnapshot{
		ProviderID:  "claude",
		DisplayName: "Claude",
		Lines: []metricLine{
			{Type: lineText, Label: "Today", Value: "$1.23 · 1M tokens"},
			{Type: lineText, Label: "claude-opus", Value: "99.9%"},
			{Type: lineText, Label: "claude-haiku", Value: "<0.1%"},
		},
	}
	cards := renderSnapshotCards(snap)
	if len(cards) != 2 {
		t.Fatalf("want main + model cards, got %d: %+v", len(cards), cards)
	}
	if strings.Contains(cards[0].Text, "claude-opus") || !strings.Contains(cards[0].Text, "Today") {
		t.Errorf("main card should keep spend but not model mix: %q", cards[0].Text)
	}
	if !strings.Contains(cards[1].Title, "Model mix") || !strings.Contains(cards[1].Text, "claude-opus") || !strings.Contains(cards[1].Text, "claude-haiku") {
		t.Errorf("model card wrong: title=%q text=%q", cards[1].Title, cards[1].Text)
	}
}

func strPtr(s string) *string { return &s }
