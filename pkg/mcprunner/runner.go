package mcprunner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// getRunners fetches the list of runners from the API endpoint.
func (c *Client) getRunners(ctx context.Context) error {
	url := fmt.Sprintf("%s/runners", c.baseURL)
	c.logger.V(1).Info("Polling MCP Runners", "url", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var listResp ListRunnersResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	c.logger.Info("Successfully fetched MCP Runners", "count", len(listResp))

	// Call the callback if set
	if c.onRunnersFound != nil {
		c.onRunnersFound(ctx, listResp)
	}

	return nil
}

// GetRunner fetches a single runner by ID from the API endpoint.
func (c *Client) GetRunner(ctx context.Context, id string) (*Runner, error) {
	url := fmt.Sprintf("%s/runners/%s", c.baseURL, id)
	c.logger.V(1).Info("Fetching MCP Runner", "url", url, "id", id)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var runner Runner
	if err := json.NewDecoder(resp.Body).Decode(&runner); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	c.logger.Info("Successfully fetched MCP Runner", "id", id)

	return &runner, nil
}
