package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kong/kong-operator/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/crd-from-oas/pkg/parser"
)

func ptr[T any](v T) *T {
	return &v
}

func TestKubebuilderTags(t *testing.T) {
	tests := []struct {
		name        string
		prop        *parser.Property
		entityName  string
		fieldConfig *config.Config
		expected    []string
	}{
		{
			name: "required non-nullable string without validations",
			prop: &parser.Property{
				Name:     "name",
				Type:     "string",
				Required: true,
				Nullable: false,
			},
			expected: []string{
				"+required",
				"+kubebuilder:validation:MinLength=1",
				"+kubebuilder:validation:MaxLength=256",
			},
		},
		{
			name: "optional string",
			prop: &parser.Property{
				Name:     "description",
				Type:     "string",
				Required: false,
			},
			expected: []string{
				"+optional",
				"+kubebuilder:validation:MaxLength=256",
			},
		},
		{
			name: "required nullable string is optional",
			prop: &parser.Property{
				Name:     "title",
				Type:     "string",
				Required: true,
				Nullable: true,
			},
			expected: []string{
				"+optional",
				"+kubebuilder:validation:MaxLength=256",
			},
		},
		{
			name: "string with explicit min and max length",
			prop: &parser.Property{
				Name:      "code",
				Type:      "string",
				Required:  true,
				MinLength: ptr(int64(3)),
				MaxLength: ptr(int64(10)),
			},
			expected: []string{
				"+required",
				"+kubebuilder:validation:MinLength=3",
				"+kubebuilder:validation:MaxLength=10",
			},
		},
		{
			name: "string with pattern",
			prop: &parser.Property{
				Name:     "email",
				Type:     "string",
				Required: true,
				Pattern:  `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`,
			},
			expected: []string{
				"+required",
				"+kubebuilder:validation:MinLength=1",
				"+kubebuilder:validation:MaxLength=256",
				"+kubebuilder:validation:Pattern=`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$`",
			},
		},
		{
			name: "string with enum",
			prop: &parser.Property{
				Name:     "status",
				Type:     "string",
				Required: true,
				Enum:     []interface{}{"active", "inactive", "pending"},
			},
			expected: []string{
				"+required",
				"+kubebuilder:validation:MinLength=1",
				"+kubebuilder:validation:MaxLength=256",
				"+kubebuilder:validation:Enum=active;inactive;pending",
			},
		},
		{
			name: "integer with minimum and maximum",
			prop: &parser.Property{
				Name:     "port",
				Type:     "integer",
				Required: true,
				Minimum:  ptr(float64(1)),
				Maximum:  ptr(float64(65535)),
			},
			expected: []string{
				"+required",
				"+kubebuilder:validation:Minimum=1",
				"+kubebuilder:validation:Maximum=65535",
			},
		},
		{
			name: "integer with only minimum",
			prop: &parser.Property{
				Name:     "retries",
				Type:     "integer",
				Required: false,
				Minimum:  ptr(float64(0)),
			},
			expected: []string{
				"+optional",
				"+kubebuilder:validation:Minimum=0",
			},
		},
		{
			name: "boolean with default true",
			prop: &parser.Property{
				Name:     "enabled",
				Type:     "boolean",
				Required: false,
				Default:  true,
			},
			expected: []string{
				"+optional",
				"+kubebuilder:default=true",
			},
		},
		{
			name: "boolean with default false",
			prop: &parser.Property{
				Name:     "disabled",
				Type:     "boolean",
				Required: false,
				Default:  false,
			},
			expected: []string{
				"+optional",
				"+kubebuilder:default=false",
			},
		},
		{
			name: "string with default value",
			prop: &parser.Property{
				Name:     "protocol",
				Type:     "string",
				Required: false,
				Default:  "https",
			},
			expected: []string{
				"+optional",
				"+kubebuilder:validation:MaxLength=256",
				"+kubebuilder:default=https",
			},
		},
		{
			name: "non-string type without string validations",
			prop: &parser.Property{
				Name:     "count",
				Type:     "integer",
				Required: true,
			},
			expected: []string{
				"+required",
			},
		},
		{
			name: "array type",
			prop: &parser.Property{
				Name:     "items",
				Type:     "array",
				Required: false,
			},
			expected: []string{
				"+optional",
			},
		},
		{
			name: "object type",
			prop: &parser.Property{
				Name:     "metadata",
				Type:     "object",
				Required: false,
			},
			expected: []string{
				"+optional",
			},
		},
		{
			name: "enum with mixed types only uses strings",
			prop: &parser.Property{
				Name:     "priority",
				Type:     "string",
				Required: false,
				Enum:     []interface{}{"low", 1, "high", nil, "medium"},
			},
			expected: []string{
				"+optional",
				"+kubebuilder:validation:MaxLength=256",
				"+kubebuilder:validation:Enum=low;high;medium",
			},
		},
		{
			name: "enum with no string values",
			prop: &parser.Property{
				Name:     "level",
				Type:     "integer",
				Required: false,
				Enum:     []interface{}{1, 2, 3},
			},
			expected: []string{
				"+optional",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := KubebuilderTags(tt.prop, tt.entityName, tt.fieldConfig)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestKubebuilderTags_WithFieldConfig(t *testing.T) {
	tests := []struct {
		name        string
		prop        *parser.Property
		entityName  string
		fieldConfig *config.Config
		expected    []string
	}{
		{
			name: "field with custom validations from config",
			prop: &parser.Property{
				Name:     "url",
				Type:     "string",
				Required: true,
			},
			entityName: "Portal",
			fieldConfig: &config.Config{
				Entities: map[string]*config.EntityConfig{
					"Portal": {
						Fields: map[string]*config.FieldConfig{
							"url": {
								Validations: []string{
									"+kubebuilder:validation:Format=uri",
								},
							},
						},
					},
				},
			},
			expected: []string{
				"+required",
				"+kubebuilder:validation:MinLength=1",
				"+kubebuilder:validation:MaxLength=256",
				"+kubebuilder:validation:Format=uri",
			},
		},
		{
			name: "field with multiple custom validations",
			prop: &parser.Property{
				Name:     "host",
				Type:     "string",
				Required: false,
			},
			entityName: "Service",
			fieldConfig: &config.Config{
				Entities: map[string]*config.EntityConfig{
					"Service": {
						Fields: map[string]*config.FieldConfig{
							"host": {
								Validations: []string{
									"+kubebuilder:validation:Format=hostname",
									"+kubebuilder:validation:XValidation:rule=\"self.matches('^[a-z]')\"",
								},
							},
						},
					},
				},
			},
			expected: []string{
				"+optional",
				"+kubebuilder:validation:MaxLength=256",
				"+kubebuilder:validation:Format=hostname",
				"+kubebuilder:validation:XValidation:rule=\"self.matches('^[a-z]')\"",
			},
		},
		{
			name: "field config for different entity does not apply",
			prop: &parser.Property{
				Name:     "name",
				Type:     "string",
				Required: true,
			},
			entityName: "Portal",
			fieldConfig: &config.Config{
				Entities: map[string]*config.EntityConfig{
					"Service": {
						Fields: map[string]*config.FieldConfig{
							"name": {
								Validations: []string{
									"+kubebuilder:validation:Format=dns",
								},
							},
						},
					},
				},
			},
			expected: []string{
				"+required",
				"+kubebuilder:validation:MinLength=1",
				"+kubebuilder:validation:MaxLength=256",
			},
		},
		{
			name: "field config for different field does not apply",
			prop: &parser.Property{
				Name:     "name",
				Type:     "string",
				Required: true,
			},
			entityName: "Portal",
			fieldConfig: &config.Config{
				Entities: map[string]*config.EntityConfig{
					"Portal": {
						Fields: map[string]*config.FieldConfig{
							"url": {
								Validations: []string{
									"+kubebuilder:validation:Format=uri",
								},
							},
						},
					},
				},
			},
			expected: []string{
				"+required",
				"+kubebuilder:validation:MinLength=1",
				"+kubebuilder:validation:MaxLength=256",
			},
		},
		{
			name: "nil field config",
			prop: &parser.Property{
				Name:     "name",
				Type:     "string",
				Required: true,
			},
			entityName:  "Portal",
			fieldConfig: nil,
			expected: []string{
				"+required",
				"+kubebuilder:validation:MinLength=1",
				"+kubebuilder:validation:MaxLength=256",
			},
		},
		{
			name: "empty field config",
			prop: &parser.Property{
				Name:     "name",
				Type:     "string",
				Required: true,
			},
			entityName: "Portal",
			fieldConfig: &config.Config{
				Entities: map[string]*config.EntityConfig{},
			},
			expected: []string{
				"+required",
				"+kubebuilder:validation:MinLength=1",
				"+kubebuilder:validation:MaxLength=256",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := KubebuilderTags(tt.prop, tt.entityName, tt.fieldConfig)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestKubebuilderTags_DefaultPanic(t *testing.T) {
	prop := &parser.Property{
		Name:     "count",
		Type:     "integer",
		Required: false,
		Default:  123, // int type, not bool or string
	}

	assert.Panics(t, func() {
		KubebuilderTags(prop, "Test", nil)
	})
}
