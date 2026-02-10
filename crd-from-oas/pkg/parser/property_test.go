package parser

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProperty_BasicString(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:        &openapi3.Types{"string"},
			Description: "A string property",
			Format:      "email",
		},
	}

	prop := ParseProperty("email", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "email", prop.Name)
	assert.Equal(t, "string", prop.Type)
	assert.Equal(t, "email", prop.Format)
	assert.Equal(t, "A string property", prop.Description)
}

func TestParseProperty_StringValidations(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:      &openapi3.Types{"string"},
			MinLength: 5,
			MaxLength: openapi3.Ptr(uint64(100)),
			Pattern:   "^[a-z]+$",
		},
	}

	prop := ParseProperty("name", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "string", prop.Type)
	require.NotNil(t, prop.MinLength)
	assert.Equal(t, int64(5), *prop.MinLength)
	require.NotNil(t, prop.MaxLength)
	assert.Equal(t, int64(100), *prop.MaxLength)
	assert.Equal(t, "^[a-z]+$", prop.Pattern)
}

func TestParseProperty_NumberValidations(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"number"},
			Min:  openapi3.Ptr(float64(0.0)),
			Max:  openapi3.Ptr(float64(100.0)),
		},
	}

	prop := ParseProperty("score", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "number", prop.Type)
	require.NotNil(t, prop.Minimum)
	assert.Equal(t, 0.0, *prop.Minimum)
	require.NotNil(t, prop.Maximum)
	assert.Equal(t, 100.0, *prop.Maximum)
}

func TestParseProperty_IntegerType(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:   &openapi3.Types{"integer"},
			Format: "int32",
		},
	}

	prop := ParseProperty("count", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "integer", prop.Type)
	assert.Equal(t, "int32", prop.Format)
}

func TestParseProperty_BooleanType(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:        &openapi3.Types{"boolean"},
			Description: "Is active flag",
		},
	}

	prop := ParseProperty("is_active", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "boolean", prop.Type)
	assert.Equal(t, "Is active flag", prop.Description)
}

func TestParseProperty_EnumValues(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"string"},
			Enum: []interface{}{"active", "inactive", "pending"},
		},
	}

	prop := ParseProperty("status", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "string", prop.Type)
	assert.Equal(t, []interface{}{"active", "inactive", "pending"}, prop.Enum)
}

func TestParseProperty_DefaultValue(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:    &openapi3.Types{"string"},
			Default: "default_value",
		},
	}

	prop := ParseProperty("field", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "default_value", prop.Default)
}

func TestParseProperty_NullableProperty(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"string"},
			Nullable: true,
		},
	}

	prop := ParseProperty("optional_field", schemaRef, 0, make(map[string]bool))

	assert.True(t, prop.Nullable)
}

func TestParseProperty_ReadOnlyProperty(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:     &openapi3.Types{"string"},
			Format:   "date-time",
			ReadOnly: true,
		},
	}

	prop := ParseProperty("created_at", schemaRef, 0, make(map[string]bool))

	assert.True(t, prop.ReadOnly)
	assert.Equal(t, "date-time", prop.Format)
}

func TestParseProperty_ReferenceProperty(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:   &openapi3.Types{"string"},
			Format: "uuid",
		},
	}

	prop := ParseProperty("portal_id", schemaRef, 0, make(map[string]bool))

	assert.True(t, prop.IsReference, "property ending with _id and uuid format should be a reference")
}

func TestParseProperty_NonReferenceUUID(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:   &openapi3.Types{"string"},
			Format: "uuid",
		},
	}

	prop := ParseProperty("uuid_field", schemaRef, 0, make(map[string]bool))

	assert.False(t, prop.IsReference, "uuid property not ending with _id should not be a reference")
}

func TestParseProperty_ArrayType(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:        &openapi3.Types{"array"},
			Description: "List of tags",
			Items: &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Type: &openapi3.Types{"string"},
				},
			},
		},
	}

	prop := ParseProperty("tags", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "array", prop.Type)
	assert.Equal(t, "List of tags", prop.Description)
	require.NotNil(t, prop.Items)
	assert.Equal(t, "string", prop.Items.Type)
}

func TestParseProperty_ArrayOfObjects(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"array"},
			Items: &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Type: &openapi3.Types{"object"},
					Properties: openapi3.Schemas{
						"id": &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Type: &openapi3.Types{"string"},
							},
						},
						"name": &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Type: &openapi3.Types{"string"},
							},
						},
					},
				},
			},
		},
	}

	prop := ParseProperty("items", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "array", prop.Type)
	require.NotNil(t, prop.Items)
	assert.Equal(t, "object", prop.Items.Type)
	require.Len(t, prop.Items.Properties, 2)
}

func TestParseProperty_NestedObject(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"street": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
					},
				},
				"city": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
					},
				},
				"zip": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
					},
				},
			},
			Required: []string{"street", "city"},
		},
	}

	prop := ParseProperty("address", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "object", prop.Type)
	require.Len(t, prop.Properties, 3)

	// Properties should be sorted alphabetically
	assert.Equal(t, "city", prop.Properties[0].Name)
	assert.True(t, prop.Properties[0].Required)

	assert.Equal(t, "street", prop.Properties[1].Name)
	assert.True(t, prop.Properties[1].Required)

	assert.Equal(t, "zip", prop.Properties[2].Name)
	assert.False(t, prop.Properties[2].Required)
}

func TestParseProperty_MapType(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			AdditionalProperties: openapi3.AdditionalProperties{
				Schema: &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
					},
				},
			},
		},
	}

	prop := ParseProperty("labels", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "object", prop.Type)
	require.NotNil(t, prop.AdditionalProperties)
	assert.Equal(t, "string", prop.AdditionalProperties.Type)
}

func TestParseProperty_WithSchemaRef(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Ref: "#/components/schemas/LabelsUpdate",
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			AdditionalProperties: openapi3.AdditionalProperties{
				Schema: &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
					},
				},
			},
		},
	}

	prop := ParseProperty("labels", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "LabelsUpdate", prop.RefName)
	assert.Equal(t, "object", prop.Type)
}

func TestParseProperty_CycleDetection(t *testing.T) {
	visited := map[string]bool{
		"RecursiveSchema": true, // Already visited
	}

	schemaRef := &openapi3.SchemaRef{
		Ref: "#/components/schemas/RecursiveSchema",
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"name": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
					},
				},
			},
		},
	}

	prop := ParseProperty("recursive", schemaRef, 0, visited)

	// Should return early with just the ref name set, no type/properties
	assert.Equal(t, "RecursiveSchema", prop.RefName)
	assert.Equal(t, "", prop.Type)
	assert.Empty(t, prop.Properties)
}

func TestParseProperty_DepthLimit(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"nested": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"string"},
					},
				},
			},
		},
	}

	// Depth > 10 should return early
	prop := ParseProperty("deep", schemaRef, 11, make(map[string]bool))

	assert.Equal(t, "deep", prop.Name)
	assert.Equal(t, "", prop.Type)
	assert.Empty(t, prop.Properties)
}

func TestParseProperty_NilSchemaValue(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Ref:   "#/components/schemas/SomeSchema",
		Value: nil,
	}

	prop := ParseProperty("field", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "field", prop.Name)
	assert.Equal(t, "SomeSchema", prop.RefName)
	assert.Equal(t, "", prop.Type)
}

func TestParseProperty_EmptySchema(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{},
	}

	prop := ParseProperty("empty", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "empty", prop.Name)
	assert.Equal(t, "", prop.Type)
}

func TestParseProperty_MultipleTypes(t *testing.T) {
	// OpenAPI 3.1 allows multiple types like ["string", "null"]
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"string", "null"},
		},
	}

	prop := ParseProperty("nullable_string", schemaRef, 0, make(map[string]bool))

	// Should return the first non-null type
	assert.Equal(t, "string", prop.Type)
}

func TestParseProperty_DeeplyNestedObject(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: openapi3.Schemas{
				"level1": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"object"},
						Properties: openapi3.Schemas{
							"level2": &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type: &openapi3.Types{"object"},
									Properties: openapi3.Schemas{
										"value": &openapi3.SchemaRef{
											Value: &openapi3.Schema{
												Type: &openapi3.Types{"string"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	prop := ParseProperty("nested", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "object", prop.Type)
	require.Len(t, prop.Properties, 1)

	level1 := prop.Properties[0]
	assert.Equal(t, "level1", level1.Name)
	assert.Equal(t, "object", level1.Type)
	require.Len(t, level1.Properties, 1)

	level2 := level1.Properties[0]
	assert.Equal(t, "level2", level2.Name)
	assert.Equal(t, "object", level2.Type)
	require.Len(t, level2.Properties, 1)

	value := level2.Properties[0]
	assert.Equal(t, "value", value.Name)
	assert.Equal(t, "string", value.Type)
}

func TestParseProperty_AllValidations(t *testing.T) {
	schemaRef := &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type:        &openapi3.Types{"string"},
			Description: "Full validation test",
			Format:      "custom",
			MinLength:   5,
			MaxLength:   openapi3.Ptr(uint64(50)),
			Pattern:     "^[a-z]+$",
			Enum:        []interface{}{"a", "b", "c"},
			Default:     "a",
			Nullable:    true,
			ReadOnly:    true,
			Min:         openapi3.Ptr(float64(1.0)),
			Max:         openapi3.Ptr(float64(100.0)),
		},
	}

	prop := ParseProperty("full", schemaRef, 0, make(map[string]bool))

	assert.Equal(t, "full", prop.Name)
	assert.Equal(t, "string", prop.Type)
	assert.Equal(t, "custom", prop.Format)
	assert.Equal(t, "Full validation test", prop.Description)
	assert.Equal(t, int64(5), *prop.MinLength)
	assert.Equal(t, int64(50), *prop.MaxLength)
	assert.Equal(t, "^[a-z]+$", prop.Pattern)
	assert.Equal(t, []interface{}{"a", "b", "c"}, prop.Enum)
	assert.Equal(t, "a", prop.Default)
	assert.True(t, prop.Nullable)
	assert.True(t, prop.ReadOnly)
	assert.Equal(t, 1.0, *prop.Minimum)
	assert.Equal(t, 100.0, *prop.Maximum)
}
