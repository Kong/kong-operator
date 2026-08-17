package builder

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	pkgmetadata "github.com/kong/kong-operator/v2/pkg/metadata"
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
func (b *KongPluginBuilder) WithLabels(route client.Object, parentRef *gwtypes.ParentReference) *KongPluginBuilder {
	labels := metadata.BuildLabels(route, parentRef)
	if b.plugin.Labels == nil {
		b.plugin.Labels = make(map[string]string)
	}
	maps.Copy(b.plugin.Labels, labels)
	return b
}

// WithAnnotations sets the annotations for the KongPlugin resource based on the given HTTPRoute and parent reference.
func (b *KongPluginBuilder) WithAnnotations(route client.Object, parentRef *gwtypes.ParentReference) *KongPluginBuilder {
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

// WithTagsAnnotation merges the konghq.com/tags annotation value from the given
// sources into the KongPlugin being built. This ensures that when the generated
// KongPlugin copy is later read by the Konnect ops layer (via metadata.ExtractTags),
// the user-supplied tags are present. Multiple sources are merged and deduplicated.
func (b *KongPluginBuilder) WithTagsAnnotation(sources ...pkgmetadata.ObjectWithAnnotations) *KongPluginBuilder {
	var allTags []string
	for _, src := range sources {
		if src == nil {
			continue
		}
		allTags = append(allTags, pkgmetadata.ExtractTags(src)...)
	}
	if len(allTags) == 0 {
		return b
	}
	// Deduplicate while preserving order.
	seen := make(map[string]struct{}, len(allTags))
	deduped := make([]string, 0, len(allTags))
	for _, t := range allTags {
		// Trim trailing and leading whitespace from each tag to avoid duplicates that differ only by whitespace.
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			deduped = append(deduped, t)
		}
	}
	if len(deduped) == 0 {
		return b
	}

	if b.plugin.Annotations == nil {
		b.plugin.Annotations = make(map[string]string)
	}
	sort.Strings(deduped)
	b.plugin.Annotations[pkgmetadata.AnnotationKeyTags] = strings.Join(deduped, ",")
	return b
}

// WithPluginName sets the plugin name for the KongPlugin being built.
func (b *KongPluginBuilder) WithPluginName(name string) *KongPluginBuilder {
	b.plugin.PluginName = name
	return b
}

// WithPluginConfig sets the plugin config for the KongPlugin being built.
func (b *KongPluginBuilder) WithPluginConfig(config json.RawMessage) *KongPluginBuilder {
	b.plugin.Config.Raw = config
	return b
}
