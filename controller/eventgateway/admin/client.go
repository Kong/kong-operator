/*
Copyright 2025 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package admin provides an HTTP client for the on-prem event gateway admin
// API. It is shared by the event gateway controllers.
package admin

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"time"
)

const (
	host             = "localhost" // "gw-admin.default.svc.cluster.local"
	port             = 8082
	eventGatewayPath = "/v1/event-gateways"
	requestTimeout   = 10 * time.Second
)

// ErrEventGatewayNotFound is returned by GetEventGateway when the admin API
// responds with 404.
var ErrEventGatewayNotFound = errors.New("event gateway not found")

// Client is an HTTP client for the on-prem event gateway admin API. TLS
// verification is disabled because the admin endpoint uses a self-signed cert
// in this PoC.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Client that targets host:port over HTTPS.
func New() *Client {
	return &Client{
		baseURL: fmt.Sprintf("https://%s:%d", host, port),
		http: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // PoC: admin endpoint uses self-signed cert.
			},
		},
	}
}

// EventGateway is the admin API representation of an event gateway.
type EventGateway struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// EnsureEventGateway creates the event gateway if it doesn't exist, or patches
// it if name/description drifted.
func (c *Client) EnsureEventGateway(ctx context.Context, desired EventGateway) error {
	current, err := c.GetEventGateway(ctx, desired.ID)
	if err != nil {
		// Fixme: NotFpound produces 500 in gw-admin.
		if err = c.CreateEventGateway(ctx, desired); err != nil {
			return fmt.Errorf("create event gateway: %w", err)
		}
	}

	if current.Name == desired.Name && current.Description == desired.Description {
		return nil
	}
	return c.UpdateEventGateway(ctx, desired)
}

// GetEventGateway returns the event gateway with the given id, or
// ErrEventGatewayNotFound if the admin API returns 404.
func (c *Client) GetEventGateway(ctx context.Context, id string) (EventGateway, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+eventGatewayPath+"/"+id, nil)
	if err != nil {
		return EventGateway{}, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return EventGateway{}, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return EventGateway{}, ErrEventGatewayNotFound
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return EventGateway{}, fmt.Errorf("admin API returned %d: %s", resp.StatusCode, string(b))
	}

	var eg EventGateway
	if err := json.NewDecoder(resp.Body).Decode(&eg); err != nil {
		return EventGateway{}, fmt.Errorf("decode response: %w", err)
	}
	return eg, nil
}

// CreateEventGateway POSTs an event gateway to the admin API.
func (c *Client) CreateEventGateway(ctx context.Context, body EventGateway) error {
	return c.doJSON(ctx, http.MethodPost, c.baseURL+eventGatewayPath, body)
}

// UpdateEventGateway PATCHes name/description on an existing event gateway.
func (c *Client) UpdateEventGateway(ctx context.Context, body EventGateway) error {
	return c.doJSON(ctx, http.MethodPatch, c.baseURL+eventGatewayPath+"/"+body.ID, body)
}

// PushSnapshot POSTs the given snapshot payload to the entities endpoint of
// the event gateway identified by gatewayID. Empty/unset fields are pruned
// before sending so the admin API does not reject zero-valued optional
// fields (e.g. null booleans, empty oneOf objects).
func (c *Client) PushSnapshot(ctx context.Context, gatewayID string, snapshot any) error {
	pruned, err := pruneEmptyFields(snapshot)
	if err != nil {
		return fmt.Errorf("prune snapshot: %w", err)
	}
	return c.doJSON(ctx, http.MethodPost, c.baseURL+eventGatewayPath+"/"+gatewayID+"/entities", pruned)
}

// pruneEmptyFields marshals v to JSON, decodes it generically, recursively
// removes nil values, empty strings, empty maps, and empty slices, and
// converts the Kubernetes enum strings "Enabled" / "Disabled" to native
// booleans (true / false) — the admin API uses bools where the CRD uses the
// string enum. Booleans (including false) and numbers (including 0) are
// preserved.
func pruneEmptyFields(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return prune(generic), nil
}

func prune(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			cleaned := prune(val)
			if isJSONEmpty(cleaned) {
				delete(x, k)
				continue
			}
			x[k] = cleaned
		}
		flattenUnion(x)
		return x
	case []any:
		out := make([]any, 0, len(x))
		for _, val := range x {
			cleaned := prune(val)
			if isJSONEmpty(cleaned) {
				continue
			}
			out = append(out, cleaned)
		}
		return out
	case string:
		switch x {
		case "Enabled":
			return true
		case "Disabled":
			return false
		}
		return x
	default:
		return v
	}
}

// flattenUnion converts the CRD union shape {type: X, X: {...fields}} into the
// admin API's flat shape {type: X, ...fields}. It's a no-op when the map does
// not have a "type" string key whose value also names a sibling map key.
func flattenUnion(m map[string]any) {
	typeStr, ok := m["type"].(string)
	if !ok || typeStr == "" {
		return
	}
	inner, ok := m[typeStr].(map[string]any)
	if !ok {
		return
	}
	delete(m, typeStr)
	maps.Copy(m, inner)
}

func isJSONEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case map[string]any:
		return len(x) == 0
	case []any:
		return len(x) == 0
	}
	return false
}

func (c *Client) doJSON(ctx context.Context, method, url string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("admin API returned %d: %s (request body: %s)", resp.StatusCode, string(b), string(payload))
	}
	return nil
}
