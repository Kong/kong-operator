package push

import (
	"bytes"
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/kong/go-kong/kong"
)

const (
	// WellKnownInitialHash is the hash of an empty configuration.
	WellKnownInitialHash = "00000000000000000000000000000000"
)

// ConfigurationChangeDetector tells whether a configuration push is necessary.
type ConfigurationChangeDetector interface {
	HasConfigurationChanged(
		ctx context.Context,
		oldSHA, newSHA []byte,
		targetConfigEmpty bool,
		lastDBLessConfigHash string,
		compareDBLessConfigHash bool,
		statusClient StatusClient,
	) (bool, error)
}

// StatusClient reads the Kong status endpoint.
type StatusClient interface {
	Status(context.Context) (*kong.Status, error)
}

// KongGatewayConfigurationChangeDetector detects changes in Kong Gateway configuration. Besides comparing SHA hashes,
// it also checks if the Kong Gateway has no configuration yet or if the configuration to be pushed is empty.
type KongGatewayConfigurationChangeDetector struct {
	logger logr.Logger
}

// NewKongGatewayConfigurationChangeDetector creates a Kong Gateway configuration change detector.
func NewKongGatewayConfigurationChangeDetector(logger logr.Logger) *KongGatewayConfigurationChangeDetector {
	return &KongGatewayConfigurationChangeDetector{logger: logger}
}

// HasConfigurationChanged verifies whether configuration has changed by comparing
// old and new config's SHAs. In case the SHAs are equal, it still can return true if a client is considered crashed or
// just booted up based on its status, or if the hash reported by Kong differs from the hash observed after the last
// successful push. In case the status indicates an empty config and the desired config is also empty this will return
// false to prevent continuously sending empty configuration to Gateway.
func (d *KongGatewayConfigurationChangeDetector) HasConfigurationChanged(
	ctx context.Context,
	oldSHA, newSHA []byte,
	targetConfigEmpty bool,
	lastDBLessConfigHash string,
	compareDBLessConfigHash bool,
	statusClient StatusClient,
) (bool, error) {
	if !bytes.Equal(oldSHA, newSHA) {
		return true, nil
	}

	status, err := statusClient.Status(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to verify kong readiness: %w", err)
	}

	// Kong instance has no configuration, we should push despite the oldSHA and newSHA being equal...
	if status.ConfigurationHash == WellKnownInitialHash {
		// ... unless we're trying to push an empty config in such case skip.
		if targetConfigEmpty {
			return false, nil
		}

		return true, nil
	}

	// A non-empty hash that differs from the hash observed after the last
	// successful push means the DataPlane configuration changed outside the
	// operator (for example a manual edit). An empty reported hash means Kong did
	// not expose it, so keep the previous behavior to avoid a push loop.
	if compareDBLessConfigHash && status.ConfigurationHash != "" &&
		status.ConfigurationHash != lastDBLessConfigHash {
		d.logger.V(1).Info("DataPlane configuration hash differs from the last successfully pushed configuration, pushing",
			"lastPushed", lastDBLessConfigHash,
			"reported", status.ConfigurationHash,
		)
		return true, nil
	}

	return false, nil
}

// KonnectConfigurationChangeDetector detects changes in Konnect configuration by comparing SHA hashes only.
type KonnectConfigurationChangeDetector struct{}

// NewKonnectConfigurationChangeDetector creates a Konnect configuration change detector.
func NewKonnectConfigurationChangeDetector() *KonnectConfigurationChangeDetector {
	return &KonnectConfigurationChangeDetector{}
}

// HasConfigurationChanged verifies whether configuration has changed by comparing old and new config's SHAs.
func (d *KonnectConfigurationChangeDetector) HasConfigurationChanged(
	_ context.Context,
	oldSHA, newSHA []byte,
	_ bool,
	_ string,
	_ bool,
	_ StatusClient,
) (bool, error) {
	return !bytes.Equal(oldSHA, newSHA), nil
}
