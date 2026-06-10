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
	// Progress bars render as inline-code pills, never a fenced block (which
	// clips horizontally in the Mattermost webapp).
	if strings.Contains(att.Text, "```") {
		t.Errorf("text must not use a fenced block: %q", att.Text)
	}
	if !strings.Contains(att.Text, "`█") {
		t.Errorf("text missing inline-code bar: %q", att.Text)
	}
	if !strings.Contains(att.Text, "42%") {
		t.Errorf("text missing 42%%: %q", att.Text)
	}
	if !strings.Contains(att.Text, "**Session**") {
		t.Errorf("text missing bold label: %q", att.Text)
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

// TestProgressLinesShape asserts each progress line is one markdown line:
// inline-code `bar  value` pill, bold label, then the reset window. Percent
// values pad to a fixed width so pills align across lines.
func TestProgressLinesShape(t *testing.T) {
	mk := func(label string, used float64) metricLine {
		u, l := used, 100.0
		reset := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
		return metricLine{
			Type: lineProgress, Label: label, Used: &u, Limit: &l,
			Format: &lineFormat{Kind: formatPercent}, ResetsAt: &reset,
		}
	}
	got := progressLines([]metricLine{mk("Session", 20), mk("Spark Weekly", 0)})
	rows := strings.Split(got, "\n")
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d: %q", len(rows), got)
	}
	if rows[0] != "`██░░░░░░░░░░   20%` **Session** · resets in 2h" {
		t.Errorf("row 0 = %q", rows[0])
	}
	if rows[1] != "`░░░░░░░░░░░░    0%` **Spark Weekly** · resets in 2h" {
		t.Errorf("row 1 = %q", rows[1])
	}
	// Pills end at the same rune column: bar width + 2 + padded value.
	if w0, w1 := strings.Index(rows[0], "` "), strings.Index(rows[1], "` "); w0 != w1 {
		t.Errorf("pills not aligned: %d vs %d", w0, w1)
	}
}

// liveSample mirrors the real /v1/usage shape observed in production
// (progress + spend text + barChart trend + model-mix text).
const liveSample = `{
  "providerId": "codex",
  "displayName": "Codex",
  "plan": "Pro 20x",
  "lines": [
    {"type":"progress","label":"Weekly","used":44.0,"limit":100.0,"format":{"kind":"percent"},"resetsAt":"2026-06-11T04:32:49.000Z","periodDurationMs":604800000,"color":null},
    {"type":"progress","label":"Credits","used":1000.0,"limit":1000.0,"format":{"kind":"count","suffix":"credits"},"resetsAt":null,"periodDurationMs":null,"color":null},
    {"type":"text","label":"Today","value":"$415.12 · 474M tokens","color":null,"subtitle":null},
    {"type":"barChart","label":"Usage Trend","points":[
      {"label":"6/8","value":571809733.0,"valueLabel":"572M tokens"},
      {"label":"6/9","value":516472937.0,"valueLabel":"516M tokens"},
      {"label":"6/10","value":164600215.0,"valueLabel":"165M tokens"}],
     "note":"Estimated from local Codex logs.","color":"#74AA9C"},
    {"type":"text","label":"gpt-5.5","value":"99.9%","color":null,"subtitle":null}
  ],
  "fetchedAt":"2026-06-10T11:26:09Z"
}`

func TestRenderLiveSampleCoversAllVariants(t *testing.T) {
	snap, err := parseSnapshot([]byte(liveSample))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	att := renderSnapshot(snap)

	if strings.Contains(att.Text, "unsupported line type") {
		t.Errorf("text has unsupported placeholder: %q", att.Text)
	}
	// Fields preserve API order: Today, Usage Trend, gpt-5.5.
	if len(att.Fields) != 3 {
		t.Fatalf("fields = %+v", att.Fields)
	}
	for i, want := range []string{"Today", "Usage Trend", "gpt-5.5"} {
		if att.Fields[i].Title != want {
			t.Errorf("field %d title = %q, want %q", i, att.Fields[i].Title, want)
		}
		v, _ := att.Fields[i].Value.(string)
		if strings.Contains(v, "unsupported line type") {
			t.Errorf("field %d has unsupported placeholder: %q", i, v)
		}
	}
	// Trend renders full-width with a sparkline scaled to the peak and a
	// latest/peak caption plus the note.
	trend := att.Fields[1]
	if trend.Short {
		t.Errorf("trend field should be full-width")
	}
	v, _ := trend.Value.(string)
	if !strings.Contains(v, "`██▃`") {
		t.Errorf("sparkline wrong: %q", v)
	}
	if !strings.Contains(v, "Latest 6/10: 165M tokens · Peak 6/8: 572M tokens") {
		t.Errorf("caption wrong: %q", v)
	}
	if !strings.Contains(v, "_Estimated from local Codex logs._") {
		t.Errorf("note wrong: %q", v)
	}
	// Credits (count) renders with its raw value and must not drive severity:
	// Weekly 44% is the worst percent line -> green is wrong, want good color?
	// 44% < 70 -> good. A red card here would mean credits drove severity.
	if att.Color != colorGood {
		t.Errorf("color = %q, want good (count line must not drive severity)", att.Color)
	}
	if !strings.Contains(att.Text, "1000/1000 credits") {
		t.Errorf("text missing credits value: %q", att.Text)
	}
}

func TestSparklineScaling(t *testing.T) {
	pts := []chartPoint{{Value: 0}, {Value: 1}, {Value: 50}, {Value: 100}}
	if got := sparkline(pts); got != "▁▁▄█" {
		t.Errorf("sparkline = %q, want ▁▁▄█", got)
	}
	if got := sparkline(nil); got != "" {
		t.Errorf("sparkline(nil) = %q", got)
	}
	// All-zero charts stay flat instead of dividing by zero.
	if got := sparkline([]chartPoint{{Value: 0}, {Value: 0}}); got != "▁▁" {
		t.Errorf("sparkline(zeros) = %q", got)
	}
}
