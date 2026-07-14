package metadata

import "strings"

// tagsKey is the suffix of the konghq.com/tags annotation.
const tagsKey = "/tags"

// ExtractTags extracts the konghq.com/tags annotation as a slice of tags.
// The value is comma-separated; each entry is whitespace-trimmed and empty
// entries are dropped. Returns nil when the annotation is absent or yields no
// non-empty tags. Mirrors ingress-controller/internal/annotations.ExtractUserTags.
func ExtractTags(anns map[string]string) []string {
	val := anns[annotationPrefix+tagsKey]
	if val == "" {
		return nil
	}
	var tags []string
	for _, t := range strings.Split(val, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}
