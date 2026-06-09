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
	// Progress bar lives in the monospace Text block.
	if !strings.Contains(att.Text, "```") || !strings.Contains(att.Text, "█") {
		t.Errorf("text missing bar block: %q", att.Text)
	}
	if !strings.Contains(att.Text, "42%") {
		t.Errorf("text missing 42%%: %q", att.Text)
	}
	if !strings.Contains(att.Text, "Session") {
		t.Errorf("text missing label: %q", att.Text)
	}
	// Text line becomes a field.
	if len(att.Fields) != 1 || att.Fields[0].Title != "Today" {
		t.Fatalf("fields = %+v", att.Fields)
	}
	if v, _ := att.Fields[0].Value.(string); !strings.Contains(v, "$5.17") {
		t.Errorf("today field = %q", v)
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
		{0, 0}, {100, barWidth}, {50, barWidth / 2}, {42, 5}, {200, barWidth}, {-5, 0},
	}
	for _, tt := range tests {
		bar := usageBar(tt.pct)
		if got := strings.Count(bar, "█"); got != tt.wantFilled {
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

func TestBadgeAndSubtitleField(t *testing.T) {
	sub := "Last sync 5m ago"
	snap := providerSnapshot{
		ProviderID:  "x",
		DisplayName: "X",
		Lines:       []metricLine{{Type: lineBadge, Label: "Status", Text: "Connected", Subtitle: &sub}},
	}
	att := renderSnapshot(snap)
	if len(att.Fields) != 1 {
		t.Fatalf("fields = %+v", att.Fields)
	}
	v, _ := att.Fields[0].Value.(string)
	if !strings.Contains(v, "Connected") || !strings.Contains(v, "Last sync 5m ago") {
		t.Errorf("badge field = %q", v)
	}
}

func TestProgressBlockAligns(t *testing.T) {
	mk := func(label string, used float64) metricLine {
		u, l := used, 100.0
		return metricLine{Type: lineProgress, Label: label, Used: &u, Limit: &l, Format: &lineFormat{Kind: formatPercent}}
	}
	block := progressBlock([]metricLine{mk("Session", 20), mk("Spark Weekly", 0)})
	lines := strings.Split(strings.Trim(block, "`\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 rows, got %d: %q", len(lines), block)
	}
	// Bars start at the same column => labels padded to equal width.
	c0 := strings.Index(lines[0], "█")
	c1 := strings.Index(lines[1], "░")
	if c0 != c1 {
		t.Errorf("bars not aligned: %d vs %d\n%s", c0, c1, block)
	}
}

// TestProgressResetBelowBar asserts the reset window is on its own line below
// the bar (not trailing the bar row), indented under the bar's start column.
func TestProgressResetBelowBar(t *testing.T) {
	u, l := 42.0, 100.0
	reset := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	line := metricLine{
		Type: lineProgress, Label: "Session", Used: &u, Limit: &l,
		Format: &lineFormat{Kind: formatPercent}, ResetsAt: &reset,
	}
	block := progressBlock([]metricLine{line})
	rows := strings.Split(strings.Trim(block, "`\n"), "\n")
	if len(rows) != 2 {
		t.Fatalf("want bar row + reset row, got %d: %q", len(rows), block)
	}
	if !strings.Contains(rows[0], "█") || strings.Contains(rows[0], "resets") {
		t.Errorf("bar row should hold the bar and no reset text: %q", rows[0])
	}
	if !strings.Contains(rows[1], "resets in 2h") {
		t.Errorf("reset row missing reset window: %q", rows[1])
	}
	// Reset row indents past the label so it sits under the bar.
	barCol := strings.Index(rows[0], "█")
	resetCol := strings.IndexFunc(rows[1], func(r rune) bool { return r != ' ' })
	if resetCol < barCol {
		t.Errorf("reset row not indented under bar: resetCol=%d barCol=%d\n%s", resetCol, barCol, block)
	}
}
