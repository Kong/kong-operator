package builder

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

// KongTargetBuilder is a builder for configurationv1alpha1.KongTarget resources.
type KongTargetBuilder struct {
	target configurationv1alpha1.KongTarget
	errors []error
}

// NewKongTarget creates and returns a new KongTargetBuilder instance.
func NewKongTarget() *KongTargetBuilder {
	return &KongTargetBuilder{
		target: configurationv1alpha1.KongTarget{},
		errors: make([]error, 0),
	}
}

// WithName sets the name for the KongTarget being built.
func (b *KongTargetBuilder) WithName(name string) *KongTargetBuilder {
	b.target.Name = name
	return b
}

// WithNamespace sets the namespace for the KongTarget being built.
func (b *KongTargetBuilder) WithNamespace(namespace string) *KongTargetBuilder {
	b.target.Namespace = namespace
	return b
}

// WithLabels sets the labels for the KongTarget being built.
func (b *KongTargetBuilder) WithLabels(route *gwtypes.HTTPRoute, parentRef *gwtypes.ParentReference) *KongTargetBuilder {
	labels := metadata.BuildLabels(route, parentRef)
	if b.target.Labels == nil {
		b.target.Labels = make(map[string]string)
	}
	maps.Copy(b.target.Labels, labels)
	return b
}

// WithTarget sets the target address for the KongTarget.
// It combines the host and port into a single target string in the format "host:port".
// The host can be an IP address, hostname, or FQDN, and the port must be a valid port number.
func (b *KongTargetBuilder) WithTarget(host string, port int) *KongTargetBuilder {
	b.target.Spec.Target = net.JoinHostPort(host, strconv.Itoa(port))
	return b
}

// WithWeight sets the weight for the KongTarget, which determines the proportion of traffic
// this target will receive relative to other targets in the same upstream.
// If weight is nil, it defaults to 100. Higher weights receive more traffic.
func (b *KongTargetBuilder) WithWeight(weight *int32) *KongTargetBuilder {
	// Weight is optional, default to 100 if not specified.
	if weight != nil {
		b.target.Spec.Weight = int(*weight)
	} else {
		b.target.Spec.Weight = 100
	}
	return b
}

// WithUpstreamRef sets the upstream reference for the KongTarget.
func (b *KongTargetBuilder) WithUpstreamRef(upstreamRef string) *KongTargetBuilder {
	b.target.Spec.UpstreamRef = commonv1alpha1.NameRef{
		Name: upstreamRef,
	}
	return b
}

// WithAnnotations sets the annotations for the KongTarget being built.
func (b *KongTargetBuilder) WithAnnotations(route *gwtypes.HTTPRoute, parentRef *gwtypes.ParentReference) *KongTargetBuilder {
	annotations := metadata.BuildAnnotations(route, parentRef)
	if b.target.Annotations == nil {
		b.target.Annotations = make(map[string]string)
	}
	maps.Copy(b.target.Annotations, annotations)
	return b
}

// WithOwner sets the owner reference for the KongTarget to the given HTTPRoute.
func (b *KongTargetBuilder) WithOwner(owner *gwtypes.HTTPRoute) *KongTargetBuilder {
	if owner == nil {
		b.errors = append(b.errors, errors.New("owner cannot be nil"))
		return b
	}

	err := controllerutil.SetControllerReference(owner, &b.target, scheme.Get(), controllerutil.WithBlockOwnerDeletion(true))
	if err != nil {
		b.errors = append(b.errors, fmt.Errorf("failed to set owner reference: %w", err))
	}
	return b
}

// Build returns the constructed KongTarget resource and any accumulated errors.
func (b *KongTargetBuilder) Build() (configurationv1alpha1.KongTarget, error) {
	if len(b.errors) > 0 {
		return configurationv1alpha1.KongTarget{}, errors.Join(b.errors...)
	}
	return b.target, nil
}

// MustBuild returns the constructed KongTarget resource, panicking on any errors.
// Useful for tests or when you're certain the build will succeed.
func (b *KongTargetBuilder) MustBuild() configurationv1alpha1.KongTarget {
	target, err := b.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build KongTarget: %w", err))
	}
	return target
}
