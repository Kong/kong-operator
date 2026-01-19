package mcprunner

import (
	"context"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DefaultPollingPeriod is the default period for polling the MCP Runner API.
	DefaultPollingPeriod = 30 * time.Second
)

// Client is a client for the MCP Runner API.
type Client struct {
	baseURL        string
	httpClient     *http.Client
	pollingPeriod  time.Duration
	logger         logr.Logger
	onRunnersFound func(ctx context.Context, runners []Runner)
}

// ClientOption is a functional option for configuring the Client.

type ClientOption func(*Client)

// WithPollingPeriod sets the polling period for the client.
func WithPollingPeriod(period time.Duration) ClientOption {
	return func(c *Client) {
		c.pollingPeriod = period
	}
}

// WithHTTPClient sets the HTTP client for the client.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// WithOnRunnersFound sets the callback function that is called when runners are found.
func WithOnRunnersFound(k8sClient client.Client, fn func(ctx context.Context, k8sClient client.Client, runners []Runner)) ClientOption {
	return func(c *Client) {
		c.onRunnersFound = func(ctx context.Context, runners []Runner) {
			fn(ctx, k8sClient, runners)
		}
	}
}

// NewClient creates a new MCP Runner client.
func NewClient(logger logr.Logger, opts ...ClientOption) *Client {

	// mock API server URL
	baseURL := "http://localhost:1234"

	c := &Client{
		baseURL:       baseURL,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		pollingPeriod: DefaultPollingPeriod,
		logger:        logger,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Start starts the polling loop to continuously fetch runners from the API.
func (c *Client) Start(ctx context.Context) error {
	ticker := time.NewTicker(c.pollingPeriod)
	defer ticker.Stop()

	// Poll immediately on start
	if err := c.getRunners(ctx); err != nil {
		c.logger.Error(err, "Failed to poll runners on start")
	}

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Context done: shutting down MCP Runner client")
			return ctx.Err()
		case <-ticker.C:
			if err := c.getRunners(ctx); err != nil {
				c.logger.Error(err, "Failed to poll runners")
			}
		}
	}
}
