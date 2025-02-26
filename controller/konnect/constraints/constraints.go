package constraints

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// SupportedCredentialType is a generic type constraint that all Kong credential
// types must implement.
type SupportedCredentialType interface {
	configurationv1alpha1.KongCredentialBasicAuth |
		configurationv1alpha1.KongCredentialAPIKey |
		configurationv1alpha1.KongCredentialACL |
		configurationv1alpha1.KongCredentialJWT |
		configurationv1alpha1.KongCredentialHMAC

	GetTypeName() string
}

// KongCredential is a generic type constraint that all Kong credential types
// must implement.
type KongCredential[T SupportedCredentialType] interface {
	*T
	client.Object
	GetConditions() []metav1.Condition
	SetConditions([]metav1.Condition)
}

// SupportedKonnectEntityType is an interface that all Konnect entity types
// must implement.
type SupportedKonnectEntityType interface {
	konnectv1alpha1.KonnectGatewayControlPlane |
		konnectv1alpha1.KonnectCloudGatewayNetwork |
		configurationv1alpha1.KongService |
		configurationv1alpha1.KongRoute |
		configurationv1.KongConsumer |
		configurationv1beta1.KongConsumerGroup |
		configurationv1alpha1.KongPluginBinding |
		configurationv1alpha1.KongCredentialBasicAuth |
		configurationv1alpha1.KongCredentialAPIKey |
		configurationv1alpha1.KongCredentialACL |
		configurationv1alpha1.KongCredentialJWT |
		configurationv1alpha1.KongCredentialHMAC |
		configurationv1alpha1.KongUpstream |
		configurationv1alpha1.KongCACertificate |
		configurationv1alpha1.KongCertificate |
		configurationv1alpha1.KongTarget |
		configurationv1alpha1.KongVault |
		configurationv1alpha1.KongKey |
		configurationv1alpha1.KongKeySet |
		configurationv1alpha1.KongSNI |
		configurationv1alpha1.KongDataPlaneClientCertificate
	// TODO: add other types

	GetTypeName() string
}

// EntityTypeObject is an interface that allows non Konnect types to be used
// in the Konnect reconciler and its helper functions.
type EntityTypeObject[T any] interface {
	*T

	// Kubernetes Object methods

	GetObjectMeta() metav1.Object
	client.Object

	// Additional methods

	GetConditions() []metav1.Condition
	SetConditions([]metav1.Condition)
	GetTypeName() string
}

// EntityType is an interface that all Konnect entity types must implement.
// Separating this from constraints.SupportedKonnectEntityType allows us to use EntityType
// where client.Object is required, since it embeds client.Object and uses pointer
// to refer to the constraints.SupportedKonnectEntityType.
type EntityType[T any] interface {
	EntityTypeObject[T]

	// Additional methods which are used in reconciling Konnect entities.

	SetKonnectID(string)
	GetKonnectStatus() *konnectv1alpha1.KonnectEntityStatus
}

// SupportedKonnectEntityPluginReferenceableType is an interface that all Konnect
// entity types that can be referenced by a KonnectPluginBinding must implement.
type SupportedKonnectEntityPluginReferenceableType interface {
	configurationv1alpha1.KongService |
		configurationv1alpha1.KongRoute |
		configurationv1.KongConsumer |
		configurationv1beta1.KongConsumerGroup

	GetTypeName() string
}

// EntityWithKonnectAPIAuthConfigurationRef is an interface that all Konnect entity types
// that reference a KonnectAPIAuthConfiguration must implement.
// More specifically Konnect's ControlPlane does implement that, while all the other
// Konnect entities that are defined within a ControlPlane do not because their
// KonnectAPIAuthConfigurationRef is defined in the referenced ControlPlane.
type EntityWithKonnectAPIAuthConfigurationRef interface {
	GetKonnectAPIAuthConfigurationRef() konnectv1alpha1.KonnectAPIAuthConfigurationRef
}
