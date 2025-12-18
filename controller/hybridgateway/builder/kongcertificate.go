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

// KongCertificateBuilder is a builder for configurationv1alpha1.KongCertificate resources.
type KongCertificateBuilder struct {
	certificate configurationv1alpha1.KongCertificate
	errors      []error
}

// NewKongCertificate creates and returns a new KongCertificateBuilder instance.
func NewKongCertificate() *KongCertificateBuilder {
	secretRefType := configurationv1alpha1.KongCertificateSourceTypeSecretRef
	return &KongCertificateBuilder{
		certificate: configurationv1alpha1.KongCertificate{
			Spec: configurationv1alpha1.KongCertificateSpec{
				Type: &secretRefType,
			},
		},
		errors: make([]error, 0),
	}
}

// WithName sets the name for the KongCertificate being built.
func (b *KongCertificateBuilder) WithName(name string) *KongCertificateBuilder {
	b.certificate.Name = name
	return b
}

// WithNamespace sets the namespace for the KongCertificate being built.
func (b *KongCertificateBuilder) WithNamespace(namespace string) *KongCertificateBuilder {
	b.certificate.Namespace = namespace
	return b
}

// WithSecretRef sets the secret reference for the KongCertificate.
func (b *KongCertificateBuilder) WithSecretRef(name, namespace string) *KongCertificateBuilder {
	b.certificate.Spec.SecretRef = &commonv1alpha1.NamespacedRef{
		Name:      name,
		Namespace: &namespace,
	}
	return b
}

// WithControlPlaneRef sets the ControlPlaneRef for the KongCertificate being built.
func (b *KongCertificateBuilder) WithControlPlaneRef(cpr commonv1alpha1.ControlPlaneRef) *KongCertificateBuilder {
	b.certificate.Spec.ControlPlaneRef = &cpr
	return b
}

// WithLabels sets the labels for the KongCertificate being built.
func (b *KongCertificateBuilder) WithLabels(gateway *gwtypes.Gateway, listener *gwtypes.Listener) *KongCertificateBuilder {
	// Create a ParentReference that points to the Gateway itself
	parentRef := &gwtypes.ParentReference{
		Name: gwtypes.ObjectName(gateway.Name),
	}
	labels := metadata.BuildLabels(gateway, parentRef)
	if b.certificate.Labels == nil {
		b.certificate.Labels = make(map[string]string)
	}
	maps.Copy(b.certificate.Labels, labels)
	if listener != nil {
		// Add listener port as a label for easier identification.
		b.certificate.Labels[consts.GatewayOperatorHybridListenerPortLabel] = strconv.Itoa(int(listener.Port))
	}

	return b
}

// WithAnnotations sets the annotations for the KongCertificate being built.
func (b *KongCertificateBuilder) WithAnnotations(gateway *gwtypes.Gateway) *KongCertificateBuilder {
	// Create a ParentReference that points to the Gateway itself
	parentRef := &gwtypes.ParentReference{
		Name: gwtypes.ObjectName(gateway.Name),
	}
	annotations := metadata.BuildAnnotations(gateway, parentRef)
	if b.certificate.Annotations == nil {
		b.certificate.Annotations = make(map[string]string)
	}
	maps.Copy(b.certificate.Annotations, annotations)
	return b
}

// WithOwner sets the owner reference for the KongCertificate to the given Gateway.
func (b *KongCertificateBuilder) WithOwner(owner *gwtypes.Gateway) *KongCertificateBuilder {
	if owner == nil {
		b.errors = append(b.errors, errors.New("owner cannot be nil"))
		return b
	}

	err := controllerutil.SetControllerReference(owner, &b.certificate, scheme.Get(), controllerutil.WithBlockOwnerDeletion(true))
	if err != nil {
		b.errors = append(b.errors, fmt.Errorf("failed to set owner reference: %w", err))
	}
	return b
}

// Build returns the constructed KongCertificate resource and any accumulated errors.
func (b *KongCertificateBuilder) Build() (configurationv1alpha1.KongCertificate, error) {
	if len(b.errors) > 0 {
		return configurationv1alpha1.KongCertificate{}, errors.Join(b.errors...)
	}
	return b.certificate, nil
}

// MustBuild returns the constructed KongCertificate resource, panicking on any errors.
// Useful for tests or when you're certain the build will succeed.
func (b *KongCertificateBuilder) MustBuild() configurationv1alpha1.KongCertificate {
	cert, err := b.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build KongCertificate: %w", err))
	}
	return cert
}
