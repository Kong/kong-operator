package consts

const (
	// KongPluginInstallationManagedLabelValue indicates that an object's lifecycle is managed by the
	// KongPluginInstallation controller.
	KongPluginInstallationManagedLabelValue = "kong-plugin-installation"

	// AnnotationMappedToKongPluginInstallation is the annotation key used to store the name of the KongPluginInstallation
	// that maps to particular ConfigMap.
	AnnotationMappedToKongPluginInstallation = OperatorLabelPrefix + "mapped-to-kong-plugin-installation"

	// AnnotationKongPluginInstallationGenerationInternal is the annotation key used to store KongPluginInstallation
	// and its generation, internal usage to re-trigger deployment when KongPluginInstallation changes.
	AnnotationKongPluginInstallationGenerationInternal = OperatorLabelPrefix + "kong-plugin-installation-generation"
)
