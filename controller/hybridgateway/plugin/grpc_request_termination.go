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

// GRPCRequestTerminationForBackendNotFound creates a plugin that terminates requests to a
// backend-less gRPC service, mirroring RequestTerminationForBackendNotFound's role for HTTPRoute.
//
// Kong's own default for a service with an empty upstream is a bare HTTP 503, which Kong (via its
// HTTP_TO_GRPC_STATUS table) does not map to a specific gRPC status for gRPC content types, so a
// gRPC client is left to fall back on its own HTTP-status conversion. We explicitly terminate with
// status_code 500 and content_type application/grpc so Kong frames the response as gRPC: this
// mirrors the "Internal" gRPC status (13) that HTTP 500 conventionally maps to (matching the
// Google API HTTP-to-gRPC status mapping), analogous to HTTPRoute's 500-on-no-backend requirement.
func GRPCRequestTerminationForBackendNotFound(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	grpcRoute *gwtypes.GRPCRoute,
	pRef *gwtypes.ParentReference,
	serviceName string,
) (*configurationv1.KongPlugin, error) {
	pluginName := namegen.NewKongPluginNameForService(serviceName, "request-termination")
	config, err := json.Marshal(map[string]any{
		"status_code":  500,
		"message":      "no existing backendRef provided",
		"content_type": "application/grpc",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request-termination config: %w", err)
	}

	plugin, err := builder.NewKongPlugin().
		WithName(pluginName).
		WithNamespace(metadata.NamespaceFromParentRef(grpcRoute, pRef)).
		WithLabels(grpcRoute, pRef).
		WithAnnotations(grpcRoute, pRef).
		WithTagsAnnotation(grpcRoute).
		WithPluginName("request-termination").
		WithPluginConfig(config).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build request-termination plugin %s: %w", pluginName, err)
	}

	if _, err := translator.VerifyAndUpdate(ctx, logger.WithValues("kongplugin", pluginName), cl, &plugin, grpcRoute, false); err != nil {
		return nil, err
	}

	return &plugin, nil
}
