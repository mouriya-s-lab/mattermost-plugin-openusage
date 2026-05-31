package main

import (
	"strings"
	"testing"
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
	if len(snap.Lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(snap.Lines))
	}
	if snap.Lines[0].Type != lineProgress || snap.Lines[0].Used == nil || *snap.Lines[0].Used != 42 {
		t.Fatalf("progress line wrong: %+v", snap.Lines[0])
	}
	if snap.Lines[1].Type != lineText || snap.Lines[1].Value == "" {
		t.Fatalf("text line wrong: %+v", snap.Lines[1])
	}
}

func TestRenderSnapshotDocSample(t *testing.T) {
	snap, err := parseSnapshot([]byte(docSample))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	att := renderSnapshot(snap)
	if att.Title != "Claude — Team 5x" {
		t.Errorf("title = %q", att.Title)
	}
	if len(att.Fields) != 2 {
		t.Fatalf("want 2 fields, got %d", len(att.Fields))
	}
	session := att.Fields[0]
	if session.Title != "Session" {
		t.Errorf("field0 title = %q", session.Title)
	}
	val, _ := session.Value.(string)
	if !strings.Contains(val, "42% used") {
		t.Errorf("session value = %q, want 42%% used", val)
	}
	if !strings.Contains(val, "resets 2026-03-26 13:00 UTC") {
		t.Errorf("session value = %q, want reset hint", val)
	}
	if !strings.Contains(att.Footer, "claude") || !strings.Contains(att.Footer, "fetched 2026-03-26 11:16 UTC") {
		t.Errorf("footer = %q", att.Footer)
	}
	// 42% is below the warning threshold → good color.
	if att.Color != colorGood {
		t.Errorf("color = %q, want good", att.Color)
	}
}

func TestProgressFormats(t *testing.T) {
	f := func(kind formatKind, suffix string, used, limit float64, hasLimit bool) string {
		line := metricLine{Type: lineProgress, Used: &used}
		if hasLimit {
			line.Limit = &limit
		}
		line.Format = &lineFormat{Kind: kind, Suffix: suffix}
		return progressMetric(line)
	}
	if got := f(formatPercent, "", 20, 100, true); got != "20% used" {
		t.Errorf("percent = %q", got)
	}
	if got := f(formatDollars, "", 12.34, 100, true); got != "$12.34 of $100" {
		t.Errorf("dollars = %q", got)
	}
	if got := f(formatCount, "credits", 1000, 1000, true); got != "1000 / 1000 credits" {
		t.Errorf("count = %q", got)
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
}

func TestRenderSnapshotsEmpty(t *testing.T) {
	atts := renderSnapshots(nil)
	if len(atts) != 1 || !strings.Contains(atts[0].Text, "No enabled providers") {
		t.Fatalf("unexpected empty rendering: %+v", atts)
	}
}

func TestBadgeAndSubtitle(t *testing.T) {
	sub := "Last sync 5m ago"
	line := metricLine{Type: lineBadge, Label: "Status", Text: "Connected", Subtitle: &sub}
	f := renderLine(line)
	val, _ := f.Value.(string)
	if !strings.Contains(val, "Connected") || !strings.Contains(val, "Last sync 5m ago") {
		t.Errorf("badge value = %q", val)
	}
}
