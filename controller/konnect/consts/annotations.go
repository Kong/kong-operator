package konnect

const (
	// AnnotationPrefix is the prefix for Kong annotations.
	AnnotationPrefix = "konghq.com"

	// UserTagKey is the key for the user tag annotation.
	UserTagKey = "/tags"

	// AnnotationTags is the key for the tags annotation.
	AnnotationTags = AnnotationPrefix + UserTagKey
)
