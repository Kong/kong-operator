package builder

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	v1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

// KongPluginBuilder is a builder for configurationv1.KongPlugin resources.
type KongPluginBuilder struct {
	plugin configurationv1.KongPlugin
	errors []error
}

// NewKongPlugin creates and returns a new KongPluginBuilder instance.
func NewKongPlugin() *KongPluginBuilder {
	return &KongPluginBuilder{
		plugin: configurationv1.KongPlugin{},
		errors: make([]error, 0),
	}
}

// WithName sets the name for the KongPlugin being built.
func (b *KongPluginBuilder) WithName(name string) *KongPluginBuilder {
	b.plugin.Name = name
	return b
}

// WithNamespace sets the namespace for the KongPlugin being built.
func (b *KongPluginBuilder) WithNamespace(namespace string) *KongPluginBuilder {
	b.plugin.Namespace = namespace
	return b
}

// WithLabels sets the labels for the KongPlugin resource based on the given HTTPRoute.
func (b *KongPluginBuilder) WithLabels(route *gwtypes.HTTPRoute) *KongPluginBuilder {
	labels := metadata.BuildLabels(route)
	if b.plugin.Labels == nil {
		b.plugin.Labels = make(map[string]string)
	}
	maps.Copy(b.plugin.Labels, labels)
	return b
}

// WithAnnotations sets the annotations for the KongPlugin resource based on the given HTTPRoute and parent reference.
func (b *KongPluginBuilder) WithAnnotations(route *gwtypes.HTTPRoute, parentRef *gwtypes.ParentReference) *KongPluginBuilder {
	if route == nil {
		b.errors = append(b.errors, errors.New("route cannot be nil"))
		return b
	}
	if parentRef == nil {
		b.errors = append(b.errors, errors.New("parentRef cannot be nil"))
		return b
	}
	annotations := metadata.BuildAnnotations(route, parentRef)
	if b.plugin.Annotations == nil {
		b.plugin.Annotations = make(map[string]string)
	}
	maps.Copy(b.plugin.Annotations, annotations)
	return b
}

// WithOwner sets the owner reference for the KongPlugin to the given HTTPRoute.
func (b *KongPluginBuilder) WithOwner(owner *gwtypes.HTTPRoute) *KongPluginBuilder {
	if owner == nil {
		b.errors = append(b.errors, errors.New("owner cannot be nil"))
		return b
	}

	err := controllerutil.SetOwnerReference(owner, &b.plugin, scheme.Get(), controllerutil.WithBlockOwnerDeletion(true))
	if err != nil {
		b.errors = append(b.errors, fmt.Errorf("failed to set owner reference: %w", err))
	}
	return b
}

// Build returns the constructed KongPlugin resource and any accumulated errors.
func (b *KongPluginBuilder) Build() (configurationv1.KongPlugin, error) {
	if len(b.errors) > 0 {
		return configurationv1.KongPlugin{}, errors.Join(b.errors...)
	}
	return b.plugin, nil
}

// MustBuild returns the constructed KongPlugin resource, panicking on any errors.
// Useful for tests or when you're certain the build will succeed.
func (b *KongPluginBuilder) MustBuild() configurationv1.KongPlugin {
	plugin, err := b.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build KongPlugin: %w", err))
	}
	return plugin
}

// WithFilter sets the KongPlugin configuration based on the given HTTPRouteFilter.
func (b *KongPluginBuilder) WithFilter(filter gwtypes.HTTPRouteFilter) *KongPluginBuilder {

	switch filter.Type {
	case v1.HTTPRouteFilterRequestHeaderModifier:
		rt, err := translateRequestModifier(filter)
		if err != nil {
			b.errors = append(b.errors, err)
			return b
		}

		b.plugin.PluginName = "request-transformer"

		configJSON, err := json.Marshal(rt)
		if err != nil {
			b.errors = append(b.errors, fmt.Errorf("failed to marshal %q plugin config: %w", b.plugin.PluginName, err))
			return b
		}
		b.plugin.Config.Raw = []byte(configJSON)

	default:
		b.errors = append(b.errors, fmt.Errorf("unsupported filter type: %s", filter.Type))
	}
	return b
}

// internal functions and types for translating HTTPRouteFilter to KongPlugin configurations

type requestTransformerTargetSlice struct {
	Headers []string `json:"headers,omitempty"`
}

type requestTransformer struct {
	Add    requestTransformerTargetSlice `json:"add,omitzero"`
	Remove requestTransformerTargetSlice `json:"remove,omitzero"`
}

func translateRequestModifier(filter gwtypes.HTTPRouteFilter) (plugin requestTransformer, err error) {
	plugin = requestTransformer{}
	err = nil

	if filter.RequestHeaderModifier == nil {
		err = errors.New("RequestHeaderModifier filter config is missing")
		return
	}
	plugin.Remove.Headers = []string{}
	plugin.Add.Headers = []string{}

	if len(filter.RequestHeaderModifier.Set) > 0 {
		for _, v := range filter.RequestHeaderModifier.Set {
			plugin.Remove.Headers = append(plugin.Remove.Headers, string(v.Name))
			plugin.Add.Headers = append(plugin.Add.Headers, string(v.Name)+":"+v.Value)
		}
	}
	if len(filter.RequestHeaderModifier.Add) > 0 {
		for _, v := range filter.RequestHeaderModifier.Add {
			plugin.Add.Headers = append(plugin.Add.Headers, string(v.Name)+":"+v.Value)
		}
	}
	if len(filter.RequestHeaderModifier.Remove) > 0 {
		for _, v := range filter.RequestHeaderModifier.Remove {
			plugin.Remove.Headers = append(plugin.Remove.Headers, string(v))
		}
	}

	if len(plugin.Add.Headers) == 0 && len(plugin.Remove.Headers) == 0 {
		err = errors.New("RequestHeaderModifier filter config is empty")
		plugin = requestTransformer{}
	}
	return
}
