package main

import "testing"

func TestBuildOpenusageRequest(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantMode commandMode
		wantProv string
		wantErr  bool
	}{
		{name: "empty defaults to all", raw: "/openusage", wantMode: modeAll},
		{name: "all", raw: "/openusage all", wantMode: modeAll},
		{name: "summary alias", raw: "/openusage summary", wantMode: modeAll},
		{name: "help", raw: "/openusage help", wantMode: modeHelp},
		{name: "bare provider", raw: "/openusage claude", wantMode: modeProvider, wantProv: "claude"},
		{name: "provider keyword", raw: "/openusage provider codex", wantMode: modeProvider, wantProv: "codex"},
		{name: "kebab provider", raw: "/openusage jetbrains-ai", wantMode: modeProvider, wantProv: "jetbrains-ai"},
		{name: "uppercased provider normalizes", raw: "/openusage CLAUDE", wantMode: modeProvider, wantProv: "claude"},
		{name: "all rejects extra args", raw: "/openusage all claude", wantErr: true},
		{name: "provider needs id", raw: "/openusage provider", wantErr: true},
		{name: "path traversal rejected", raw: "/openusage ../secrets", wantErr: true},
		{name: "slash in provider rejected", raw: "/openusage a/b", wantErr: true},
		{name: "extra args after provider", raw: "/openusage claude codex", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildOpenusageRequest(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got req %+v", req)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.Mode != tt.wantMode {
				t.Errorf("mode = %q, want %q", req.Mode, tt.wantMode)
			}
			if req.Provider != tt.wantProv {
				t.Errorf("provider = %q, want %q", req.Provider, tt.wantProv)
			}
		})
	}
}

func TestCommandArgsStripsTrigger(t *testing.T) {
	got := commandArgs("/openusage provider claude")
	want := []string{"provider", "claude"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
