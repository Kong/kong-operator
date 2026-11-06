package mocks

import (
	"context"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/sendconfig"
)

// ConfigurationChangeDetector is a mock implementation of sendconfig.ConfigurationChangeDetector.
type ConfigurationChangeDetector struct {
	ConfigurationChanged bool
	TargetConfigEmpty    *bool
}

func (m ConfigurationChangeDetector) HasConfigurationChanged(
	_ context.Context, _, _ []byte, targetConfigEmpty bool, _ sendconfig.StatusClient,
) (bool, error) {
	if m.TargetConfigEmpty != nil {
		*m.TargetConfigEmpty = targetConfigEmpty
	}
	return m.ConfigurationChanged, nil
}
