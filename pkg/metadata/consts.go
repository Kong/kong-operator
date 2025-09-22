package metadata

const (
	// annotationPrefix is the prefix for Kong annotations.
	annotationPrefix = "konghq.com"

	// AnnotationKeyTags is the annotation key used to set tags on resources.
	AnnotationKeyTags = annotationPrefix + "/tags"

	// AnnotationKeyPlugins is the annotation key used to attach KongPlugins to resources.
	AnnotationKeyPlugins = annotationPrefix + "/plugins"
)
