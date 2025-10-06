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

func TestKongUpstreamBuilder_NewKongUpstream(t *testing.T) {
	builder := NewKongUpstream()

	assert.NotNil(t, builder)
	assert.NotNil(t, builder.errors)
	assert.Empty(t, builder.errors)
	assert.Equal(t, configurationv1alpha1.KongUpstream{}, builder.upstream)
}

func TestKongUpstreamBuilder_WithName(t *testing.T) {
	builder := NewKongUpstream().WithName("test-upstream")

	upstream, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-upstream", upstream.Name)
}

func TestKongUpstreamBuilder_WithNamespace(t *testing.T) {
	builder := NewKongUpstream().WithNamespace("test-namespace")

	upstream, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-namespace", upstream.Namespace)
}

func TestKongUpstreamBuilder_WithLabels(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	builder := NewKongUpstream().WithLabels(httpRoute, parentRef)

	upstream, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, upstream.Labels)
	assert.NotEmpty(t, upstream.Labels)
}

func TestKongUpstreamBuilder_WithAnnotations(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	builder := NewKongUpstream().WithAnnotations(httpRoute, parentRef)

	upstream, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, upstream.Annotations)
}

func TestKongUpstreamBuilder_WithSpecName(t *testing.T) {
	builder := NewKongUpstream().WithSpecName("test-upstream-spec")

	upstream, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-upstream-spec", upstream.Spec.Name)
}

func TestKongUpstreamBuilder_WithControlPlaneRef(t *testing.T) {
	controlPlaneRef := commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name:      "test-konnect-cp",
			Namespace: "test-namespace",
		},
	}

	builder := NewKongUpstream().WithControlPlaneRef(controlPlaneRef)

	upstream, err := builder.Build()
	require.NoError(t, err)

	require.NotNil(t, upstream.Spec.ControlPlaneRef)
	assert.Equal(t, controlPlaneRef, *upstream.Spec.ControlPlaneRef)
}

func TestKongUpstreamBuilder_WithOwner(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-http-route",
			Namespace: "test-namespace",
			UID:       "test-uid",
		},
	}

	t.Run("valid owner", func(t *testing.T) {
		builder := NewKongUpstream().
			WithNamespace("test-namespace").
			WithOwner(httpRoute)

		upstream, err := builder.Build()
		require.NoError(t, err)

		require.Len(t, upstream.OwnerReferences, 1)
		ownerRef := upstream.OwnerReferences[0]
		assert.Equal(t, "test-http-route", ownerRef.Name)
		assert.Equal(t, "test-uid", string(ownerRef.UID))
		assert.True(t, *ownerRef.BlockOwnerDeletion)
	})

	t.Run("nil owner", func(t *testing.T) {
		builder := NewKongUpstream().WithOwner(nil)

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

		builder := NewKongUpstream().WithOwner(httpRouteWithoutTypeMeta)

		_, err := builder.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set owner reference")
	})
}

func TestKongUpstreamBuilder_Build(t *testing.T) {
	t.Run("successful build", func(t *testing.T) {
		builder := NewKongUpstream().
			WithName("test-upstream").
			WithNamespace("test-namespace").
			WithSpecName("test-spec")

		upstream, err := builder.Build()
		require.NoError(t, err)
		assert.Equal(t, "test-upstream", upstream.Name)
		assert.Equal(t, "test-namespace", upstream.Namespace)
		assert.Equal(t, "test-spec", upstream.Spec.Name)
	})

	t.Run("build with errors", func(t *testing.T) {
		builder := NewKongUpstream().WithOwner(nil)

		_, err := builder.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "owner cannot be nil")
	})
}

func TestKongUpstreamBuilder_MustBuild(t *testing.T) {
	t.Run("successful must build", func(t *testing.T) {
		builder := NewKongUpstream().WithName("test-upstream")

		upstream := builder.MustBuild()
		assert.Equal(t, "test-upstream", upstream.Name)
	})

	t.Run("must build with errors panics", func(t *testing.T) {
		builder := NewKongUpstream().WithOwner(nil)

		assert.Panics(t, func() {
			builder.MustBuild()
		})
	})
}

func TestKongUpstreamBuilder_Chaining(t *testing.T) {
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

	upstream := NewKongUpstream().
		WithName("test-upstream").
		WithNamespace("test-namespace").
		WithSpecName("test-spec").
		WithControlPlaneRef(controlPlaneRef).
		WithOwner(httpRoute).
		WithLabels(httpRoute, parentRef).
		WithAnnotations(httpRoute, parentRef).
		MustBuild()

	assert.Equal(t, "test-upstream", upstream.Name)
	assert.Equal(t, "test-namespace", upstream.Namespace)
	assert.Equal(t, "test-spec", upstream.Spec.Name)
	assert.Equal(t, controlPlaneRef, *upstream.Spec.ControlPlaneRef)
	assert.Len(t, upstream.OwnerReferences, 1)
	assert.NotNil(t, upstream.Labels)
	assert.NotNil(t, upstream.Annotations)
}

func TestKongUpstreamBuilder_MultipleErrors(t *testing.T) {
	builder := NewKongUpstream()

	builder.WithOwner(nil)
	builder.errors = append(builder.errors, assert.AnError)

	_, err := builder.Build()
	require.Error(t, err)

	assert.Contains(t, err.Error(), "owner cannot be nil")
	assert.Contains(t, err.Error(), assert.AnError.Error())
}

func TestKongUpstreamBuilder_ErrorAccumulation(t *testing.T) {
	builder := NewKongUpstream().
		WithOwner(nil).
		WithName("test-upstream").
		WithSpecName("test-spec")

	_, err := builder.Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner cannot be nil")

	assert.Equal(t, "test-upstream", builder.upstream.Name)
	assert.Equal(t, "test-spec", builder.upstream.Spec.Name)
}
