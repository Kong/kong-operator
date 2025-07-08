package utils

import (
	"slices"

	"github.com/google/go-cmp/cmp"
)

// IgnoreAnnotationKeysComparer returns a cmp.Option that ignores specified annotation keys when comparing.
func IgnoreAnnotationKeysComparer(keys ...string) cmp.Option {
	return cmp.FilterPath(
		func(p cmp.Path) bool {
			return p.String() == "ObjectMeta.Annotations"
		},
		cmp.Comparer(
			func(a, b map[string]string) bool {
				if a == nil && b == nil {
					return true
				}
				if a == nil || b == nil {
					return false
				}

				for k, v := range a {
					// Skip ignored keys
					if containsString(keys, k) {
						continue
					}
					if b[k] != v {
						return false
					}
				}

				for k, v := range b {
					// Skip ignored keys
					if containsString(keys, k) {
						continue
					}
					if a[k] != v {
						return false
					}
				}

				return true
			},
		),
	)
}

func containsString(slice []string, s string) bool {
	return slices.Contains(slice, s)
}
