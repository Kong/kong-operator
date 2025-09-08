package metadata

import (
	"strings"
)

// ExtractTags extracts a set of tags from a comma-separated string.
// Copy pasted from: https://github.com/Kong/kubernetes-ingress-controller/blob/eb80ec2c58f4d53f8c6d7c997bcfb1f334b801e1/internal/annotations/annotations.go#L407-L416
func ExtractTags(obj ObjectWithAnnotations) []string {
	ann, ok := obj.GetAnnotations()[AnnotationKeyTags]
	if !ok || len(ann) == 0 {
		return nil
	}

	return strings.Split(ann, ",")
}
