package builder

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestNewKongPlugin(t *testing.T) {
	builder := NewKongPlugin()

	assert.NotNil(t, builder)
	assert.Empty(t, builder.errors)
	assert.Equal(t, configurationv1.KongPlugin{}, builder.plugin)
}

func TestKongPluginBuilder_WithName(t *testing.T) {
	builder := NewKongPlugin().WithName("test-plugin")

	plugin, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-plugin", plugin.Name)
}

func TestKongPluginBuilder_WithNamespace(t *testing.T) {
	builder := NewKongPlugin().WithNamespace("test-namespace")

	plugin, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-namespace", plugin.Namespace)
}

func TestKongPluginBuilder_WithLabels(t *testing.T) {
	route := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	builder := NewKongPlugin().WithLabels(route, parentRef)

	plugin, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, plugin.Labels)
	assert.NotEmpty(t, plugin.Labels)
}

func TestKongPluginBuilder_WithAnnotations(t *testing.T) {
	route := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}
	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	builder := NewKongPlugin().WithAnnotations(route, parentRef)

	plugin, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, plugin.Annotations)
	assert.NotEmpty(t, plugin.Annotations)

	t.Run("route is nil", func(t *testing.T) {
		parentRef := &gwtypes.ParentReference{Name: "test-gateway"}
		builder := NewKongPlugin().WithAnnotations(nil, parentRef)
		require.NotEmpty(t, builder.errors)
		assert.Contains(t, builder.errors[0].Error(), "route cannot be nil")
	})

	t.Run("parentRef is nil", func(t *testing.T) {
		route := &gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "default",
			},
		}
		builder := NewKongPlugin().WithAnnotations(route, nil)
		require.NotEmpty(t, builder.errors)
		assert.Contains(t, builder.errors[0].Error(), "parentRef cannot be nil")
	})
}

func TestKongPluginBuilder_WithOwner(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-http-route",
			Namespace: "test-namespace",
			UID:       "test-uid",
		},
	}

	t.Run("valid owner", func(t *testing.T) {
		builder := NewKongPlugin().
			WithNamespace("test-namespace").
			WithOwner(httpRoute)

		plugin, err := builder.Build()
		require.NoError(t, err)

		require.Len(t, plugin.OwnerReferences, 1)
		ownerRef := plugin.OwnerReferences[0]
		assert.Equal(t, "HTTPRoute", ownerRef.Kind)
		assert.Equal(t, "gateway.networking.k8s.io/v1", ownerRef.APIVersion)
		assert.Equal(t, "test-http-route", ownerRef.Name)
		assert.Equal(t, "test-uid", string(ownerRef.UID))
		assert.True(t, *ownerRef.BlockOwnerDeletion)
	})

	t.Run("nil owner", func(t *testing.T) {
		builder := NewKongPlugin().WithOwner(nil)

		_, err := builder.Build()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "owner cannot be nil")
	})

	t.Run("owner reference error", func(t *testing.T) {
		builder := NewKongPlugin().
			WithNamespace("wrong-namespace").
			WithOwner(httpRoute)
		_, err := builder.Build()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set owner reference")
	})
}

func TestKongPluginBuilder_MustBuild(t *testing.T) {
	t.Run("successful must build", func(t *testing.T) {
		builder := NewKongPlugin().WithName("test-plugin")

		plugin := builder.MustBuild()
		assert.Equal(t, "test-plugin", plugin.Name)
	})

	t.Run("must build panics on error", func(t *testing.T) {
		builder := NewKongPlugin().WithOwner(nil)

		assert.Panics(t, func() {
			builder.MustBuild()
		})
	})
}

func TestKongPluginBuilder_WithPluginName(t *testing.T) {
	builder := NewKongPlugin().WithPluginName("rate-limiting")

	plugin, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "rate-limiting", plugin.PluginName)
}

func TestKongPluginBuilder_WithPluginConfig(t *testing.T) {
	config := json.RawMessage(`{"limit": 100}`)
	builder := NewKongPlugin().WithPluginConfig(config)

	plugin, err := builder.Build()
	require.NoError(t, err)
	assert.JSONEq(t, string(config), string(plugin.Config.Raw))
}

func TestKongPluginBuilder_ChainedCalls(t *testing.T) {
	route := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	config := json.RawMessage(`{"limit": 100}`)

	plugin := NewKongPlugin().
		WithName("test-plugin").
		WithNamespace("test-ns").
		WithLabels(route, parentRef).
		WithAnnotations(route, parentRef).
		WithPluginName("rate-limiting").
		WithPluginConfig(config).
		MustBuild()

	assert.Equal(t, "test-plugin", plugin.Name)
	assert.Equal(t, "test-ns", plugin.Namespace)
	assert.Equal(t, "rate-limiting", plugin.PluginName)
	assert.NotNil(t, plugin.Labels)
	assert.NotNil(t, plugin.Annotations)
	assert.JSONEq(t, string(config), string(plugin.Config.Raw))
}
