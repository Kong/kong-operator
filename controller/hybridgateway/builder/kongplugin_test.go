package builder

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
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

func TestKongPluginBuilder_WithTags(t *testing.T) {
	t.Run("sets tags", func(t *testing.T) {
		plugin, err := NewKongPlugin().
			WithName("test-plugin").
			WithTags(commonv1alpha1.Tags{"team-payments", "env-prod"}).
			Build()
		require.NoError(t, err)
		assert.Equal(t, commonv1alpha1.Tags{"team-payments", "env-prod"}, plugin.Tags)
	})

	t.Run("nil tags", func(t *testing.T) {
		plugin, err := NewKongPlugin().
			WithName("test-plugin").
			WithTags(nil).
			Build()
		require.NoError(t, err)
		assert.Empty(t, plugin.Tags)
	})

	t.Run("composes with WithTagsFromAnnotations", func(t *testing.T) {
		sourcePlugin := &configurationv1.KongPlugin{
			ObjectMeta: metav1.ObjectMeta{
				Name: "user-plugin",
				Annotations: map[string]string{
					"konghq.com/tags": "annotation-tag",
				},
			},
			Tags: commonv1alpha1.Tags{"spec-tag"},
		}

		plugin, err := NewKongPlugin().
			WithName("test-plugin").
			WithTagsFromAnnotations(sourcePlugin).
			WithTags(sourcePlugin.Tags).
			Build()
		require.NoError(t, err)
		assert.Equal(t, "annotation-tag", plugin.Annotations["konghq.com/tags"])
		assert.Equal(t, commonv1alpha1.Tags{"spec-tag"}, plugin.Tags)
	})
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

func TestKongPluginBuilder_WithTagsAnnotation(t *testing.T) {
	t.Run("tags from single source", func(t *testing.T) {
		route := &gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "default",
				Annotations: map[string]string{
					"konghq.com/tags": "team-payments,env-prod",
				},
			},
		}

		plugin, err := NewKongPlugin().
			WithName("test-plugin").
			WithTagsFromAnnotations(route).
			Build()
		require.NoError(t, err)
		assert.Equal(t, "env-prod,team-payments", plugin.Annotations["konghq.com/tags"])
	})

	t.Run("tags from multiple sources are merged and deduplicated", func(t *testing.T) {
		route := &gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "default",
				Annotations: map[string]string{
					"konghq.com/tags": "shared-tag,route-tag",
				},
			},
		}
		sourcePlugin := &configurationv1.KongPlugin{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "user-plugin",
				Namespace: "default",
				Annotations: map[string]string{
					"konghq.com/tags": "shared-tag,plugin-tag",
				},
			},
		}

		plugin, err := NewKongPlugin().
			WithName("test-plugin").
			WithTagsFromAnnotations(route, sourcePlugin).
			Build()
		require.NoError(t, err)
		assert.Equal(t, "plugin-tag,route-tag,shared-tag", plugin.Annotations["konghq.com/tags"])
	})

	t.Run("no tags annotation when sources have no tags", func(t *testing.T) {
		route := &gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "default",
			},
		}

		plugin, err := NewKongPlugin().
			WithName("test-plugin").
			WithTagsFromAnnotations(route).
			Build()
		require.NoError(t, err)
		assert.Empty(t, plugin.Annotations["konghq.com/tags"])
	})

	t.Run("nil source is safely skipped", func(t *testing.T) {
		route := &gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "default",
				Annotations: map[string]string{
					"konghq.com/tags": "my-tag",
				},
			},
		}

		plugin, err := NewKongPlugin().
			WithName("test-plugin").
			WithTagsFromAnnotations(route, nil).
			Build()
		require.NoError(t, err)
		assert.Equal(t, "my-tag", plugin.Annotations["konghq.com/tags"])
	})

	t.Run("tags annotation preserved alongside tracking annotations", func(t *testing.T) {
		route := &gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "default",
				Annotations: map[string]string{
					"konghq.com/tags": "my-tag",
				},
			},
		}
		parentRef := &gwtypes.ParentReference{
			Name: "test-gateway",
		}

		plugin, err := NewKongPlugin().
			WithName("test-plugin").
			WithAnnotations(route, parentRef).
			WithTagsFromAnnotations(route).
			Build()
		require.NoError(t, err)
		// Tags annotation should be present
		assert.Equal(t, "my-tag", plugin.Annotations["konghq.com/tags"])
		// Tracking annotations from BuildAnnotations should also be present
		assert.NotEmpty(t, plugin.Annotations["gateway-operator.konghq.com/hybrid-gateways"])
	})

	t.Run("tags with spaces are trimmed", func(t *testing.T) {
		route := &gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "default",
				Annotations: map[string]string{
					"konghq.com/tags": "team-payments , env-prod,env-prod, ,  shared-tag  ",
				},
			},
		}

		plugin, err := NewKongPlugin().
			WithName("test-plugin").
			WithTagsFromAnnotations(route).
			Build()
		require.NoError(t, err)
		assert.Equal(t, "env-prod,shared-tag,team-payments", plugin.Annotations["konghq.com/tags"])
	})
}
