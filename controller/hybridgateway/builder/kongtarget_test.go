package builder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestKongTargetBuilder_NewKongTarget(t *testing.T) {
	builder := NewKongTarget()

	assert.NotNil(t, builder)
	assert.NotNil(t, builder.errors)
	assert.Empty(t, builder.errors)
	assert.Equal(t, configurationv1alpha1.KongTarget{}, builder.target)
}

func TestKongTargetBuilder_WithName(t *testing.T) {
	builder := NewKongTarget().WithName("test-target")

	target, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-target", target.Name)
}

func TestKongTargetBuilder_WithNamespace(t *testing.T) {
	builder := NewKongTarget().WithNamespace("test-namespace")

	target, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-namespace", target.Namespace)
}

func TestKongTargetBuilder_WithLabels(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	builder := NewKongTarget().WithLabels(httpRoute)

	target, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, target.Labels)
	assert.NotEmpty(t, target.Labels)
}

func TestKongTargetBuilder_WithBackendRef(t *testing.T) {
	port := gatewayv1.PortNumber(8080)
	weight := int32(100)

	tests := []struct {
		name           string
		httpRoute      *gwtypes.HTTPRoute
		backendRef     *gwtypes.HTTPBackendRef
		expectedTarget string
		expectedWeight int
	}{
		{
			name: "backend ref with same namespace",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			backendRef: &gwtypes.HTTPBackendRef{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name: "test-service",
						Port: &port,
					},
					Weight: &weight,
				},
			},
			expectedTarget: "test-service.test-namespace.svc.cluster.local:8080",
			expectedWeight: 100,
		},
		{
			name: "backend ref with different namespace",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			backendRef: &gwtypes.HTTPBackendRef{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name:      "test-service",
						Namespace: &[]gatewayv1.Namespace{"other-namespace"}[0],
						Port:      &port,
					},
					Weight: &weight,
				},
			},
			expectedTarget: "test-service.other-namespace.svc.cluster.local:8080",
			expectedWeight: 100,
		},
		{
			name: "backend ref with empty namespace",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			backendRef: &gwtypes.HTTPBackendRef{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name:      "test-service",
						Namespace: &[]gatewayv1.Namespace{""}[0],
						Port:      &port,
					},
					Weight: &weight,
				},
			},
			expectedTarget: "test-service.test-namespace.svc.cluster.local:8080",
			expectedWeight: 100,
		},
		{
			name: "backend ref with nil namespace",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			backendRef: &gwtypes.HTTPBackendRef{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name:      "test-service",
						Namespace: nil,
						Port:      &port,
					},
					Weight: &weight,
				},
			},
			expectedTarget: "test-service.test-namespace.svc.cluster.local:8080",
			expectedWeight: 100,
		},
		{
			name: "backend ref without weight",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			backendRef: &gwtypes.HTTPBackendRef{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name: "test-service",
						Port: &port,
					},
					Weight: nil,
				},
			},
			expectedTarget: "test-service.test-namespace.svc.cluster.local:8080",
			expectedWeight: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewKongTarget().WithBackendRef(tt.httpRoute, tt.backendRef)

			target, err := builder.Build()
			require.NoError(t, err)
			assert.Equal(t, tt.expectedTarget, target.Spec.Target)
			assert.Equal(t, tt.expectedWeight, target.Spec.Weight)
		})
	}
}

func TestKongTargetBuilder_WithUpstreamRef(t *testing.T) {
	builder := NewKongTarget().WithUpstreamRef("test-upstream")

	target, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-upstream", target.Spec.UpstreamRef.Name)
}

func TestKongTargetBuilder_WithAnnotations(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	builder := NewKongTarget().WithAnnotations(httpRoute, parentRef)

	target, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, target.Annotations)
}

func TestKongTargetBuilder_WithOwner(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-http-route",
			Namespace: "test-namespace",
			UID:       "test-uid",
		},
	}

	t.Run("valid owner", func(t *testing.T) {
		builder := NewKongTarget().
			WithNamespace("test-namespace").
			WithOwner(httpRoute)

		target, err := builder.Build()
		require.NoError(t, err)

		require.Len(t, target.OwnerReferences, 1)
		ownerRef := target.OwnerReferences[0]
		assert.Equal(t, "test-http-route", ownerRef.Name)
		assert.Equal(t, "test-uid", string(ownerRef.UID))
		assert.True(t, *ownerRef.BlockOwnerDeletion)
	})

	t.Run("nil owner", func(t *testing.T) {
		builder := NewKongTarget().WithOwner(nil)

		_, err := builder.Build()
		require.Error(t, err)
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

		builder := NewKongTarget().WithOwner(httpRouteWithoutTypeMeta)

		_, err := builder.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set owner reference")
	})
}

func TestKongTargetBuilder_Build(t *testing.T) {
	t.Run("successful build", func(t *testing.T) {
		builder := NewKongTarget().
			WithName("test-target").
			WithNamespace("test-namespace").
			WithUpstreamRef("test-upstream")

		target, err := builder.Build()
		require.NoError(t, err)
		assert.Equal(t, "test-target", target.Name)
		assert.Equal(t, "test-namespace", target.Namespace)
		assert.Equal(t, "test-upstream", target.Spec.UpstreamRef.Name)
	})

	t.Run("build with errors", func(t *testing.T) {
		builder := NewKongTarget().WithOwner(nil)

		_, err := builder.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "owner cannot be nil")
	})
}

func TestKongTargetBuilder_MustBuild(t *testing.T) {
	t.Run("successful must build", func(t *testing.T) {
		builder := NewKongTarget().WithName("test-target")

		target := builder.MustBuild()
		assert.Equal(t, "test-target", target.Name)
	})

	t.Run("must build with errors panics", func(t *testing.T) {
		builder := NewKongTarget().WithOwner(nil)

		assert.Panics(t, func() {
			builder.MustBuild()
		})
	})
}

func TestKongTargetBuilder_Chaining(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-http-route",
			Namespace: "test-namespace",
			UID:       "test-uid",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	port := gatewayv1.PortNumber(8080)
	weight := int32(50)

	backendRef := &gwtypes.HTTPBackendRef{
		BackendRef: gatewayv1.BackendRef{
			BackendObjectReference: gatewayv1.BackendObjectReference{
				Name: "test-service",
				Port: &port,
			},
			Weight: &weight,
		},
	}

	target := NewKongTarget().
		WithName("test-target").
		WithNamespace("test-namespace").
		WithBackendRef(httpRoute, backendRef).
		WithUpstreamRef("test-upstream").
		WithOwner(httpRoute).
		WithLabels(httpRoute).
		WithAnnotations(httpRoute, parentRef).
		MustBuild()

	assert.Equal(t, "test-target", target.Name)
	assert.Equal(t, "test-namespace", target.Namespace)
	assert.Equal(t, "test-service.test-namespace.svc.cluster.local:8080", target.Spec.Target)
	assert.Equal(t, 50, target.Spec.Weight)
	assert.Equal(t, "test-upstream", target.Spec.UpstreamRef.Name)
	assert.Len(t, target.OwnerReferences, 1)
	assert.NotNil(t, target.Labels)
	assert.NotNil(t, target.Annotations)
}

func TestKongTargetBuilder_MultipleErrors(t *testing.T) {
	builder := NewKongTarget()

	builder.WithOwner(nil)
	builder.errors = append(builder.errors, assert.AnError)

	_, err := builder.Build()
	require.Error(t, err)

	assert.Contains(t, err.Error(), "owner cannot be nil")
	assert.Contains(t, err.Error(), assert.AnError.Error())
}
