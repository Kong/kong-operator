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
