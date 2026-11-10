package sendconfig

import (
	"github.com/go-logr/logr"

	dataplanepush "github.com/kong/kong-operator/v2/ingress-controller/pkg/dataplane/push"
)

const (
	// WellKnownInitialHash is the hash of an empty configuration.
	WellKnownInitialHash = dataplanepush.WellKnownInitialHash
)

type (
	// ConfigurationChangeDetector tells whether a configuration push is necessary.
	ConfigurationChangeDetector = dataplanepush.ConfigurationChangeDetector

	// StatusClient reads the Kong status endpoint.
	StatusClient = dataplanepush.StatusClient

	// KongGatewayConfigurationChangeDetector detects changes in Kong Gateway configuration.
	KongGatewayConfigurationChangeDetector = dataplanepush.KongGatewayConfigurationChangeDetector

	// KonnectConfigurationChangeDetector detects changes in Konnect configuration.
	KonnectConfigurationChangeDetector = dataplanepush.KonnectConfigurationChangeDetector
)

// NewKongGatewayConfigurationChangeDetector creates a Kong Gateway configuration change detector.
func NewKongGatewayConfigurationChangeDetector(logger logr.Logger) *KongGatewayConfigurationChangeDetector {
	return dataplanepush.NewKongGatewayConfigurationChangeDetector(logger)
}

// NewKonnectConfigurationChangeDetector creates a Konnect configuration change detector.
func NewKonnectConfigurationChangeDetector() *KonnectConfigurationChangeDetector {
	return dataplanepush.NewKonnectConfigurationChangeDetector()
}
