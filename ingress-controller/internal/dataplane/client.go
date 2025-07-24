package dataplane

import (
	"context"
	"time"

	dpconf "github.com/kong/kong-operator/ingress-controller/internal/dataplane/config"
)

// -----------------------------------------------------------------------------
// Dataplane Client - Public Vars & Consts
// -----------------------------------------------------------------------------

const (
	// DefaultTimeout indicates the time.Duration allowed for responses to
	// come back from the backend data-plane API.
	//
	// NOTE: the current default is based on observed latency in a CI environment using
	// the GKE cloud provider with the Kong Admin API.
	DefaultTimeout = 30 * time.Second
)

// -----------------------------------------------------------------------------
// Dataplane Client - Public Interface
// -----------------------------------------------------------------------------

type Client interface {
	// DBMode informs the caller which DB mode the data-plane has employed
	// (e.g. "off" (dbless) or "postgres").
	DBMode() dpconf.DBMode

	// Update the data-plane by parsing the current configuring and applying
	// it to the backend API.
	Update(ctx context.Context) error
}
