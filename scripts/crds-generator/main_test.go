package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func allOf(entries ...apiext.JSONSchemaProps) *apiext.JSONSchemaProps {
	return &apiext.JSONSchemaProps{AllOf: entries}
}

func minLengthSchema(i int64) apiext.JSONSchemaProps {
	return apiext.JSONSchemaProps{MinLength: new(i)}
}

func TestSimplifyAllOf(t *testing.T) {
	tests := []struct {
		name     string
		in       *apiext.JSONSchemaProps
		expected *apiext.JSONSchemaProps
	}{
		{
			name: "duplicate allOf entries are removed and the single remaining entry is hoisted",
			in: &apiext.JSONSchemaProps{
				MaxLength: new(int64(253)),
				AllOf: []apiext.JSONSchemaProps{
					minLengthSchema(1),
					minLengthSchema(1),
				},
			},
			expected: &apiext.JSONSchemaProps{
				MaxLength: new(int64(253)),
				MinLength: new(int64(1)),
			},
		},
		{
			name:     "single allOf entry that the parent does not conflict with is hoisted",
			in:       allOf(minLengthSchema(1)),
			expected: &apiext.JSONSchemaProps{MinLength: new(int64(1))},
		},
		{
			name: "conflicting single allOf entry is kept",
			in: &apiext.JSONSchemaProps{
				MinLength: new(int64(2)),
				AllOf:     []apiext.JSONSchemaProps{minLengthSchema(1)},
			},
			expected: &apiext.JSONSchemaProps{
				MinLength: new(int64(2)),
				AllOf:     []apiext.JSONSchemaProps{minLengthSchema(1)},
			},
		},
		{
			name: "multiple distinct allOf entries are kept",
			in: &apiext.JSONSchemaProps{
				AllOf: []apiext.JSONSchemaProps{
					minLengthSchema(1),
					minLengthSchema(3),
				},
			},
			expected: &apiext.JSONSchemaProps{
				AllOf: []apiext.JSONSchemaProps{
					minLengthSchema(1),
					minLengthSchema(3),
				},
			},
		},
		{
			name: "duplicate anyOf entries are removed but not hoisted",
			in: &apiext.JSONSchemaProps{
				AnyOf: []apiext.JSONSchemaProps{
					minLengthSchema(1),
					minLengthSchema(1),
				},
			},
			expected: &apiext.JSONSchemaProps{
				AnyOf: []apiext.JSONSchemaProps{minLengthSchema(1)},
			},
		},
		{
			name: "hoisting recurses into the merged schema's nested properties",
			in: &apiext.JSONSchemaProps{
				Properties: map[string]apiext.JSONSchemaProps{
					"kind": {
						AllOf: []apiext.JSONSchemaProps{
							minLengthSchema(1),
							minLengthSchema(1),
						},
					},
				},
				AllOf: []apiext.JSONSchemaProps{minLengthSchema(3)},
			},
			expected: &apiext.JSONSchemaProps{
				MinLength: new(int64(3)),
				Properties: map[string]apiext.JSONSchemaProps{
					"kind": minLengthSchema(1),
				},
			},
		},
		{
			name: "nested schemas are simplified",
			in: &apiext.JSONSchemaProps{
				Properties: map[string]apiext.JSONSchemaProps{
					"name": {
						AllOf: []apiext.JSONSchemaProps{
							minLengthSchema(1),
							minLengthSchema(1),
						},
					},
				},
			},
			expected: &apiext.JSONSchemaProps{
				Properties: map[string]apiext.JSONSchemaProps{
					"name": minLengthSchema(1),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := simplifyAllOf(tt.in); err != nil {
				t.Fatalf("simplifyAllOf() error = %v", err)
			}

			// The hoisting path round-trips the schema through JSON, which
			// turns nil slices into empty ones (and vice versa). Compare the
			// canonical JSON instead of the structs directly.
			got, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatalf("failed to marshal actual schema: %v", err)
			}

			want, err := json.Marshal(tt.expected)
			if err != nil {
				t.Fatalf("failed to marshal expected schema: %v", err)
			}

			if !bytes.Equal(got, want) {
				t.Fatalf("simplifyAllOf() = %s, want %s", got, want)
			}
		})
	}
}

func TestDropRequiredField(t *testing.T) {
	schema := &apiext.JSONSchemaProps{
		Properties: map[string]apiext.JSONSchemaProps{
			"spec": {
				Required: []string{"cloud_gateway", "name"},
				Properties: map[string]apiext.JSONSchemaProps{
					"createControlPlaneRequest": {
						Required: []string{"cloud_gateway", "name"},
					},
				},
			},
		},
	}

	dropRequiredField(schema, "spec", "createControlPlaneRequest", "cloud_gateway")

	got := schema.Properties["spec"].Properties["createControlPlaneRequest"].Required
	if want := []string{"name"}; !slices.Equal(got, want) {
		t.Fatalf("required = %v, want %v", got, want)
	}
	if got, want := schema.Properties["spec"].Required, []string{"cloud_gateway", "name"}; !slices.Equal(got, want) {
		t.Fatalf("unrelated required list changed: %v, want %v", got, want)
	}

	// A missing path leaves the schema untouched.
	dropRequiredField(schema, "spec", "doesNotExist", "cloud_gateway")
}
