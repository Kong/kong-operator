package metadata

const (
	// annotationPrefix is the prefix for Kong annotations.
	annotationPrefix = "konghq.com"

	// AnnotationKeyTags is the annotation key used to set tags on resources.
	AnnotationKeyTags = annotationPrefix + "/tags"

	// AnnotationKeyPlugins is the annotation key used to attach KongPlugins to resources.
	AnnotationKeyPlugins = annotationPrefix + "/plugins"

	// AnnotationKeyKonnectCPLabels is the annotation key used to set Konnect labels on the
	// KonnectGatewayControlPlane created for a Gateway.
	AnnotationKeyKonnectCPLabels = annotationPrefix + "/konnect-cp-labels"

	// AnnotationKeyKonnectDPLabels is the annotation key used to set Konnect labels on the
	// KonnectExtension (and thus the Konnect DataPlane) created for a Gateway.
	AnnotationKeyKonnectDPLabels = annotationPrefix + "/konnect-dp-labels"
)
