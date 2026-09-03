package plugin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestGRPCRequestTerminationForBackendNotFound(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	grpcRoute := &gwtypes.GRPCRoute{
		TypeMeta:  grpcRouteTypeMeta,
		Name:      "test-route",
		Namespace: "test-namespace",
		UID:       "test-uid",
	}
	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	fakeClient := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	plugin, err := GRPCRequestTerminationForBackendNotFound(ctx, logger, fakeClient, grpcRoute, parentRef, "test-service")
	require.NoError(t, err)
	require.NotNil(t, plugin)

	assert.Equal(t, "request-termination", plugin.PluginName)
	assert.Equal(t, "test-namespace", plugin.Namespace)
	assert.Equal(t, "test-namespace/test-route", plugin.Annotations[consts.GatewayOperatorHybridRoutesGRPCRouteAnnotation])

	var config map[string]any
	require.NoError(t, json.Unmarshal(plugin.Config.Raw, &config))
	assert.InEpsilon(t, 500.0, config["status_code"], 0.001)
	assert.Equal(t, "application/grpc", config["content_type"])
}
