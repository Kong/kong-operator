package builder

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
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
func (b *KongTargetBuilder) WithLabels(route *gwtypes.HTTPRoute) *KongTargetBuilder {
	labels := metadata.BuildLabels(route)
	if b.target.Labels == nil {
		b.target.Labels = make(map[string]string)
	}
	maps.Copy(b.target.Labels, labels)
	return b
}

// WithBackendRef sets the target specification based on the given HTTPRoute and backend reference.
func (b *KongTargetBuilder) WithBackendRef(httpRoute *gwtypes.HTTPRoute, bRef *gwtypes.HTTPBackendRef) *KongTargetBuilder {
	// Build the dns name of the service for the backendRef.
	// TODO(alacuku): We need to handle the cluster domain properly for the cluster where we are running.
	var namespace string
	if bRef.Namespace == nil || *bRef.Namespace == "" {
		namespace = httpRoute.Namespace
	} else {
		namespace = string(*bRef.Namespace)
	}

	host := string(bRef.Name) + "." + namespace + ".svc.cluster.local"
	port := strconv.Itoa(int(*bRef.Port))
	target := net.JoinHostPort(host, port)
	b.target.Spec.Target = target

	// Weight is optional, default to 100 if not specified
	if bRef.Weight != nil {
		b.target.Spec.Weight = int(*bRef.Weight)
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

	err := controllerutil.SetOwnerReference(owner, &b.target, scheme.Get(), controllerutil.WithBlockOwnerDeletion(true))
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
