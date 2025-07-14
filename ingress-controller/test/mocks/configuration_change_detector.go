package mocks

import (
	"context"

	"github.com/kong/go-database-reconciler/pkg/file"

	"github.com/kong/kong-operator/ingress-controller/internal/dataplane/sendconfig"
)

// ConfigurationChangeDetector is a mock implementation of sendconfig.ConfigurationChangeDetector.
type ConfigurationChangeDetector struct {
	ConfigurationChanged bool
}

func (m ConfigurationChangeDetector) HasConfigurationChanged(
	context.Context, []byte, []byte, *file.Content, sendconfig.StatusClient,
) (bool, error) {
	return m.ConfigurationChanged, nil
}
