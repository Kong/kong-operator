package mocks

import (
	"context"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/sendconfig"
)

// ConfigurationChangeDetector is a mock implementation of sendconfig.ConfigurationChangeDetector.
type ConfigurationChangeDetector struct {
	ConfigurationChanged    bool
	TargetConfigEmpty       *bool
	LastDBLessConfigHash    *string
	CompareDBLessConfigHash *bool
}

func (m ConfigurationChangeDetector) HasConfigurationChanged(
	_ context.Context,
	_, _ []byte,
	targetConfigEmpty bool,
	lastDBLessConfigHash string,
	compareDBLessConfigHash bool,
	_ sendconfig.StatusClient,
) (bool, error) {
	if m.TargetConfigEmpty != nil {
		*m.TargetConfigEmpty = targetConfigEmpty
	}
	if m.LastDBLessConfigHash != nil {
		*m.LastDBLessConfigHash = lastDBLessConfigHash
	}
	if m.CompareDBLessConfigHash != nil {
		*m.CompareDBLessConfigHash = compareDBLessConfigHash
	}
	return m.ConfigurationChanged, nil
}
