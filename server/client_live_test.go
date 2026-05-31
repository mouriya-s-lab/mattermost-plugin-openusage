package main

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestLiveOpenUsage exercises the real fetch + render path against a running
// OpenUsage instance. It is skipped unless OPENUSAGE_LIVE=1 so CI without the
// app stays green. Set OPENUSAGE_BASE_URL to override the default loopback.
//
//	OPENUSAGE_LIVE=1 go test ./server/ -run TestLiveOpenUsage -v
func TestLiveOpenUsage(t *testing.T) {
	if os.Getenv("OPENUSAGE_LIVE") != "1" {
		t.Skip("set OPENUSAGE_LIVE=1 to run against a live OpenUsage instance")
	}
	base := os.Getenv("OPENUSAGE_BASE_URL")
	if base == "" {
		base = defaultBaseURL
	}
	uc := newUsageClient(base, &http.Client{Timeout: 10 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	snaps, err := uc.fetchAll(ctx)
	if err != nil {
		t.Fatalf("fetchAll(%s): %v", base, err)
	}
	t.Logf("fetched %d provider(s) from %s", len(snaps), base)

	atts := renderSnapshots(snaps)
	for _, att := range atts {
		t.Logf("CARD %s [%s] footer=%q", att.Title, att.Color, att.Footer)
		for _, f := range att.Fields {
			t.Logf("    %-14s %v", f.Title, f.Value)
		}
	}

	// Single-provider path against the first provider returned.
	if len(snaps) > 0 {
		one, err := uc.fetchProvider(ctx, snaps[0].ProviderID)
		if err != nil {
			t.Fatalf("fetchProvider(%s): %v", snaps[0].ProviderID, err)
		}
		if one.ProviderID != snaps[0].ProviderID {
			t.Fatalf("single fetch id = %q, want %q", one.ProviderID, snaps[0].ProviderID)
		}
		t.Logf("single-provider fetch ok: %s", one.ProviderID)
	}
}
