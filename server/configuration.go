package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	baseURLEnv     = "OPENUSAGE_BASE_URL"
	defaultBaseURL = "http://127.0.0.1:6736"
)

// configuration mirrors plugin.json's settings_schema. It currently carries no
// tunable settings (the base URL comes from the OPENUSAGE_BASE_URL environment
// variable), but the loader is kept so OnConfigurationChange stays a no-op-safe
// hook if settings are added later.
type configuration struct{}

func (p *Plugin) loadPluginConfiguration() (configuration, error) {
	var cfg configuration
	if err := p.API.LoadPluginConfiguration(&cfg); err != nil {
		return configuration{}, fmt.Errorf("LoadPluginConfiguration: %w", err)
	}
	return cfg, nil
}

// resolveBaseURL reads OPENUSAGE_BASE_URL from the Mattermost server process
// environment. OpenUsage binds 127.0.0.1:6736 on the host Mac and that bind
// address is not configurable, so for a remote Mattermost server this must
// point at a netbird-reachable forwarder that proxies to the Mac's loopback
// (e.g. http://<mac-netbird-ip>:6736). Defaults to the local loopback address
// for same-host/dev installs.
func resolveBaseURL() (string, error) {
	raw := strings.TrimSpace(os.Getenv(baseURLEnv))
	if raw == "" {
		raw = defaultBaseURL
	}
	raw = strings.TrimRight(raw, "/")
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s is not a valid URL %q: %w", baseURLEnv, raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%s must be an http(s) URL, got %q", baseURLEnv, raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%s must include a host, got %q", baseURLEnv, raw)
	}
	return raw, nil
}
