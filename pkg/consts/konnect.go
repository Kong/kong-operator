package consts

// -----------------------------------------------------------------------------
// Consts - KonnectExtension Generic Parameters
// -----------------------------------------------------------------------------

const (
	// KonnectExtensionPrefix is used as a name prefix to generate KonnectExtension-owned objects' name
	KonnectExtensionPrefix = "konnect-extension"
)

// -----------------------------------------------------------------------------
// Consts - Finalizers
// -----------------------------------------------------------------------------

const (
	// CleanupPluginBindingFinalizer is the finalizer that is attached to entities that
	// are referenced as targets by managed KongPluginBindings (binding instances created
	// by the controller out of entities' konghq.com/plugins annotations).
	// This finalizer is used by the controller to be sure that whenever an entity is deleted,
	// all the targeting managed KongPluginBindings are deleted as well.
	CleanupPluginBindingFinalizer = "gateway.konghq.com/cleanup-plugin-binding"
	// PluginInUseFinalizer is the finalizer attached to KongPlugin resources that are
	// properly referenced by KongPluginBindings.
	// It avoids that KongPlugins get deleted when KongPluginBindings are still referencing them.
	PluginInUseFinalizer = "gateway.konghq.com/plugin-in-use"
	// KonnectExtensionSecretInUseFinalizer is the finalizer added to the secret
	// referenced by KonnectExtension to ensure that the secret is not deleted
	// when in use by an active KonnectExtension.
	KonnectExtensionSecretInUseFinalizer = "gateway.konghq.com/secret-in-use"
)

// -----------------------------------------------------------------------------
// Consts - Labels
// -----------------------------------------------------------------------------

const (
	// SecretProvisioningLabelKey is the label key used to store the provisioning method
	// of the secret resource.
	SecretProvisioningLabelKey = "gateway.konghq.com/secret-provisioning"
	// SecretProvisioningAutomaticLabelValue indicates that the secret resource is
	// automatically provisioned by the controller.
	SecretProvisioningAutomaticLabelValue = "automatic"
	// KonnectExtensionManagedByLabelValue indicates that an object's lifecycle is managed
	// by the KonnectExtension controller.
	KonnectExtensionManagedByLabelValue = "konnect-extension"
)

// -----------------------------------------------------------------------------
// Consts - Annotations
// -----------------------------------------------------------------------------

const (
	// DataPlaneCertificateIDAnnotationKey is the label key used to store the certificate IDs
	// associated with the secret resource. Since multiple Konnect Certificates can be
	// created out of a single secret, this label is used to store the certificate ID
	// of all the certificates created out of the secret, separated by commas.
	// Example: konnect.konghq.com/certificate-ids: "xxxxxx,yyyyyy,zzzzzz"
	DataPlaneCertificateIDAnnotationKey = "konnect.konghq.com/certificate-ids"
)

// -----------------------------------------------------------------------------
// Consts - KongDataPlaneClientCertificate labels
// -----------------------------------------------------------------------------

const (
	// SecretPrefix is used as a name prefix to generate secret-owned objects' name.
	SecretPrefix = "secret"

	// SecretManagedLabelValue indicates that an object's lifecycle is managed
	// by the secret controller.
	SecretManagedLabelValue = "secret"
)
