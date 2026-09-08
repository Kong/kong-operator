package pluginbinding

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

var grpcRouteTypeMeta = metav1.TypeMeta{
	Kind:       "GRPCRoute",
	APIVersion: "gateway.networking.k8s.io/v1",
}

func TestBindingForGRPCPluginAndRoute(t *testing.T) {
	logger := logr.Discard()
	ctx := context.Background()

	grpcRoute := &gwtypes.GRPCRoute{
		TypeMeta:  grpcRouteTypeMeta,
		Name:      "test-grpcroute",
		Namespace: "test-namespace",
		UID:       "test-uid",
	}
	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}
	cpRef := &commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: "test-cp",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()

	binding, err := BindingForGRPCPluginAndRoute(ctx, logger, fakeClient, grpcRoute, parentRef, cpRef, "test-plugin", "test-route")
	require.NoError(t, err)
	require.NotNil(t, binding)

	assert.Equal(t, "test-namespace", binding.Namespace)
	assert.Equal(t, "test-plugin", binding.Spec.PluginReference.Name)
	require.NotNil(t, binding.Spec.Targets)
	require.NotNil(t, binding.Spec.Targets.RouteReference)
	assert.Equal(t, "test-route", binding.Spec.Targets.RouteReference.Name)
	assert.Equal(t, "configuration.konghq.com", binding.Spec.Targets.RouteReference.Group)
	assert.Equal(t, "KongRoute", binding.Spec.Targets.RouteReference.Kind)
	assert.Equal(t, commonv1alpha1.ControlPlaneRefKonnectNamespacedRef, binding.Spec.ControlPlaneRef.Type)
	assert.Equal(t, "test-cp", binding.Spec.ControlPlaneRef.KonnectNamespacedRef.Name)
	assert.Contains(t, binding.Annotations, consts.GatewayOperatorHybridRoutesGRPCRouteAnnotation)
	assert.Equal(t, "test-namespace/test-grpcroute", binding.Annotations[consts.GatewayOperatorHybridRoutesGRPCRouteAnnotation])
}

func TestBindingForGRPCPluginAndService(t *testing.T) {
	logger := logr.Discard()
	ctx := context.Background()

	grpcRoute := &gwtypes.GRPCRoute{
		TypeMeta:  grpcRouteTypeMeta,
		Name:      "test-grpcroute",
		Namespace: "test-namespace",
		UID:       "test-uid",
	}
	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}
	cpRef := &commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: "test-cp",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()

	binding, err := BindingForGRPCPluginAndService(ctx, logger, fakeClient, grpcRoute, parentRef, cpRef, "test-plugin", "test-service")
	require.NoError(t, err)
	require.NotNil(t, binding)

	assert.Equal(t, "test-namespace", binding.Namespace)
	assert.Equal(t, "test-plugin", binding.Spec.PluginReference.Name)
	require.NotNil(t, binding.Spec.Targets)
	require.NotNil(t, binding.Spec.Targets.ServiceReference)
	assert.Equal(t, "test-service", binding.Spec.Targets.ServiceReference.Name)
	assert.Equal(t, "configuration.konghq.com", binding.Spec.Targets.ServiceReference.Group)
	assert.Equal(t, "KongService", binding.Spec.Targets.ServiceReference.Kind)
	assert.Contains(t, binding.Annotations, consts.GatewayOperatorHybridRoutesGRPCRouteAnnotation)
	assert.Equal(t, "test-namespace/test-grpcroute", binding.Annotations[consts.GatewayOperatorHybridRoutesGRPCRouteAnnotation])
}
