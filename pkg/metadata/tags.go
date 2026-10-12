package metadata

import (
	"reflect"
	"strings"
)

// ExtractTags extracts a set of tags from a comma-separated string.
// Copy pasted from https://github.com/Kong/kubernetes-ingress-controller/blob/eb80ec2c58f4d53f8c6d7c997bcfb1f334b801e1/internal/annotations/annotations.go#L407-L416
//
//godoclint:disable max-len
func ExtractTags(obj ObjectWithAnnotations) []string {
	if obj == nil {
		return nil
	}
	// If the object is a nil pointer, return nil to avoid panics when calling GetAnnotations().
	if v := reflect.ValueOf(obj); v.Kind() == reflect.Pointer && v.IsNil() {
		return nil
	}

	ann, ok := obj.GetAnnotations()[AnnotationKeyTags]
	if !ok || len(ann) == 0 {
		return nil
	}

	return strings.Split(ann, ",")
}

// ReservedTagPrefixes are the prefixes of the tags the operator attaches to Konnect entities
// to carry the metadata of the source Kubernetes object (e.g. "k8s-uid:<uid>").
// Users must not set tags with these prefixes as the operator relies on them, e.g. to find
// the Konnect entity owned by a Kubernetes object by its UID.
var ReservedTagPrefixes = []string{
	"k8s-generation:",
	"k8s-group:",
	"k8s-kind:",
	"k8s-name:",
	"k8s-namespace:",
	"k8s-uid:",
	"k8s-version:",
	"managed-by:",
}

// IsReservedTag returns true if the given tag starts with one of the ReservedTagPrefixes.
func IsReservedTag(tag string) bool {
	tag = strings.TrimSpace(tag)
	for _, prefix := range ReservedTagPrefixes {
		if strings.HasPrefix(tag, prefix) {
			return true
		}
	}
	return false
}

// ReservedTags returns the tags from the given list which start with one of the ReservedTagPrefixes.
func ReservedTags(tags []string) []string {
	var reserved []string
	for _, tag := range tags {
		if IsReservedTag(tag) {
			reserved = append(reserved, tag)
		}
	}
	return reserved
}

// WithoutReservedTags returns the tags from the given list which do not start with any of the ReservedTagPrefixes.
// It returns nil if no tags are left.
func WithoutReservedTags(tags []string) []string {
	var filtered []string
	for _, tag := range tags {
		if !IsReservedTag(tag) {
			filtered = append(filtered, tag)
		}
	}
	return filtered
}
