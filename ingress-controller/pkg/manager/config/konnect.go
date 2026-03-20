package config

import (
	"time"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/license"
)

type KonnectConfig struct {
	// TODO: https://github.com/Kong/kubernetes-ingress-controller/issues/3922
	// ConfigSynchronizationEnabled is the only toggle we had prior to the addition of the license agent.
	// We likely want to combine these into a single Konnect toggle or piggyback off other Konnect functionality.
	ConfigSynchronizationEnabled bool
	ControlPlaneID               string
	Address                      string
	UploadConfigPeriod           time.Duration
	RefreshNodePeriod            time.Duration
	TLSClient                    TLSClientConfig

	LicenseSynchronizationEnabled bool
	InitialLicensePollingPeriod   time.Duration
	LicensePollingPeriod          time.Duration
	LicenseStorageEnabled         bool
	ConsumersSyncDisabled         bool
}

// DefaultKonnectConfig returns a KonnectConfig with default values for all fields.
func DefaultKonnectConfig() KonnectConfig {
	return KonnectConfig{
		ConfigSynchronizationEnabled:  false,
		LicenseSynchronizationEnabled: false,
		LicenseStorageEnabled:         true,
		InitialLicensePollingPeriod:   license.DefaultInitialPollingPeriod,
		LicensePollingPeriod:          license.DefaultPollingPeriod,
		ControlPlaneID:                "",
		Address:                       "https://us.kic.api.konghq.com",
		TLSClient: TLSClientConfig{
			Cert:     "",
			CertFile: "",
			Key:      "",
			KeyFile:  "",
		},
		UploadConfigPeriod:    DefaultKonnectConfigUploadPeriod,
		RefreshNodePeriod:     DefaultKonnectNodeRefreshPeriod,
		ConsumersSyncDisabled: false,
	}
}
