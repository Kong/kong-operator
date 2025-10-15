package builder

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

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
func (b *KongPluginBuilder) WithLabels(route *gwtypes.HTTPRoute, parentRef *gwtypes.ParentReference) *KongPluginBuilder {
	labels := metadata.BuildLabels(route, parentRef)
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
	case gatewayv1.HTTPRouteFilterRequestHeaderModifier:
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
		b.plugin.Config.Raw = configJSON
	case gatewayv1.HTTPRouteFilterResponseHeaderModifier:
		rt, err := translateResponseModifier(filter)
		if err != nil {
			b.errors = append(b.errors, err)
			return b
		}

		b.plugin.PluginName = "response-transformer"

		configJSON, err := json.Marshal(rt)
		if err != nil {
			b.errors = append(b.errors, fmt.Errorf("failed to marshal %q plugin config: %w", b.plugin.PluginName, err))
			return b
		}
		b.plugin.Config.Raw = configJSON
	default:
		b.errors = append(b.errors, fmt.Errorf("unsupported filter type: %s", filter.Type))
	}
	return b
}

// internal functions and types for translating HTTPRouteFilter to KongPlugin configurations

type transformerTargetSlice struct {
	Headers []string `json:"headers,omitempty"`
}

type transformerData struct {
	// Add: adds an header only if the header is not existent.
	// Append: adds a new header even if the header is already existent (adds a new instance).
	// Remove: removes an entry.
	// Replace: overwrites an header value only if the header exists.
	Add     transformerTargetSlice `json:"add,omitzero"`
	Append  transformerTargetSlice `json:"append,omitzero"`
	Remove  transformerTargetSlice `json:"remove,omitzero"`
	Replace transformerTargetSlice `json:"replace,omitzero"`
}

func translateRequestModifier(filter gwtypes.HTTPRouteFilter) (transformerData, error) {
	var err error
	plugin := transformerData{}

	if filter.RequestHeaderModifier == nil {
		err = errors.New("RequestHeaderModifier filter config is missing")
		return plugin, err
	}

	// In order to overwrite an header of add if not present (GWAPI Set) we should do a Kong Plugin
	// Replace (so it will overwrite it if found) + Add (so if not found, will add it).
	if len(filter.RequestHeaderModifier.Set) > 0 {
		for _, v := range filter.RequestHeaderModifier.Set {
			plugin.Replace.Headers = append(plugin.Replace.Headers, string(v.Name)+":"+v.Value)
			plugin.Add.Headers = append(plugin.Add.Headers, string(v.Name)+":"+v.Value)
		}
	}
	// Add for GWAPI equals "append" for Kong Plugins (it will add another instance of the header).
	if len(filter.RequestHeaderModifier.Add) > 0 {
		for _, v := range filter.RequestHeaderModifier.Add {
			plugin.Append.Headers = append(plugin.Append.Headers, string(v.Name)+":"+v.Value)
		}
	}
	if len(filter.RequestHeaderModifier.Remove) > 0 {
		plugin.Remove.Headers = append(plugin.Remove.Headers, filter.RequestHeaderModifier.Remove...)
	}

	if len(plugin.Add.Headers)+len(plugin.Append.Headers)+
		len(plugin.Remove.Headers)+len(plugin.Replace.Headers) == 0 {
		err = errors.New("RequestHeaderModifier filter config is empty")
		plugin = transformerData{}
	}
	return plugin, err
}

func translateResponseModifier(filter gwtypes.HTTPRouteFilter) (transformerData, error) {
	var err error
	plugin := transformerData{}

	if filter.ResponseHeaderModifier == nil {
		err = errors.New("ResponseHeaderModifier filter config is missing")
		return plugin, err
	}

	if len(filter.ResponseHeaderModifier.Set) > 0 {
		for _, v := range filter.ResponseHeaderModifier.Set {
			plugin.Replace.Headers = append(plugin.Replace.Headers, string(v.Name)+":"+v.Value)
			plugin.Add.Headers = append(plugin.Add.Headers, string(v.Name)+":"+v.Value)
		}
	}
	if len(filter.ResponseHeaderModifier.Add) > 0 {
		for _, v := range filter.ResponseHeaderModifier.Add {
			plugin.Append.Headers = append(plugin.Append.Headers, string(v.Name)+":"+v.Value)
		}
	}
	if len(filter.ResponseHeaderModifier.Remove) > 0 {
		plugin.Remove.Headers = append(plugin.Remove.Headers, filter.ResponseHeaderModifier.Remove...)
	}

	if len(plugin.Add.Headers)+len(plugin.Append.Headers)+len(plugin.Remove.Headers)+len(plugin.Replace.Headers) == 0 {
		err = errors.New("ResponseHeaderModifier filter config is empty")
		plugin = transformerData{}
	}
	return plugin, err
}
