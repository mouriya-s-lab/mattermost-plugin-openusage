package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *usageClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return newUsageClient(srv.URL, srv.Client())
}

func TestFetchAllOK(t *testing.T) {
	uc := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/usage" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"providerId":"claude","displayName":"Claude","plan":"Pro","lines":[],"fetchedAt":"2026-03-26T08:15:30Z"}]`))
	})
	snaps, err := uc.fetchAll(context.Background())
	if err != nil {
		t.Fatalf("fetchAll: %v", err)
	}
	if len(snaps) != 1 || snaps[0].ProviderID != "claude" {
		t.Fatalf("unexpected: %+v", snaps)
	}
}

func TestFetchAllServerBusy(t *testing.T) {
	uc := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"server_busy"}`))
	})
	_, err := uc.fetchAll(context.Background())
	if !errors.Is(err, errServerBusy) {
		t.Fatalf("err = %v, want errServerBusy", err)
	}
}

func TestFetchProviderStatuses(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr error
		wantID  string
	}{
		{name: "ok", status: 200, body: `{"providerId":"codex","displayName":"Codex","plan":null,"lines":[],"fetchedAt":"x"}`, wantID: "codex"},
		{name: "no content", status: 204, wantErr: errProviderNotCached},
		{name: "not found", status: 404, body: `{"error":"provider_not_found"}`, wantErr: errProviderUnknown},
		{name: "busy", status: 503, body: `{"error":"server_busy"}`, wantErr: errServerBusy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/usage/codex" {
					t.Errorf("path = %q", r.URL.Path)
				}
				w.WriteHeader(tt.status)
				if tt.body != "" {
					_, _ = w.Write([]byte(tt.body))
				}
			})
			snap, err := uc.fetchProvider(context.Background(), "codex")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if snap.ProviderID != tt.wantID {
				t.Fatalf("id = %q, want %q", snap.ProviderID, tt.wantID)
			}
		})
	}
}
