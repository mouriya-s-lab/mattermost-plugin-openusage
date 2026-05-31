package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const maxResponseBytes = 1 << 20 // 1 MiB ceiling on a usage response body.

// usageClient is a thin read-only HTTP client for the OpenUsage local API.
type usageClient struct {
	baseURL string
	http    *http.Client
}

func newUsageClient(baseURL string, httpClient *http.Client) *usageClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &usageClient{baseURL: baseURL, http: httpClient}
}

// errProviderNotCached signals an HTTP 204 from GET /v1/usage/:providerId: the
// provider is known to OpenUsage but has no successful probe snapshot yet.
var errProviderNotCached = errors.New("provider has no cached usage snapshot yet")

// errProviderUnknown signals an HTTP 404 provider_not_found.
var errProviderUnknown = errors.New("provider is unknown to OpenUsage")

// errServerBusy signals an HTTP 503 server_busy.
var errServerBusy = errors.New("OpenUsage is busy; retry shortly")

// fetchAll returns the snapshots for all enabled providers (GET /v1/usage).
func (c *usageClient) fetchAll(ctx context.Context) ([]providerSnapshot, error) {
	body, status, err := c.get(ctx, "/v1/usage")
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
		return parseSnapshots(body)
	case http.StatusServiceUnavailable:
		return nil, errServerBusy
	default:
		return nil, unexpectedStatus(status, body)
	}
}

// fetchProvider returns one provider snapshot (GET /v1/usage/:providerId).
func (c *usageClient) fetchProvider(ctx context.Context, providerID string) (providerSnapshot, error) {
	body, status, err := c.get(ctx, "/v1/usage/"+providerID)
	if err != nil {
		return providerSnapshot{}, err
	}
	switch status {
	case http.StatusOK:
		return parseSnapshot(body)
	case http.StatusNoContent:
		return providerSnapshot{}, errProviderNotCached
	case http.StatusNotFound:
		return providerSnapshot{}, errProviderUnknown
	case http.StatusServiceUnavailable:
		return providerSnapshot{}, errServerBusy
	default:
		return providerSnapshot{}, unexpectedStatus(status, body)
	}
}

func (c *usageClient) get(ctx context.Context, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build request %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("GET %s: %w", c.baseURL+path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read body %s: %w", path, err)
	}
	return body, resp.StatusCode, nil
}

func unexpectedStatus(status int, body []byte) error {
	trimmed := string(body)
	if len(trimmed) > 200 {
		trimmed = trimmed[:200] + "..."
	}
	if trimmed == "" {
		return fmt.Errorf("OpenUsage returned HTTP %d", status)
	}
	return fmt.Errorf("OpenUsage returned HTTP %d: %s", status, trimmed)
}
