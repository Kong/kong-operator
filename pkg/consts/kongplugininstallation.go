package consts

const (
	// KongPluginInstallationManagedLabelValue indicates that an object's lifecycle is managed by the
	// KongPluginInstallation controller.
	KongPluginInstallationManagedLabelValue = "kong-plugin-installation"

	// AnnotationKongPluginInstallationName is the annotation key used to store the name of the KongPluginInstallation
	// that maps to particular ConfigMap.
	AnnotationKongPluginInstallationMappedKongPluginInstallation = OperatorLabelPrefix + "mapped-to-kong-plugin-installation"
)
