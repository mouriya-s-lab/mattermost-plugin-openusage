package main

import (
	"encoding/json"
	"fmt"
)

// The types below mirror the OpenUsage local HTTP API response documented at
// docs/local-http-api.md and defined in the OpenUsage source
// (src-tauri/src/local_http_api/cache.rs, plugin schema docs/plugins/schema.md).
//
// OpenUsage models metric lines as a discriminated union tagged by "type".
// Go has no native sum type, so we keep the tag explicit (lineKind), decode the
// superset of fields once at the HTTP boundary, and dispatch on the tag in the
// renderer. Fields that only apply to one variant are pointers so we can tell
// "absent" from "zero".

type lineKind string

const (
	lineText     lineKind = "text"
	lineProgress lineKind = "progress"
	lineBadge    lineKind = "badge"
	lineBarChart lineKind = "barChart"
)

type formatKind string

const (
	formatPercent formatKind = "percent"
	formatDollars formatKind = "dollars"
	formatCount   formatKind = "count"
)

// providerSnapshot is one element of GET /v1/usage, and the whole body of
// GET /v1/usage/:providerId.
type providerSnapshot struct {
	ProviderID  string       `json:"providerId"`
	DisplayName string       `json:"displayName"`
	Plan        *string      `json:"plan"`
	Lines       []metricLine `json:"lines"`
	FetchedAt   string       `json:"fetchedAt"`
}

// lineFormat is the progress-line value format.
type lineFormat struct {
	Kind   formatKind `json:"kind"`
	Suffix string     `json:"suffix,omitempty"`
}

// metricLine is the decoded superset of all three line variants. The Type field
// is the discriminator; only the fields belonging to that variant are populated.
type metricLine struct {
	Type  lineKind `json:"type"`
	Label string   `json:"label"`

	// text + badge
	Color    *string `json:"color"`
	Subtitle *string `json:"subtitle"`

	// text
	Value string `json:"value"`

	// badge
	Text string `json:"text"`

	// progress
	Used             *float64    `json:"used"`
	Limit            *float64    `json:"limit"`
	Format           *lineFormat `json:"format"`
	ResetsAt         *string     `json:"resetsAt"`
	PeriodDurationMs *int64      `json:"periodDurationMs"`

	// barChart
	Points []chartPoint `json:"points"`
	Note   *string      `json:"note"`
}

// chartPoint is one bar of a barChart line.
type chartPoint struct {
	Label      string  `json:"label"`
	Value      float64 `json:"value"`
	ValueLabel string  `json:"valueLabel"`
}

// parseSnapshots decodes the GET /v1/usage array body.
func parseSnapshots(body []byte) ([]providerSnapshot, error) {
	var snaps []providerSnapshot
	if err := json.Unmarshal(body, &snaps); err != nil {
		return nil, fmt.Errorf("decode usage array: %w", err)
	}
	return snaps, nil
}

// parseSnapshot decodes the GET /v1/usage/:providerId object body.
func parseSnapshot(body []byte) (providerSnapshot, error) {
	var snap providerSnapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		return providerSnapshot{}, fmt.Errorf("decode usage object: %w", err)
	}
	return snap, nil
}
