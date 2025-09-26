package builder

import (
	"errors"
	"fmt"
	"maps"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

// KongServiceBuilder is a builder for configurationv1alpha1.KongService resources.
type KongServiceBuilder struct {
	service configurationv1alpha1.KongService
	errors  []error
}

// NewKongService creates and returns a new KongServiceBuilder instance.
func NewKongService() *KongServiceBuilder {
	return &KongServiceBuilder{
		service: configurationv1alpha1.KongService{},
		errors:  make([]error, 0),
	}
}

// WithName sets the name for the KongService being built.
func (b *KongServiceBuilder) WithName(name string) *KongServiceBuilder {
	b.service.Name = name
	return b
}

// WithNamespace sets the namespace for the KongService being built.
func (b *KongServiceBuilder) WithNamespace(namespace string) *KongServiceBuilder {
	b.service.Namespace = namespace
	return b
}

// WithLabels sets the labels for the KongService being built.
func (b *KongServiceBuilder) WithLabels(route *gwtypes.HTTPRoute) *KongServiceBuilder {
	labels := metadata.BuildLabels(route)
	if b.service.Labels == nil {
		b.service.Labels = make(map[string]string)
	}
	maps.Copy(b.service.Labels, labels)
	return b
}

// WithAnnotations sets the annotations for the KongService being built.
func (b *KongServiceBuilder) WithAnnotations(route *gwtypes.HTTPRoute, parentRef *gwtypes.ParentReference) *KongServiceBuilder {
	annotations := metadata.BuildAnnotations(route, parentRef)
	if b.service.Annotations == nil {
		b.service.Annotations = make(map[string]string)
	}
	maps.Copy(b.service.Annotations, annotations)
	return b
}

// WithSpecName sets the name field in the KongService spec.
func (b *KongServiceBuilder) WithSpecName(name string) *KongServiceBuilder {
	b.service.Spec.Name = &name
	return b
}

// WithSpecHost sets the host field in the KongService spec.
func (b *KongServiceBuilder) WithSpecHost(host string) *KongServiceBuilder {
	b.service.Spec.Host = host
	return b
}

// WithControlPlaneRef sets the ControlPlaneRef for the KongService being built.
func (b *KongServiceBuilder) WithControlPlaneRef(cpr commonv1alpha1.ControlPlaneRef) *KongServiceBuilder {
	b.service.Spec.ControlPlaneRef = &cpr
	return b
}

// WithOwner sets the owner reference for the KongService to the given HTTPRoute.
func (b *KongServiceBuilder) WithOwner(owner *gwtypes.HTTPRoute) *KongServiceBuilder {
	if owner == nil {
		b.errors = append(b.errors, errors.New("owner cannot be nil"))
		return b
	}

	err := controllerutil.SetControllerReference(owner, &b.service, scheme.Get(), controllerutil.WithBlockOwnerDeletion(true))
	if err != nil {
		b.errors = append(b.errors, fmt.Errorf("failed to set owner reference: %w", err))
	}
	return b
}

// Build returns the constructed KongService resource and any accumulated errors.
func (b *KongServiceBuilder) Build() (configurationv1alpha1.KongService, error) {
	if len(b.errors) > 0 {
		return configurationv1alpha1.KongService{}, errors.Join(b.errors...)
	}
	return b.service, nil
}

// MustBuild returns the constructed KongService resource, panicking on any errors.
// Useful for tests or when you're certain the build will succeed.
func (b *KongServiceBuilder) MustBuild() configurationv1alpha1.KongService {
	service, err := b.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build KongService: %w", err))
	}
	return service
}
