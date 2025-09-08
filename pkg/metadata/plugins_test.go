package metadata

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
)

type mockObject struct {
	annotations map[string]string
	namespace   string
}

func (m *mockObject) GetAnnotations() map[string]string {
	return m.annotations
}

func (m *mockObject) GetNamespace() string {
	return m.namespace
}

func TestExtractPluginsWithNamespace(t *testing.T) {
	tests := []struct {
		name     string
		obj      *mockObject
		expected []string
	}{
		{
			name: "nil annotations",
			obj: &mockObject{
				annotations: nil,
				namespace:   "default",
			},
			expected: nil,
		},
		{
			name: "no annotations",
			obj: &mockObject{
				annotations: map[string]string{},
				namespace:   "default",
			},
			expected: nil,
		},
		{
			name: "single plugin",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1",
				},
				namespace: "default",
			},
			expected: []string{"default/plugin1"},
		},
		{
			name: "multiple plugins",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1,plugin2,plugin3",
				},
				namespace: "default",
			},
			expected: []string{"default/plugin1", "default/plugin2", "default/plugin3"},
		},
		{
			name: "empty plugin name gets filtered out",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1,,plugin3",
				},
				namespace: "default",
			},
			expected: []string{"default/plugin1", "default/plugin3"},
		},
		{
			name: "plugins with spaces",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: " plugin1 , plugin2 , plugin3 ",
				},
				namespace: "default",
			},
			expected: []string{"default/plugin1", "default/plugin2", "default/plugin3"},
		},
		{
			name: "different namespace",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1,plugin2",
				},
				namespace: "custom",
			},
			expected: []string{"custom/plugin1", "custom/plugin2"},
		},
		{
			name: "empty names are ignored",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1,",
				},
				namespace: "custom",
			},
			expected: []string{"custom/plugin1"},
		},
		{
			name: "whitespaces are ignored",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1, ",
				},
				namespace: "custom",
			},
			expected: []string{"custom/plugin1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractPluginsWithNamespaces(tt.obj)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractPluginsNamespacedNames(t *testing.T) {
	tests := []struct {
		name     string
		obj      *mockObject
		expected []types.NamespacedName
	}{
		{
			name:     "no annotations",
			obj:      &mockObject{},
			expected: nil,
		},
		{
			name: "single plugin",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1",
				},
			},
			expected: []types.NamespacedName{
				{
					Name: "plugin1",
				},
			},
		},
		{
			name: "multiple plugins",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1,plugin2,plugin3",
				},
			},
			expected: []types.NamespacedName{
				{
					Name: "plugin1",
				},
				{
					Name: "plugin2",
				},
				{
					Name: "plugin3",
				},
			},
		},
		{
			name: "empty plugin name gets filtered out",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1,,plugin3",
				},
			},
			expected: []types.NamespacedName{
				{
					Name: "plugin1",
				},
				{
					Name: "plugin3",
				},
			},
		},
		{
			name: "plugins with spaces",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: " plugin1 , plugin2 , plugin3 ",
				},
			},
			expected: []types.NamespacedName{
				{
					Name: "plugin1",
				},
				{
					Name: "plugin2",
				},
				{
					Name: "plugin3",
				},
			},
		},
		{
			name: "different namespace",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "custom:plugin1,plugin2",
				},
			},
			expected: []types.NamespacedName{
				{
					Namespace: "custom",
					Name:      "plugin1",
				},
				{
					Name: "plugin2",
				},
			},
		},
		{
			name: "empty names are ignored",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1,",
				},
			},
			expected: []types.NamespacedName{
				{
					Name: "plugin1",
				},
			},
		},
		{
			name: "empty names are ignored",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "kong:plugin1,",
				},
			},
			expected: []types.NamespacedName{
				{
					Namespace: "kong",
					Name:      "plugin1",
				},
			},
		},
		{
			name: "whitespaces are ignored",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1, ",
				},
			},
			expected: []types.NamespacedName{
				{
					Name: "plugin1",
				},
			},
		},
		{
			name: "mixed",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "kong:plugin1,plugin2",
				},
				namespace: "custom",
			},
			expected: []types.NamespacedName{
				{
					Namespace: "kong",
					Name:      "plugin1",
				},
				{
					Name: "plugin2",
				},
			},
		},
		{
			name: "invalid namespaced plugin",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "kong:",
				},
			},
			expected: []types.NamespacedName{},
		},
		{
			name: "invalid namespaced plugin",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: ":plugin1",
				},
			},
			expected: []types.NamespacedName{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractPluginsNamespacedNames(tt.obj)

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractPlugins(t *testing.T) {
	tests := []struct {
		name     string
		obj      *mockObject
		expected []string
	}{
		{
			name: "nil annotations",
			obj: &mockObject{
				annotations: nil,
			},
			expected: nil,
		},
		{
			name: "no annotations",
			obj: &mockObject{
				annotations: map[string]string{},
			},
			expected: nil,
		},
		{
			name: "single plugin",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1",
				},
			},
			expected: []string{"plugin1"},
		},
		{
			name: "multiple plugins",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1,plugin2,plugin3",
				},
			},
			expected: []string{"plugin1", "plugin2", "plugin3"},
		},
		{
			name: "plugins with spaces",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: " plugin1 , plugin2 , plugin3 ",
				},
			},
			expected: []string{"plugin1", "plugin2", "plugin3"},
		},
		{
			name: "empty plugin names",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1,,plugin3",
				},
			},
			expected: []string{"plugin1", "plugin3"},
		},
		{
			name: "trailing comma",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1,plugin2,",
				},
			},
			expected: []string{"plugin1", "plugin2"},
		},
		{
			name: "leading comma",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: ",plugin1,plugin2",
				},
			},
			expected: []string{"plugin1", "plugin2"},
		},
		{
			name: "empty names are ignored",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1,",
				},
			},
			expected: []string{"plugin1"},
		},
		{
			name: "whitespaces are ignored",
			obj: &mockObject{
				annotations: map[string]string{
					AnnotationKeyPlugins: "plugin1, ",
				},
			},
			expected: []string{"plugin1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractPlugins(tt.obj)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func objectWithNPluginsForBenchmark(n int) ObjectWithAnnotationsAndNamespace {
	obj := &mockObject{
		annotations: map[string]string{},
		namespace:   "custom",
	}

	plugins := make([]string, 0, n)
	for i := 0; i < n; i++ {
		plugins = append(plugins, "plugin"+strconv.Itoa(i))
	}
	obj.annotations[AnnotationKeyPlugins] = strings.Join(plugins, ",")
	return obj
}

func consumeSlice[
	T ~[]R,
	R any,
](
	t T,
) {
	for _, v := range t {
		_ = v
	}
}

func BenchmarkExtractPlugins(b *testing.B) {
	benchmarkSlice(b, ExtractPlugins)
}

func BenchmarkExtractPluginsWithNamespaces(b *testing.B) {
	benchmarkSlice(b, ExtractPluginsWithNamespaces)
}

func BenchmarkExtractPluginsNamespacedNames(b *testing.B) {
	benchmarkSlice(b, ExtractPluginsNamespacedNames)
}

func benchmarkPluginCountTestcases() []int {
	return []int{0, 1, 2, 3, 4, 5, 10, 32}
}

func benchmarkSlice[
	T any,
](b *testing.B, f func(ObjectWithAnnotationsAndNamespace) []T) {
	for _, tc := range benchmarkPluginCountTestcases() {
		obj := objectWithNPluginsForBenchmark(tc)

		b.Run(fmt.Sprintf("%04d", tc), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				ret := f(obj)
				consumeSlice(ret)
			}
		})
	}
}
