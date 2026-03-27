package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/translator"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// RequestTerminationForBackendNotFound creates a plugin that converts Kong's default 503
// for backend-less services into the Gateway API conformance-required 500 response.
func RequestTerminationForBackendNotFound(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	httpRoute *gwtypes.HTTPRoute,
	pRef *gwtypes.ParentReference,
	serviceName string,
) (*configurationv1.KongPlugin, error) {
	pluginName := namegen.NewKongPluginNameForService(serviceName, "request-termination")
	config, err := json.Marshal(map[string]any{
		"status_code": 500,
		"message":     "no existing backendRef provided",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request-termination config: %w", err)
	}

	plugin, err := builder.NewKongPlugin().
		WithName(pluginName).
		WithNamespace(metadata.NamespaceFromParentRef(httpRoute, pRef)).
		WithLabels(httpRoute, pRef).
		WithAnnotations(httpRoute, pRef).
		WithPluginName("request-termination").
		WithPluginConfig(config).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build request-termination plugin %s: %w", pluginName, err)
	}

	if _, err := translator.VerifyAndUpdate(ctx, logger.WithValues("kongplugin", pluginName), cl, &plugin, httpRoute, false); err != nil {
		return nil, err
	}

	return &plugin, nil
}
