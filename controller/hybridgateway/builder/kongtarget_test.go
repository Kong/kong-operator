package builder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
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

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	builder := NewKongTarget().WithLabels(httpRoute, parentRef)

	target, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, target.Labels)
	assert.NotEmpty(t, target.Labels)
}

func TestKongTargetBuilder_WithTarget(t *testing.T) {
	tests := []struct {
		name           string
		host           string
		port           int
		expectedTarget string
	}{
		{
			name:           "IPv4 address with port",
			host:           "192.168.1.10",
			port:           8080,
			expectedTarget: "192.168.1.10:8080",
		},
		{
			name:           "IPv6 address with port",
			host:           "2001:db8::1",
			port:           8080,
			expectedTarget: "[2001:db8::1]:8080",
		},
		{
			name:           "hostname with port",
			host:           "example.com",
			port:           443,
			expectedTarget: "example.com:443",
		},
		{
			name:           "service FQDN with port",
			host:           "my-service.default.svc.cluster.local",
			port:           80,
			expectedTarget: "my-service.default.svc.cluster.local:80",
		},
		{
			name:           "localhost with custom port",
			host:           "localhost",
			port:           3000,
			expectedTarget: "localhost:3000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewKongTarget().WithTarget(tt.host, tt.port)

			target, err := builder.Build()
			require.NoError(t, err)
			assert.Equal(t, tt.expectedTarget, target.Spec.Target)
		})
	}
}

func TestKongTargetBuilder_WithWeight(t *testing.T) {
	tests := []struct {
		name           string
		weight         *int32
		expectedWeight int
	}{
		{
			name:           "with weight 50",
			weight:         ptr.To[int32](50),
			expectedWeight: 50,
		},
		{
			name:           "with weight 100",
			weight:         ptr.To[int32](100),
			expectedWeight: 100,
		},
		{
			name:           "with weight 0",
			weight:         ptr.To[int32](0),
			expectedWeight: 0,
		},
		{
			name:           "with weight 1000",
			weight:         ptr.To[int32](1000),
			expectedWeight: 1000,
		},
		{
			name:           "with nil weight (defaults to 100)",
			weight:         nil,
			expectedWeight: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewKongTarget().WithWeight(tt.weight)

			target, err := builder.Build()
			require.NoError(t, err)
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

	target := NewKongTarget().
		WithName("test-target").
		WithNamespace("test-namespace").
		WithTarget("192.168.1.10", 8080).
		WithWeight(ptr.To[int32](50)).
		WithUpstreamRef("test-upstream").
		WithOwner(httpRoute).
		WithLabels(httpRoute, parentRef).
		WithAnnotations(httpRoute, parentRef).
		MustBuild()

	assert.Equal(t, "test-target", target.Name)
	assert.Equal(t, "test-namespace", target.Namespace)
	assert.Equal(t, "192.168.1.10:8080", target.Spec.Target)
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
