package builder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestNewKongPluginBinding(t *testing.T) {
	builder := NewKongPluginBinding()

	assert.NotNil(t, builder)
	assert.Empty(t, builder.errors)
	assert.Equal(t, configurationv1alpha1.KongPluginBinding{}, builder.binding)
}

func TestKongPluginBindingBuilder_WithName(t *testing.T) {
	builder := NewKongPluginBinding().WithName("test-binding")

	binding, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-binding", binding.Name)
}

func TestKongPluginBindingBuilder_WithNamespace(t *testing.T) {
	builder := NewKongPluginBinding().WithNamespace("test-namespace")

	binding, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-namespace", binding.Namespace)
}

func TestKongPluginBindingBuilder_WithLabels(t *testing.T) {
	route := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	builder := NewKongPluginBinding().WithLabels(route, parentRef)

	binding, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, binding.Labels)
}

func TestKongPluginBindingBuilder_WithAnnotations(t *testing.T) {
	route := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}
	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	builder := NewKongPluginBinding().WithAnnotations(route, parentRef)

	binding, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, binding.Annotations)
}

func TestKongPluginBindingBuilder_WithPluginRef(t *testing.T) {
	builder := NewKongPluginBinding().WithPluginRef("test-plugin")

	binding, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-plugin", binding.Spec.PluginReference.Name)
}

func TestKongPluginBindingBuilder_WithRouteRef(t *testing.T) {
	builder := NewKongPluginBinding().WithRouteRef("test-route")

	binding, err := builder.Build()
	require.NoError(t, err)

	require.NotNil(t, binding.Spec.Targets)
	require.NotNil(t, binding.Spec.Targets.RouteReference)
	assert.Equal(t, "test-route", binding.Spec.Targets.RouteReference.Name)
	assert.Equal(t, "configuration.konghq.com", binding.Spec.Targets.RouteReference.Group)
	assert.Equal(t, "KongRoute", binding.Spec.Targets.RouteReference.Kind)
}

func TestKongPluginBindingBuilder_WithServiceRef(t *testing.T) {
	builder := NewKongPluginBinding().WithServiceRef("test-service")

	binding, err := builder.Build()
	require.NoError(t, err)

	require.NotNil(t, binding.Spec.Targets)
	require.NotNil(t, binding.Spec.Targets.ServiceReference)
	assert.Equal(t, "test-service", binding.Spec.Targets.ServiceReference.Name)
	assert.Equal(t, "configuration.konghq.com", binding.Spec.Targets.ServiceReference.Group)
	assert.Equal(t, "KongService", binding.Spec.Targets.ServiceReference.Kind)
}

func TestKongPluginBindingBuilder_WithControlPlaneRef(t *testing.T) {
	controlPlaneRef := commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: "test-control-plane",
		},
	}

	builder := NewKongPluginBinding().WithControlPlaneRef(controlPlaneRef)

	binding, err := builder.Build()
	require.NoError(t, err)

	assert.Equal(t, controlPlaneRef, binding.Spec.ControlPlaneRef)
	assert.Equal(t, commonv1alpha1.ControlPlaneRefKonnectNamespacedRef, binding.Spec.ControlPlaneRef.Type)
	require.NotNil(t, binding.Spec.ControlPlaneRef.KonnectNamespacedRef)
	assert.Equal(t, "test-control-plane", binding.Spec.ControlPlaneRef.KonnectNamespacedRef.Name)
}

func TestKongPluginBindingBuilder_WithOwner(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-http-route",
			Namespace: "test-namespace",
			UID:       "test-uid",
		},
	}
	t.Run("valid owner", func(t *testing.T) {
		builder := NewKongPluginBinding().
			WithNamespace("test-namespace").
			WithOwner(httpRoute)

		binding, err := builder.Build()
		require.NoError(t, err)

		require.Len(t, binding.OwnerReferences, 1)
		ownerRef := binding.OwnerReferences[0]
		assert.Equal(t, "HTTPRoute", ownerRef.Kind)
		assert.Equal(t, "gateway.networking.k8s.io/v1", ownerRef.APIVersion)
		assert.Equal(t, "test-http-route", ownerRef.Name)
		assert.Equal(t, "test-uid", string(ownerRef.UID))
		assert.True(t, *ownerRef.BlockOwnerDeletion)
	})

	t.Run("nil owner", func(t *testing.T) {
		builder := NewKongPluginBinding().WithOwner(nil)

		_, err := builder.Build()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "owner cannot be nil")
	})
	t.Run("owner reference error", func(t *testing.T) {
		httpRouteWithoutTypeMeta := &gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-http-route",
				Namespace: "test-namespace",
				UID:       "test-uid",
			},
		}

		builder := NewKongPluginBinding().WithOwner(httpRouteWithoutTypeMeta)

		_, err := builder.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set owner reference")
	})
}

func TestKongPluginBindingBuilder_MustBuild(t *testing.T) {
	t.Run("successful build", func(t *testing.T) {
		builder := NewKongPluginBinding().
			WithName("test-binding").
			WithPluginRef("test-plugin").
			WithRouteRef("test-route")

		binding := builder.MustBuild()
		assert.Equal(t, "test-binding", binding.Name)
		assert.Equal(t, "test-plugin", binding.Spec.PluginReference.Name)
		assert.Equal(t, "test-route", binding.Spec.Targets.RouteReference.Name)
	})
	t.Run("build with errors panics", func(t *testing.T) {
		builder := NewKongPluginBinding().WithOwner(nil)

		assert.Panics(t, func() {
			builder.MustBuild()
		})
	})
}

func TestKongPluginBindingBuilder_ChainedCalls(t *testing.T) {
	route := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	controlPlaneRef := commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: "test-control-plane",
		},
	}

	binding := NewKongPluginBinding().
		WithName("chained-binding").
		WithNamespace("test-ns").
		WithLabels(route, parentRef).
		WithAnnotations(route, parentRef).
		WithPluginRef("test-plugin").
		WithRouteRef("test-route").
		WithServiceRef("test-service").
		WithControlPlaneRef(controlPlaneRef).
		MustBuild()

	assert.Equal(t, "chained-binding", binding.Name)
	assert.Equal(t, "test-ns", binding.Namespace)
	assert.Equal(t, "test-plugin", binding.Spec.PluginReference.Name)
	assert.Equal(t, "test-route", binding.Spec.Targets.RouteReference.Name)
	assert.Equal(t, "test-service", binding.Spec.Targets.ServiceReference.Name)
	assert.Equal(t, controlPlaneRef, binding.Spec.ControlPlaneRef)
	assert.NotNil(t, binding.Labels)
	assert.NotNil(t, binding.Annotations)
}

func TestKongPluginBindingBuilder_FullBinding(t *testing.T) {
	route := &gwtypes.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1",
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
			UID:       "test-uid",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	controlPlaneRef := commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: "test-control-plane",
		},
	}

	binding := NewKongPluginBinding().
		WithName("complete-binding").
		WithNamespace("test-namespace").
		WithLabels(route, parentRef).
		WithAnnotations(route, parentRef).
		WithPluginRef("rate-limiting").
		WithRouteRef("api-route").
		WithServiceRef("api-service").
		WithControlPlaneRef(controlPlaneRef).
		WithOwner(route).
		MustBuild()

	// Verify all fields are set correctly
	assert.Equal(t, "complete-binding", binding.Name)
	assert.Equal(t, "test-namespace", binding.Namespace)
	assert.NotNil(t, binding.Labels)
	assert.NotNil(t, binding.Annotations)

	// Verify spec
	assert.Equal(t, "rate-limiting", binding.Spec.PluginReference.Name)
	assert.Equal(t, controlPlaneRef, binding.Spec.ControlPlaneRef)

	// Verify targets
	require.NotNil(t, binding.Spec.Targets)
	require.NotNil(t, binding.Spec.Targets.RouteReference)
	assert.Equal(t, "api-route", binding.Spec.Targets.RouteReference.Name)
	assert.Equal(t, "configuration.konghq.com", binding.Spec.Targets.RouteReference.Group)
	assert.Equal(t, "KongRoute", binding.Spec.Targets.RouteReference.Kind)

	require.NotNil(t, binding.Spec.Targets.ServiceReference)
	assert.Equal(t, "api-service", binding.Spec.Targets.ServiceReference.Name)
	assert.Equal(t, "configuration.konghq.com", binding.Spec.Targets.ServiceReference.Group)
	assert.Equal(t, "KongService", binding.Spec.Targets.ServiceReference.Kind)

	// Verify owner reference
	require.Len(t, binding.OwnerReferences, 1)
	ownerRef := binding.OwnerReferences[0]
	assert.Equal(t, "HTTPRoute", ownerRef.Kind)
	assert.Equal(t, "test-route", ownerRef.Name)
	assert.True(t, *ownerRef.BlockOwnerDeletion)
}
