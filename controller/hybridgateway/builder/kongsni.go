package builder

import (
	"errors"
	"fmt"
	"maps"
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/consts"
)

// KongSNIBuilder is a builder for configurationv1alpha1.KongSNI resources.
type KongSNIBuilder struct {
	sni    configurationv1alpha1.KongSNI
	errors []error
}

// NewKongSNI creates and returns a new KongSNIBuilder instance.
func NewKongSNI() *KongSNIBuilder {
	return &KongSNIBuilder{
		sni:    configurationv1alpha1.KongSNI{},
		errors: make([]error, 0),
	}
}

// WithName sets the name for the KongSNI being built.
func (b *KongSNIBuilder) WithName(name string) *KongSNIBuilder {
	b.sni.Name = name
	return b
}

// WithNamespace sets the namespace for the KongSNI being built.
func (b *KongSNIBuilder) WithNamespace(namespace string) *KongSNIBuilder {
	b.sni.Namespace = namespace
	return b
}

// WithSNIName sets the SNI name (hostname) in the spec.
func (b *KongSNIBuilder) WithSNIName(hostname string) *KongSNIBuilder {
	b.sni.Spec.Name = hostname
	return b
}

// WithCertificateRef sets the certificate reference for the KongSNI.
func (b *KongSNIBuilder) WithCertificateRef(certName string) *KongSNIBuilder {
	b.sni.Spec.CertificateRef = commonv1alpha1.NameRef{
		Name: certName,
	}
	return b
}

// WithLabels sets the labels for the KongSNI being built.
func (b *KongSNIBuilder) WithLabels(gateway *gwtypes.Gateway, listener *gwtypes.Listener) *KongSNIBuilder {
	// Create a ParentReference that points to the Gateway itself
	parentRef := &gwtypes.ParentReference{
		Name: gwtypes.ObjectName(gateway.Name),
	}
	labels := metadata.BuildLabels(gateway, parentRef)
	if b.sni.Labels == nil {
		b.sni.Labels = make(map[string]string)
	}
	maps.Copy(b.sni.Labels, labels)

	// Add listener port as a label for easier identification.
	if listener != nil {
		b.sni.Labels[consts.GatewayOperatorHybridListenerPortLabel] = strconv.Itoa(int(listener.Port))
	}
	return b
}

// WithAnnotations sets the annotations for the KongSNI being built.
func (b *KongSNIBuilder) WithAnnotations(gateway *gwtypes.Gateway) *KongSNIBuilder {
	// Create a ParentReference that points to the Gateway itself
	parentRef := &gwtypes.ParentReference{
		Name: gwtypes.ObjectName(gateway.Name),
	}
	annotations := metadata.BuildAnnotations(gateway, parentRef)
	if b.sni.Annotations == nil {
		b.sni.Annotations = make(map[string]string)
	}
	maps.Copy(b.sni.Annotations, annotations)
	return b
}

// WithOwner sets the owner reference for the KongSNI using controllerutil.SetControllerReference.
func (b *KongSNIBuilder) WithOwner(owner *gwtypes.Gateway) *KongSNIBuilder {
	if owner == nil {
		b.errors = append(b.errors, errors.New("owner cannot be nil"))
		return b
	}

	err := controllerutil.SetControllerReference(owner, &b.sni, scheme.Get(), controllerutil.WithBlockOwnerDeletion(true))
	if err != nil {
		b.errors = append(b.errors, fmt.Errorf("failed to set owner reference: %w", err))
	}
	return b
}

// Build returns the constructed KongSNI resource and any accumulated errors.
func (b *KongSNIBuilder) Build() (configurationv1alpha1.KongSNI, error) {
	if len(b.errors) > 0 {
		return configurationv1alpha1.KongSNI{}, errors.Join(b.errors...)
	}
	return b.sni, nil
}

// MustBuild returns the constructed KongSNI resource, panicking on any errors.
// Useful for tests or when you're certain the build will succeed.
func (b *KongSNIBuilder) MustBuild() configurationv1alpha1.KongSNI {
	sni, err := b.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build KongSNI: %w", err))
	}
	return sni
}
