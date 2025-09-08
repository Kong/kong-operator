package metadata

// ObjectWithAnnotations is an interface that provides a method to get annotations.
type ObjectWithAnnotations interface {
	GetAnnotations() map[string]string
}

// ObjectWithAnnotationsAndNamespace is an interface that provides a method to get annotations and namespace.
type ObjectWithAnnotationsAndNamespace interface {
	ObjectWithAnnotations
	GetNamespace() string
}
