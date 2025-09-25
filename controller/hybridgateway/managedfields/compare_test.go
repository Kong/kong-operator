package managedfields

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		name           string
		current        *unstructured.Unstructured
		desired        *unstructured.Unstructured
		expectErr      bool
		expectAdded    string
		expectRemoved  string
		expectModified string
	}{
		{
			name: "success (no diff)",
			current: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongDataPlaneClientCertificate",
				"metadata":   map[string]any{"name": "foo"},
			}},
			desired: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongDataPlaneClientCertificate",
				"metadata":   map[string]any{"name": "foo"},
			}},
			expectErr:      false,
			expectAdded:    "",
			expectRemoved:  "",
			expectModified: "",
		},
		{
			name: "unsupported group",
			current: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "other.com/v1",
				"kind":       "Qux",
				"metadata":   map[string]any{"name": "foo"},
			}},
			desired: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "other.com/v1",
				"kind":       "Qux",
				"metadata":   map[string]any{"name": "foo"},
			}},
			expectErr:      true,
			expectAdded:    "",
			expectRemoved:  "",
			expectModified: "",
		},
		{
			name: "nonexistent kind",
			current: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongDataPlaneClientCertificate",
				"metadata":   map[string]any{"name": "foo"},
			}},
			desired: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "NonexistentKind",
				"metadata":   map[string]any{"name": "foo"},
			}},
			expectErr:      true,
			expectAdded:    "",
			expectRemoved:  "",
			expectModified: "",
		},
		{
			name:    "malformed object (current)",
			current: &unstructured.Unstructured{Object: map[string]any{}},
			desired: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongDataPlaneClientCertificate",
				"metadata":   map[string]any{"name": "foo"},
			}},
			expectErr:      true,
			expectAdded:    "",
			expectRemoved:  "",
			expectModified: "",
		},
		{
			name: "malformed object (desired)",
			current: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongDataPlaneClientCertificate",
				"metadata":   map[string]any{"name": "foo"},
			}},
			desired:        &unstructured.Unstructured{Object: map[string]any{}},
			expectErr:      true,
			expectAdded:    "",
			expectRemoved:  "",
			expectModified: "",
		},
		{
			name: "error from currentParsable.FromUnstructured",
			current: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongDataPlaneClientCertificate",
				"metadata":   map[string]any{"name": 123}, // invalid type, should be string
			}},
			desired: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongDataPlaneClientCertificate",
				"metadata":   map[string]any{"name": "foo"},
			}},
			expectErr:      true,
			expectAdded:    "",
			expectRemoved:  "",
			expectModified: "",
		},
		{
			name: "error from desiredParsable.FromUnstructured",
			current: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongDataPlaneClientCertificate",
				"metadata":   map[string]any{"name": "foo"},
			}},
			desired: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongDataPlaneClientCertificate",
				"metadata":   map[string]any{"name": 123}, // invalid type, should be string
			}},
			expectErr:      true,
			expectAdded:    "",
			expectRemoved:  "",
			expectModified: "",
		},
		{
			name: "added field (paths)",
			current: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongRoute",
				"metadata":   map[string]any{"name": "route1"},
				"spec": map[string]any{
					"hosts":   []any{"example.com"},
					"methods": []any{"GET"},
					"paths":   []any{},
				},
			}},
			desired: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongRoute",
				"metadata":   map[string]any{"name": "route1"},
				"spec": map[string]any{
					"hosts":   []any{"example.com"},
					"methods": []any{"GET"},
					"paths":   []any{"/new"},
				},
			}},
			expectErr:      false,
			expectAdded:    "",
			expectRemoved:  "",
			expectModified: ".spec.paths",
		},
		{
			name: "removed field (paths)",
			current: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongRoute",
				"metadata":   map[string]any{"name": "route1"},
				"spec": map[string]any{
					"hosts":   []any{"example.com"},
					"methods": []any{"GET"},
					"paths":   []any{"/old"},
				},
			}},
			desired: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongRoute",
				"metadata":   map[string]any{"name": "route1"},
				"spec": map[string]any{
					"hosts":   []any{"example.com"},
					"methods": []any{"GET"},
					"paths":   []any{},
				},
			}},
			expectErr:      false,
			expectAdded:    "",
			expectRemoved:  "",
			expectModified: ".spec.paths",
		},
		{
			name: "modified field (methods)",
			current: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongRoute",
				"metadata":   map[string]any{"name": "route1"},
				"spec": map[string]any{
					"hosts":   []any{"example.com"},
					"methods": []any{"GET"},
					"paths":   []any{"/test"},
				},
			}},
			desired: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongRoute",
				"metadata":   map[string]any{"name": "route1"},
				"spec": map[string]any{
					"hosts":   []any{"example.com"},
					"methods": []any{"POST"},
					"paths":   []any{"/test"},
				},
			}},
			expectErr:      false,
			expectAdded:    "",
			expectRemoved:  "",
			expectModified: ".spec.methods",
		},
		{
			name: "added scalar field (regex_priority)",
			current: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongRoute",
				"metadata":   map[string]any{"name": "route2"},
				"spec":       map[string]any{},
			}},
			desired: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongRoute",
				"metadata":   map[string]any{"name": "route2"},
				"spec":       map[string]any{"regex_priority": int64(10)},
			}},
			expectErr:      false,
			expectAdded:    ".spec.regex_priority",
			expectRemoved:  "",
			expectModified: "",
		},
		{
			name: "removed scalar field (regex_priority)",
			current: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongRoute",
				"metadata":   map[string]any{"name": "route2"},
				"spec":       map[string]any{"regex_priority": int64(10)},
			}},
			desired: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongRoute",
				"metadata":   map[string]any{"name": "route2"},
				"spec":       map[string]any{},
			}},
			expectErr:      false,
			expectAdded:    "",
			expectRemoved:  ".spec.regex_priority",
			expectModified: "",
		},
		{
			name: "modified scalar field (regex_priority)",
			current: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongRoute",
				"metadata":   map[string]any{"name": "route2"},
				"spec":       map[string]any{"regex_priority": int64(10)},
			}},
			desired: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "configuration.konghq.com/v1alpha1",
				"kind":       "KongRoute",
				"metadata":   map[string]any{"name": "route2"},
				"spec":       map[string]any{"regex_priority": int64(20)},
			}},
			expectErr:      false,
			expectAdded:    "",
			expectRemoved:  "",
			expectModified: ".spec.regex_priority",
		},
		// Removed cases with extra fields not in schema, as they always error
	}
	for _, tc := range cases {
		cmp, err := Compare(tc.current, tc.desired)
		if tc.expectErr {
			assert.Error(t, err, tc.name)
			assert.Nil(t, cmp, tc.name)
		} else {
			assert.NoError(t, err, tc.name)
			assert.NotNil(t, cmp, tc.name)
			if cmp != nil {
				assert.Equal(t, tc.expectAdded, cmp.Added.String(), tc.name+" added")
				assert.Equal(t, tc.expectRemoved, cmp.Removed.String(), tc.name+" removed")
				assert.Equal(t, tc.expectModified, cmp.Modified.String(), tc.name+" modified")
			}
		}
	}
}
