package generator

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

func TestApplyValidationOverrides_Requiredness(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		original string
		override string
	}{
		{"required replaces optional", "+optional", "+required"},
		{"optional replaces required", "+required", "+optional"},
		{"long required replaces optional", "+optional", "+kubebuilder:validation:Required"},
		{"long optional replaces required", "+required", "+kubebuilder:validation:Optional"},
		{"short required replaces long optional", "+kubebuilder:validation:Optional", "+required"},
		{"short optional replaces long required", "+kubebuilder:validation:Required", "+optional"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := applyValidationOverrides(
				[]string{tt.original, "+listType=atomic"},
				&config.FieldConfig{Validations: []string{tt.override}},
			)
			assert.Equal(t, []string{"+listType=atomic", tt.override}, got)
		})
	}
}

func TestKubebuilderTags(t *testing.T) {
	tests := []struct {
		name     string
		prop     *parser.Property
		expected []string
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
				"+kubebuilder:validation:MaxLength=253",
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
				"+kubebuilder:validation:MaxLength=253",
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
				"+kubebuilder:validation:MaxLength=253",
			},
		},
		{
			name: "string with explicit min and max length",
			prop: &parser.Property{
				Name:      "code",
				Type:      "string",
				Required:  true,
				MinLength: new(int64(3)),
				MaxLength: new(int64(10)),
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
				"+kubebuilder:validation:MaxLength=253",
				"+kubebuilder:validation:Pattern=`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$`",
			},
		},
		{
			name: "string with enum",
			prop: &parser.Property{
				Name:     "status",
				Type:     "string",
				Required: true,
				Enum:     []any{"active", "inactive", "pending"},
			},
			expected: []string{
				"+required",
				"+kubebuilder:validation:MinLength=1",
				"+kubebuilder:validation:MaxLength=253",
				"+kubebuilder:validation:Enum=active;inactive;pending",
			},
		},
		{
			name: "integer with minimum and maximum",
			prop: &parser.Property{
				Name:     "port",
				Type:     "integer",
				Required: true,
				Minimum:  new(float64(1)),
				Maximum:  new(float64(65535)),
			},
			expected: []string{
				"+required",
				"+kubebuilder:validation:Minimum=1",
				"+kubebuilder:validation:Maximum=65535",
			},
		},
		{
			name: "integer with large maximum is not rendered in scientific notation",
			prop: &parser.Property{
				Name:    "timeout",
				Type:    "integer",
				Minimum: new(float64(0)),
				Maximum: new(float64(2147483646)),
			},
			expected: []string{
				"+optional",
				"+kubebuilder:validation:Minimum=0",
				"+kubebuilder:validation:Maximum=2147483646",
			},
		},
		{
			name: "integer with only minimum",
			prop: &parser.Property{
				Name:     "retries",
				Type:     "integer",
				Required: false,
				Minimum:  new(float64(0)),
			},
			expected: []string{
				"+optional",
				"+kubebuilder:validation:Minimum=0",
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
			name: "array type without maxItems falls back to default",
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
			name: "array type with explicit maxItems",
			prop: &parser.Property{
				Name:     "items",
				Type:     "array",
				Required: false,
				MaxItems: new(int64(5)),
			},
			expected: []string{
				"+optional",
				"+kubebuilder:validation:MaxItems=5",
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
				Enum:     []any{"low", 1, "high", nil, "medium"},
			},
			expected: []string{
				"+optional",
				"+kubebuilder:validation:MaxLength=253",
				"+kubebuilder:validation:Enum=low;high;medium",
			},
		},
		{
			name: "enum with no string values",
			prop: &parser.Property{
				Name:     "level",
				Type:     "integer",
				Required: false,
				Enum:     []any{1, 2, 3},
			},
			expected: []string{
				"+optional",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := KubebuilderTags(tt.prop, nil)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestKubebuilderTags_EnumQuoting(t *testing.T) {
	prop := &parser.Property{
		Name: "upstreamProtocolVersion",
		Type: "string",
		Enum: []any{"2026-07-28", "2025-11-25", "key-auth", "/openai/v1", "private+public"},
	}

	assert.Contains(t, KubebuilderTags(prop, nil),
		`+kubebuilder:validation:Enum="2026-07-28";"2025-11-25";key-auth;/openai/v1;private+public`)
}

func TestQuoteEnumMarkerValue(t *testing.T) {
	tests := map[string]string{
		"inline":      "inline",
		"key-auth":    "key-auth",
		"/openai/v1":  "/openai/v1",
		"2025-06-18":  `"2025-06-18"`,
		"1":           `"1"`,
		"-1":          `"-1"`,
		".5":          `".5"`,
		"true":        `"true"`,
		"false":       `"false"`,
		"":            `""`,
		"a;b":         `"a;b"`,
		"a,b":         `"a,b"`,
		"has space":   `"has space"`,
		"a\nb":        `"a\nb"`,
		"a\rb":        `"a\rb"`,
		`quo"te`:      `"quo\"te"`,
		"key:value":   `"key:value"`,
		"trueish":     "trueish",
		"v2025-06-18": "v2025-06-18",
	}
	for in, want := range tests {
		assert.Equal(t, want, quoteEnumMarkerValue(in), "input %q", in)
	}
}

func TestKubebuilderTags_MapType(t *testing.T) {
	tests := []struct {
		name     string
		prop     *parser.Property
		expected []string
	}{
		{
			name: "map field with MaxProperties",
			prop: &parser.Property{
				Name:          "labels",
				Type:          "object",
				Required:      false,
				MaxProperties: new(int64(50)),
				AdditionalProperties: &parser.Property{
					Name: "value",
					Type: "string",
				},
			},
			expected: []string{
				"+optional",
				"+kubebuilder:validation:MaxProperties=50",
			},
		},
		{
			name: "map ref field with MaxProperties gets no field-level marker",
			// Properties referencing a named map type must not carry a
			// field-level MaxProperties marker: controller-gen rejects it
			// (the field schema has no `type:` when field markers are
			// applied) and silently drops it. The constraint is emitted on
			// the named type declaration instead.
			prop: &parser.Property{
				Name:          "labels",
				Type:          "object",
				Required:      false,
				RefName:       "LabelsUpdate",
				MaxProperties: new(int64(50)),
				AdditionalProperties: &parser.Property{
					Name:      "value",
					Type:      "string",
					MinLength: new(int64(1)),
					MaxLength: new(int64(63)),
				},
			},
			expected: []string{
				"+optional",
			},
		},
		{
			name: "map field without MaxProperties",
			prop: &parser.Property{
				Name:     "tags",
				Type:     "object",
				Required: false,
				AdditionalProperties: &parser.Property{
					Name: "value",
					Type: "string",
				},
			},
			expected: []string{
				"+optional",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := KubebuilderTags(tt.prop, nil)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValueTypeMarkers(t *testing.T) {
	tests := []struct {
		name     string
		prop     *parser.Property
		expected []string
	}{
		{
			name: "string with all constraints",
			prop: &parser.Property{
				Type:      "string",
				MinLength: new(int64(1)),
				MaxLength: new(int64(63)),
				Pattern:   `^[a-z0-9A-Z]+$`,
			},
			expected: []string{
				"+kubebuilder:validation:MinLength=1",
				"+kubebuilder:validation:MaxLength=63",
				"+kubebuilder:validation:Pattern=`^[a-z0-9A-Z]+$`",
			},
		},
		{
			name: "string with only maxLength",
			prop: &parser.Property{
				Type:      "string",
				MaxLength: new(int64(256)),
			},
			expected: []string{
				"+kubebuilder:validation:MaxLength=256",
			},
		},
		{
			name:     "string with no constraints",
			prop:     &parser.Property{Type: "string"},
			expected: nil,
		},
		{
			name: "non-string type returns nil",
			prop: &parser.Property{
				Type:      "integer",
				MinLength: new(int64(1)),
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := valueTypeMarkers(tt.prop)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPropertyToGoBaseType(t *testing.T) {
	tests := []struct {
		propType string
		expected string
	}{
		{"string", "string"},
		{"integer", "int"},
		{"boolean", "bool"},
		{"number", "float64"},
		{"unknown", "string"},
	}

	for _, tt := range tests {
		t.Run(tt.propType, func(t *testing.T) {
			result := propertyToGoBaseType(&parser.Property{Type: tt.propType})
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestKubebuilderTags_WithFieldConfig(t *testing.T) {
	tests := []struct {
		name        string
		prop        *parser.Property
		fieldCursor *config.FieldConfig
		expected    []string
	}{
		{
			name: "field with custom validations from config",
			prop: &parser.Property{
				Name:     "url",
				Type:     "string",
				Required: true,
			},
			// cursor represents the struct containing "url" (json tag matches)
			fieldCursor: &config.FieldConfig{
				Fields: map[string]*config.FieldConfig{
					"url": {
						Validations: []string{
							"+kubebuilder:validation:Format=uri",
						},
					},
				},
			},
			expected: []string{
				"+required",
				"+kubebuilder:validation:MinLength=1",
				"+kubebuilder:validation:MaxLength=253",
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
			fieldCursor: &config.FieldConfig{
				Fields: map[string]*config.FieldConfig{
					"host": {
						Validations: []string{
							"+kubebuilder:validation:Format=hostname",
							"+kubebuilder:validation:XValidation:rule=\"self.matches('^[a-z]')\"",
						},
					},
				},
			},
			expected: []string{
				"+optional",
				"+kubebuilder:validation:MaxLength=253",
				"+kubebuilder:validation:Format=hostname",
				"+kubebuilder:validation:XValidation:rule=\"self.matches('^[a-z]')\"",
			},
		},
		{
			name: "cursor without matching field key does not apply",
			prop: &parser.Property{
				Name:     "name",
				Type:     "string",
				Required: true,
			},
			fieldCursor: &config.FieldConfig{
				Fields: map[string]*config.FieldConfig{
					"url": {
						Validations: []string{
							"+kubebuilder:validation:Format=uri",
						},
					},
				},
			},
			expected: []string{
				"+required",
				"+kubebuilder:validation:MinLength=1",
				"+kubebuilder:validation:MaxLength=253",
			},
		},
		{
			name: "nil cursor",
			prop: &parser.Property{
				Name:     "name",
				Type:     "string",
				Required: true,
			},
			fieldCursor: nil,
			expected: []string{
				"+required",
				"+kubebuilder:validation:MinLength=1",
				"+kubebuilder:validation:MaxLength=253",
			},
		},
		{
			name: "empty cursor fields",
			prop: &parser.Property{
				Name:     "name",
				Type:     "string",
				Required: true,
			},
			fieldCursor: &config.FieldConfig{},
			expected: []string{
				"+required",
				"+kubebuilder:validation:MinLength=1",
				"+kubebuilder:validation:MaxLength=253",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := KubebuilderTags(tt.prop, tt.fieldCursor)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestKubebuilderTags_OverrideMaxLength(t *testing.T) {
	prop := &parser.Property{
		Name:     "certificate",
		Type:     "string",
		Required: true,
	}
	cursor := &config.FieldConfig{
		Fields: map[string]*config.FieldConfig{
			"certificate": {
				Validations: []string{"+kubebuilder:validation:MaxLength=1024"},
			},
		},
	}
	result := KubebuilderTags(prop, cursor)
	assert.Contains(t, result, "+kubebuilder:validation:MaxLength=1024", "user-provided MaxLength should be present")
	assert.NotContains(t, result, "+kubebuilder:validation:MaxLength=253", "default MaxLength should be removed")
}

func TestKubebuilderTags_ArrayConstraints(t *testing.T) {
	on := config.ValidationConfig{ArrayConstraints: true}
	tests := []struct {
		name       string
		prop       *parser.Property
		validation config.ValidationConfig
		expected   []string
	}{
		{
			name: "items of an inline string: minItems and the item constraints",
			prop: &parser.Property{
				Name: "bootstrap_servers", Type: "array", Required: true, MinItems: new(int64(1)),
				Items: &parser.Property{Type: "string", MinLength: new(int64(1)), Pattern: "^[a-z]+:\\d+$"},
			},
			validation: on,
			expected: []string{
				"+required",
				"+kubebuilder:validation:MinItems=1",
				"+kubebuilder:validation:items:MinLength=1",
				"+kubebuilder:validation:items:Pattern=`^[a-z]+:\\d+$`",
			},
		},
		{
			name: "items of an inline string enum",
			prop: &parser.Property{
				Name: "tls_versions", Type: "array",
				Items: &parser.Property{Type: "string", Enum: []any{"tls12", "tls13"}},
			},
			validation: on,
			expected:   []string{"+optional", "+kubebuilder:validation:items:Enum=tls12;tls13"},
		},
		{
			name: "items of an inline integer: bounds",
			prop: &parser.Property{
				Name: "ids", Type: "array", MaxItems: new(int64(10)),
				Items: &parser.Property{Type: "integer", Minimum: new(1.0), Maximum: new(65535.0)},
			},
			validation: on,
			expected: []string{
				"+optional",
				"+kubebuilder:validation:MaxItems=10",
				"+kubebuilder:validation:items:Minimum=1",
				"+kubebuilder:validation:items:Maximum=65535",
			},
		},
		{
			name: "items of a named type: only minItems, the type carries the rest",
			prop: &parser.Property{
				Name: "part_of_record", Type: "array", Required: true, MinItems: new(int64(1)),
				Items: &parser.Property{Type: "string", RefName: "RecordPart", Enum: []any{"key", "value"}},
			},
			validation: on,
			expected:   []string{"+required", "+kubebuilder:validation:MinItems=1"},
		},
		{
			name: "not opted in: no new marker",
			prop: &parser.Property{
				Name: "bootstrap_servers", Type: "array", Required: true, MinItems: new(int64(1)), MaxItems: new(int64(5)),
				Items: &parser.Property{Type: "string", MinLength: new(int64(1)), Pattern: "^x$"},
			},
			expected: []string{"+required", "+kubebuilder:validation:MaxItems=5"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, kubebuilderTagsWith(tt.prop, nil, tt.validation))
		})
	}
	t.Run("KubebuilderTags never emits them", func(t *testing.T) {
		assert.Equal(t, []string{"+required"}, KubebuilderTags(&parser.Property{
			Name: "addresses", Type: "array", Required: true, MinItems: new(int64(1)),
			Items: &parser.Property{Type: "string", MinLength: new(int64(1)), Pattern: "^x$", Enum: []any{"x"}},
		}, nil))
	})
}

func TestNamedTypeConstraintMarkers(t *testing.T) {
	on := &Generator{config: Config{Validation: config.ValidationConfig{ArrayConstraints: true}}}
	off := &Generator{}
	enum := &parser.Schema{Type: "string", Enum: []any{"key", "value"}}
	array := &parser.Schema{Type: "array", MinItems: new(int64(1)), MaxItems: new(int64(50))}

	assert.Equal(t, []string{"+kubebuilder:validation:Enum=key;value"}, on.namedTypeConstraintMarkers(enum))
	assert.Equal(t, []string{"+kubebuilder:validation:MinItems=1", "+kubebuilder:validation:MaxItems=50"}, on.namedTypeConstraintMarkers(array))
	assert.Empty(t, on.namedTypeConstraintMarkers(&parser.Schema{Type: "string"}))
	assert.Empty(t, off.namedTypeConstraintMarkers(enum))
	assert.Empty(t, off.namedTypeConstraintMarkers(array))

	var buf strings.Builder
	on.writeNamedTypeConstraintMarkers(&buf, enum)
	assert.Equal(t, "//\n// +kubebuilder:validation:Enum=key;value\n", buf.String())
}
