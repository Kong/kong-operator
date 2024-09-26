package annotations

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kong/gateway-operator/pkg/consts"
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
					consts.PluginsAnnotationKey: "plugin1",
				},
				namespace: "default",
			},
			expected: []string{"default/plugin1"},
		},
		{
			name: "multiple plugins",
			obj: &mockObject{
				annotations: map[string]string{
					consts.PluginsAnnotationKey: "plugin1,plugin2,plugin3",
				},
				namespace: "default",
			},
			expected: []string{"default/plugin1", "default/plugin2", "default/plugin3"},
		},
		{
			name: "empty plugin name gets filtered out",
			obj: &mockObject{
				annotations: map[string]string{
					consts.PluginsAnnotationKey: "plugin1,,plugin3",
				},
				namespace: "default",
			},
			expected: []string{"default/plugin1", "default/plugin3"},
		},
		{
			name: "plugins with spaces",
			obj: &mockObject{
				annotations: map[string]string{
					consts.PluginsAnnotationKey: " plugin1 , plugin2 , plugin3 ",
				},
				namespace: "default",
			},
			expected: []string{"default/plugin1", "default/plugin2", "default/plugin3"},
		},
		{
			name: "different namespace",
			obj: &mockObject{
				annotations: map[string]string{
					consts.PluginsAnnotationKey: "plugin1,plugin2",
				},
				namespace: "custom",
			},
			expected: []string{"custom/plugin1", "custom/plugin2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractPluginsWithNamespaces(tt.obj)
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
					consts.PluginsAnnotationKey: "plugin1",
				},
			},
			expected: []string{"plugin1"},
		},
		{
			name: "multiple plugins",
			obj: &mockObject{
				annotations: map[string]string{
					consts.PluginsAnnotationKey: "plugin1,plugin2,plugin3",
				},
			},
			expected: []string{"plugin1", "plugin2", "plugin3"},
		},
		{
			name: "plugins with spaces",
			obj: &mockObject{
				annotations: map[string]string{
					consts.PluginsAnnotationKey: " plugin1 , plugin2 , plugin3 ",
				},
			},
			expected: []string{"plugin1", "plugin2", "plugin3"},
		},
		{
			name: "empty plugin names",
			obj: &mockObject{
				annotations: map[string]string{
					consts.PluginsAnnotationKey: "plugin1,,plugin3",
				},
			},
			expected: []string{"plugin1", "plugin3"},
		},
		{
			name: "trailing comma",
			obj: &mockObject{
				annotations: map[string]string{
					consts.PluginsAnnotationKey: "plugin1,plugin2,",
				},
			},
			expected: []string{"plugin1", "plugin2"},
		},
		{
			name: "leading comma",
			obj: &mockObject{
				annotations: map[string]string{
					consts.PluginsAnnotationKey: ",plugin1,plugin2",
				},
			},
			expected: []string{"plugin1", "plugin2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractPlugins(tt.obj)
			assert.Equal(t, tt.expected, result)
		})
	}
}
