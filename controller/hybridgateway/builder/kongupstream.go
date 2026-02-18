package builder

import (
	"errors"
	"fmt"
	"maps"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// KongUpstreamBuilder is a builder for configurationv1alpha1.KongUpstream resources.
type KongUpstreamBuilder struct {
	upstream configurationv1alpha1.KongUpstream
	errors   []error
}

// NewKongUpstream returns a new KongUpstreamBuilder.
func NewKongUpstream() *KongUpstreamBuilder {
	return &KongUpstreamBuilder{
		upstream: configurationv1alpha1.KongUpstream{},
		errors:   []error{},
	}
}

// WithName sets the name for the KongUpstream being built.
func (b *KongUpstreamBuilder) WithName(name string) *KongUpstreamBuilder {
	b.upstream.Name = name
	return b
}

// WithNamespace sets the namespace for the KongUpstream being built.
func (b *KongUpstreamBuilder) WithNamespace(namespace string) *KongUpstreamBuilder {
	b.upstream.Namespace = namespace
	return b
}

// WithLabels sets the labels for the KongUpstream being built.
func (b *KongUpstreamBuilder) WithLabels(route *gwtypes.HTTPRoute, parentRef *gwtypes.ParentReference) *KongUpstreamBuilder {
	labels := metadata.BuildLabels(route, parentRef)
	if b.upstream.Labels == nil {
		b.upstream.Labels = make(map[string]string)
	}
	maps.Copy(b.upstream.Labels, labels)
	return b
}

// WithAnnotations sets the annotations for the KongUpstream being built.
func (b *KongUpstreamBuilder) WithAnnotations(route *gwtypes.HTTPRoute, parentRef *gwtypes.ParentReference) *KongUpstreamBuilder {
	annotations := metadata.BuildAnnotations(route, parentRef)
	if b.upstream.Annotations == nil {
		b.upstream.Annotations = make(map[string]string)
	}
	maps.Copy(b.upstream.Annotations, annotations)
	return b
}

// WithSpecName sets the name field in the KongUpstream spec.
func (b *KongUpstreamBuilder) WithSpecName(name string) *KongUpstreamBuilder {
	b.upstream.Spec.Name = name
	return b
}

// WithOwner sets the owner reference for the KongUpstream to the given HTTPRoute.
func (b *KongUpstreamBuilder) WithOwner(owner *gwtypes.HTTPRoute) *KongUpstreamBuilder {
	if owner == nil {
		b.errors = append(b.errors, fmt.Errorf("owner cannot be nil"))
		return b
	}
	if err := controllerutil.SetControllerReference(owner, &b.upstream, scheme.Get(), controllerutil.WithBlockOwnerDeletion(true)); err != nil {
		b.errors = append(b.errors, fmt.Errorf("failed to set owner reference: %w", err))
	}
	return b
}

// WithControlPlaneRef sets the control plane reference for the KongUpstream.
func (b *KongUpstreamBuilder) WithControlPlaneRef(cpr commonv1alpha1.ControlPlaneRef) *KongUpstreamBuilder {
	b.upstream.Spec.ControlPlaneRef = &cpr
	return b
}

// Build returns the constructed KongUpstream resource and any accumulated errors.
func (b *KongUpstreamBuilder) Build() (configurationv1alpha1.KongUpstream, error) {
	if len(b.errors) > 0 {
		return configurationv1alpha1.KongUpstream{}, errors.Join(b.errors...)
	}
	return b.upstream, nil
}

// MustBuild returns the constructed KongUpstream resource, panicking if there are any errors.
func (b *KongUpstreamBuilder) MustBuild() configurationv1alpha1.KongUpstream {
	upstream, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("KongUpstreamBuilder.MustBuild(): %v", err))
	}
	return upstream
}
