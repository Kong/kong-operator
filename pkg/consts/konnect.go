package consts

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
	// DataPlaneKonnectExtensionFinalizer is the finalizer added to the secret
	// referenced by KonnectExtension to ensure that the secret is not deleted
	// when in use by an active KonnectExtension.
	SecretKonnectExtensionFinalizer = "gateway.konghq.com/secret-in-use"
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
