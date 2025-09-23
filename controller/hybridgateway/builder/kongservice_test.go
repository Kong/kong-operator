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

func TestKongServiceBuilder_NewKongService(t *testing.T) {
	builder := NewKongService()

	assert.NotNil(t, builder)
	assert.NotNil(t, builder.errors)
	assert.Empty(t, builder.errors)
	assert.Equal(t, configurationv1alpha1.KongService{}, builder.service)
}

func TestKongServiceBuilder_WithName(t *testing.T) {
	builder := NewKongService().WithName("test-service")

	service, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-service", service.Name)
}

func TestKongServiceBuilder_WithNamespace(t *testing.T) {
	builder := NewKongService().WithNamespace("test-namespace")

	service, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-namespace", service.Namespace)
}

func TestKongServiceBuilder_WithLabels(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	builder := NewKongService().WithLabels(httpRoute)

	service, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, service.Labels)
	assert.NotEmpty(t, service.Labels)
}

func TestKongServiceBuilder_WithAnnotations(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	builder := NewKongService().WithAnnotations(httpRoute, parentRef)

	service, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, service.Annotations)
}

func TestKongServiceBuilder_WithSpecName(t *testing.T) {
	tests := []struct {
		name     string
		specName string
		expected *string
	}{
		{
			name:     "with spec name",
			specName: "test-service-spec",
			expected: &[]string{"test-service-spec"}[0],
		},
		{
			name:     "empty spec name",
			specName: "",
			expected: &[]string{""}[0],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewKongService().WithSpecName(tt.specName)

			service, err := builder.Build()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, service.Spec.Name)
		})
	}
}

func TestKongServiceBuilder_WithSpecHost(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		expected string
	}{
		{
			name:     "with host",
			host:     "example.com",
			expected: "example.com",
		},
		{
			name:     "empty host",
			host:     "",
			expected: "",
		},
		{
			name:     "with service name and cluster domain",
			host:     "my-service.my-namespace.svc.cluster.local",
			expected: "my-service.my-namespace.svc.cluster.local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewKongService().WithSpecHost(tt.host)

			service, err := builder.Build()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, service.Spec.Host)
		})
	}
}

func TestKongServiceBuilder_WithControlPlaneRef(t *testing.T) {
	controlPlaneRef := commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name:      "test-konnect-cp",
			Namespace: "test-namespace",
		},
	}

	builder := NewKongService().WithControlPlaneRef(controlPlaneRef)

	service, err := builder.Build()
	require.NoError(t, err)

	require.NotNil(t, service.Spec.ControlPlaneRef)
	assert.Equal(t, controlPlaneRef, *service.Spec.ControlPlaneRef)
}

func TestKongServiceBuilder_WithOwner(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-http-route",
			Namespace: "test-namespace",
			UID:       "test-uid",
		},
	}

	t.Run("valid owner", func(t *testing.T) {
		builder := NewKongService().
			WithNamespace("test-namespace").
			WithOwner(httpRoute)

		service, err := builder.Build()
		require.NoError(t, err)

		require.Len(t, service.OwnerReferences, 1)
		ownerRef := service.OwnerReferences[0]
		assert.Equal(t, "test-http-route", ownerRef.Name)
		assert.Equal(t, "test-uid", string(ownerRef.UID))
		assert.True(t, *ownerRef.BlockOwnerDeletion)
	})

	t.Run("nil owner", func(t *testing.T) {
		builder := NewKongService().WithOwner(nil)

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

		builder := NewKongService().WithOwner(httpRouteWithoutTypeMeta)

		_, err := builder.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set owner reference")
	})
}

func TestKongServiceBuilder_Build(t *testing.T) {
	t.Run("successful build", func(t *testing.T) {
		builder := NewKongService().
			WithName("test-service").
			WithNamespace("test-namespace").
			WithSpecName("test-spec").
			WithSpecHost("example.com")

		service, err := builder.Build()
		require.NoError(t, err)
		assert.Equal(t, "test-service", service.Name)
		assert.Equal(t, "test-namespace", service.Namespace)
		assert.Equal(t, "test-spec", *service.Spec.Name)
		assert.Equal(t, "example.com", service.Spec.Host)
	})

	t.Run("build with errors", func(t *testing.T) {
		builder := NewKongService().WithOwner(nil)

		_, err := builder.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "owner cannot be nil")
	})
}

func TestKongServiceBuilder_MustBuild(t *testing.T) {
	t.Run("successful must build", func(t *testing.T) {
		builder := NewKongService().WithName("test-service")

		service := builder.MustBuild()
		assert.Equal(t, "test-service", service.Name)
	})

	t.Run("must build with errors panics", func(t *testing.T) {
		builder := NewKongService().WithOwner(nil)

		assert.Panics(t, func() {
			builder.MustBuild()
		})
	})
}

func TestKongServiceBuilder_Chaining(t *testing.T) {
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

	controlPlaneRef := commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name:      "test-konnect-cp",
			Namespace: "test-namespace",
		},
	}

	service := NewKongService().
		WithName("test-service").
		WithNamespace("test-namespace").
		WithSpecName("test-spec").
		WithSpecHost("example.com").
		WithControlPlaneRef(controlPlaneRef).
		WithOwner(httpRoute).
		WithLabels(httpRoute).
		WithAnnotations(httpRoute, parentRef).
		MustBuild()

	assert.Equal(t, "test-service", service.Name)
	assert.Equal(t, "test-namespace", service.Namespace)
	assert.Equal(t, "test-spec", *service.Spec.Name)
	assert.Equal(t, "example.com", service.Spec.Host)
	assert.Equal(t, controlPlaneRef, *service.Spec.ControlPlaneRef)
	assert.Len(t, service.OwnerReferences, 1)
	assert.NotNil(t, service.Labels)
	assert.NotNil(t, service.Annotations)
}

func TestKongServiceBuilder_MultipleErrors(t *testing.T) {
	builder := NewKongService()

	builder.WithOwner(nil)
	builder.errors = append(builder.errors, assert.AnError)

	_, err := builder.Build()
	require.Error(t, err)

	assert.Contains(t, err.Error(), "owner cannot be nil")
	assert.Contains(t, err.Error(), assert.AnError.Error())
}
