package parser

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePaths_BasicPath(t *testing.T) {
	doc := &openapi3.T{
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/v3/portals", &openapi3.PathItem{
				Post: &openapi3.Operation{
					OperationID: "create-portal",
					RequestBody: &openapi3.RequestBodyRef{
						Ref: "#/components/requestBodies/CreatePortal",
						Value: &openapi3.RequestBody{
							Content: openapi3.Content{
								"application/json": &openapi3.MediaType{
									Schema: &openapi3.SchemaRef{
										Value: &openapi3.Schema{
											Type:        &openapi3.Types{"object"},
											Description: "Create a portal",
											Properties: openapi3.Schemas{
												"name": &openapi3.SchemaRef{
													Value: &openapi3.Schema{
														Type:        &openapi3.Types{"string"},
														Description: "Portal name",
														MinLength:   1,
														MaxLength:   openapi3.Ptr(uint64(100)),
													},
												},
												"is_public": &openapi3.SchemaRef{
													Value: &openapi3.Schema{
														Type:        &openapi3.Types{"boolean"},
														Description: "Whether the portal is public",
													},
												},
											},
											Required: []string{"name"},
										},
									},
								},
							},
						},
					},
				},
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{},
		},
	}

	parser := NewParser(doc)
	result, err := parser.ParsePaths([]string{"/v3/portals"})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.RequestBodies, 1)

	schema, ok := result.RequestBodies["CreatePortal"]
	require.True(t, ok, "expected CreatePortal schema to exist")
	assert.Equal(t, "CreatePortal", schema.Name)
	assert.Equal(t, "Create a portal", schema.Description)
	assert.Empty(t, schema.Dependencies)

	// Check properties
	require.Len(t, schema.Properties, 2)

	// Properties are sorted alphabetically
	assert.Equal(t, "is_public", schema.Properties[0].Name)
	assert.Equal(t, "boolean", schema.Properties[0].Type)
	assert.False(t, schema.Properties[0].Required)

	assert.Equal(t, "name", schema.Properties[1].Name)
	assert.Equal(t, "string", schema.Properties[1].Type)
	assert.True(t, schema.Properties[1].Required)
	assert.Equal(t, int64(1), *schema.Properties[1].MinLength)
	assert.Equal(t, int64(100), *schema.Properties[1].MaxLength)
}

func TestParsePaths_WithPathDependencies(t *testing.T) {
	doc := &openapi3.T{
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/v3/portals/{portalId}/teams", &openapi3.PathItem{
				Post: &openapi3.Operation{
					OperationID: "create-portal-team",
					RequestBody: &openapi3.RequestBodyRef{
						Value: &openapi3.RequestBody{
							Content: openapi3.Content{
								"application/json": &openapi3.MediaType{
									Schema: &openapi3.SchemaRef{
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
									},
								},
							},
						},
					},
				},
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{},
		},
	}

	parser := NewParser(doc)
	result, err := parser.ParsePaths([]string{"/v3/portals/{portalId}/teams"})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.RequestBodies, 1)

	schema, ok := result.RequestBodies["CreatePortalTeam"]
	require.True(t, ok)

	// Check dependencies from path parameters
	require.Len(t, schema.Dependencies, 1)
	dep := schema.Dependencies[0]
	assert.Equal(t, "portalId", dep.ParamName)
	assert.Equal(t, "Portal", dep.EntityName)
	assert.Equal(t, "PortalRef", dep.FieldName)
	assert.Equal(t, "portal_ref", dep.JSONName)
}

func TestParsePaths_MultiplePaths(t *testing.T) {
	doc := &openapi3.T{
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/v3/portals", &openapi3.PathItem{
				Post: &openapi3.Operation{
					OperationID: "create-portal",
					RequestBody: &openapi3.RequestBodyRef{
						Value: &openapi3.RequestBody{
							Content: openapi3.Content{
								"application/json": &openapi3.MediaType{
									Schema: &openapi3.SchemaRef{
										Value: &openapi3.Schema{
											Type: &openapi3.Types{"object"},
											Properties: openapi3.Schemas{
												"name": &openapi3.SchemaRef{
													Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}),
			openapi3.WithPath("/v3/portals/{portalId}/teams", &openapi3.PathItem{
				Post: &openapi3.Operation{
					OperationID: "create-portal-team",
					RequestBody: &openapi3.RequestBodyRef{
						Value: &openapi3.RequestBody{
							Content: openapi3.Content{
								"application/json": &openapi3.MediaType{
									Schema: &openapi3.SchemaRef{
										Value: &openapi3.Schema{
											Type: &openapi3.Types{"object"},
											Properties: openapi3.Schemas{
												"team_name": &openapi3.SchemaRef{
													Value: &openapi3.Schema{Type: &openapi3.Types{"string"}},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{},
		},
	}

	parser := NewParser(doc)
	result, err := parser.ParsePaths([]string{"/v3/portals", "/v3/portals/{portalId}/teams"})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.RequestBodies, 2)

	_, hasPortal := result.RequestBodies["CreatePortal"]
	assert.True(t, hasPortal)

	_, hasTeam := result.RequestBodies["CreatePortalTeam"]
	assert.True(t, hasTeam)
}

func TestParsePaths_PathNotFound(t *testing.T) {
	doc := &openapi3.T{
		Paths: openapi3.NewPaths(),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{},
		},
	}

	parser := NewParser(doc)
	result, err := parser.ParsePaths([]string{"/nonexistent/path"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "path not found: /nonexistent/path")
	assert.Nil(t, result)
}

func TestParsePaths_NoPostOperation(t *testing.T) {
	doc := &openapi3.T{
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/v3/portals", &openapi3.PathItem{
				Get: &openapi3.Operation{
					OperationID: "list-portals",
				},
				// No Post operation
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{},
		},
	}

	parser := NewParser(doc)
	result, err := parser.ParsePaths([]string{"/v3/portals"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not have a POST operation")
	assert.Nil(t, result)
}

func TestParsePaths_NoRequestBody(t *testing.T) {
	doc := &openapi3.T{
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/v3/portals", &openapi3.PathItem{
				Post: &openapi3.Operation{
					OperationID: "create-portal",
					// No RequestBody
				},
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{},
		},
	}

	parser := NewParser(doc)
	result, err := parser.ParsePaths([]string{"/v3/portals"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no request body")
	assert.Nil(t, result)
}

func TestParsePaths_WithReferencedSchemas(t *testing.T) {
	doc := &openapi3.T{
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/v3/portals", &openapi3.PathItem{
				Post: &openapi3.Operation{
					OperationID: "create-portal",
					RequestBody: &openapi3.RequestBodyRef{
						Value: &openapi3.RequestBody{
							Content: openapi3.Content{
								"application/json": &openapi3.MediaType{
									Schema: &openapi3.SchemaRef{
										Value: &openapi3.Schema{
											Type: &openapi3.Types{"object"},
											Properties: openapi3.Schemas{
												"labels": &openapi3.SchemaRef{
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
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{
				"LabelsUpdate": &openapi3.SchemaRef{
					Value: &openapi3.Schema{
						Type:        &openapi3.Types{"object"},
						Description: "Labels for the entity",
						AdditionalProperties: openapi3.AdditionalProperties{
							Schema: &openapi3.SchemaRef{
								Value: &openapi3.Schema{
									Type: &openapi3.Types{"string"},
								},
							},
						},
					},
				},
			},
		},
	}

	parser := NewParser(doc)
	result, err := parser.ParsePaths([]string{"/v3/portals"})

	require.NoError(t, err)
	require.NotNil(t, result)

	// Check that referenced schema is in Schemas
	labelsSchema, ok := result.Schemas["LabelsUpdate"]
	require.True(t, ok, "expected LabelsUpdate schema to be parsed")
	assert.Equal(t, "Labels for the entity", labelsSchema.Description)
}

func TestParsePaths_WithEnumValidation(t *testing.T) {
	doc := &openapi3.T{
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/v3/portals", &openapi3.PathItem{
				Post: &openapi3.Operation{
					OperationID: "create-portal",
					RequestBody: &openapi3.RequestBodyRef{
						Value: &openapi3.RequestBody{
							Content: openapi3.Content{
								"application/json": &openapi3.MediaType{
									Schema: &openapi3.SchemaRef{
										Value: &openapi3.Schema{
											Type: &openapi3.Types{"object"},
											Properties: openapi3.Schemas{
												"visibility": &openapi3.SchemaRef{
													Value: &openapi3.Schema{
														Type:        &openapi3.Types{"string"},
														Description: "Portal visibility",
														Enum:        []interface{}{"public", "private", "internal"},
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
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{},
		},
	}

	parser := NewParser(doc)
	result, err := parser.ParsePaths([]string{"/v3/portals"})

	require.NoError(t, err)
	require.NotNil(t, result)

	schema := result.RequestBodies["CreatePortal"]
	require.Len(t, schema.Properties, 1)

	visibilityProp := schema.Properties[0]
	assert.Equal(t, "visibility", visibilityProp.Name)
	assert.Equal(t, []interface{}{"public", "private", "internal"}, visibilityProp.Enum)
}

func TestParsePaths_WithPatternValidation(t *testing.T) {
	doc := &openapi3.T{
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/v3/portals", &openapi3.PathItem{
				Post: &openapi3.Operation{
					OperationID: "create-portal",
					RequestBody: &openapi3.RequestBodyRef{
						Value: &openapi3.RequestBody{
							Content: openapi3.Content{
								"application/json": &openapi3.MediaType{
									Schema: &openapi3.SchemaRef{
										Value: &openapi3.Schema{
											Type: &openapi3.Types{"object"},
											Properties: openapi3.Schemas{
												"slug": &openapi3.SchemaRef{
													Value: &openapi3.Schema{
														Type:    &openapi3.Types{"string"},
														Pattern: "^[a-z0-9-]+$",
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
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{},
		},
	}

	parser := NewParser(doc)
	result, err := parser.ParsePaths([]string{"/v3/portals"})

	require.NoError(t, err)
	require.NotNil(t, result)

	schema := result.RequestBodies["CreatePortal"]
	require.Len(t, schema.Properties, 1)

	slugProp := schema.Properties[0]
	assert.Equal(t, "slug", slugProp.Name)
	assert.Equal(t, "^[a-z0-9-]+$", slugProp.Pattern)
}

func TestParsePaths_WithArrayType(t *testing.T) {
	doc := &openapi3.T{
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/v3/portals", &openapi3.PathItem{
				Post: &openapi3.Operation{
					OperationID: "create-portal",
					RequestBody: &openapi3.RequestBodyRef{
						Value: &openapi3.RequestBody{
							Content: openapi3.Content{
								"application/json": &openapi3.MediaType{
									Schema: &openapi3.SchemaRef{
										Value: &openapi3.Schema{
											Type: &openapi3.Types{"object"},
											Properties: openapi3.Schemas{
												"tags": &openapi3.SchemaRef{
													Value: &openapi3.Schema{
														Type:        &openapi3.Types{"array"},
														Description: "List of tags",
														Items: &openapi3.SchemaRef{
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
					},
				},
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{},
		},
	}

	parser := NewParser(doc)
	result, err := parser.ParsePaths([]string{"/v3/portals"})

	require.NoError(t, err)
	require.NotNil(t, result)

	schema := result.RequestBodies["CreatePortal"]
	require.Len(t, schema.Properties, 1)

	tagsProp := schema.Properties[0]
	assert.Equal(t, "tags", tagsProp.Name)
	assert.Equal(t, "array", tagsProp.Type)
	require.NotNil(t, tagsProp.Items)
	assert.Equal(t, "string", tagsProp.Items.Type)
}

func TestParsePaths_WithNestedDependencies(t *testing.T) {
	doc := &openapi3.T{
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/v3/portals/{portalId}/teams/{teamId}/members", &openapi3.PathItem{
				Post: &openapi3.Operation{
					OperationID: "create-portal-team-member",
					RequestBody: &openapi3.RequestBodyRef{
						Value: &openapi3.RequestBody{
							Content: openapi3.Content{
								"application/json": &openapi3.MediaType{
									Schema: &openapi3.SchemaRef{
										Value: &openapi3.Schema{
											Type: &openapi3.Types{"object"},
											Properties: openapi3.Schemas{
												"user_id": &openapi3.SchemaRef{
													Value: &openapi3.Schema{
														Type:   &openapi3.Types{"string"},
														Format: "uuid",
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
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{},
		},
	}

	parser := NewParser(doc)
	result, err := parser.ParsePaths([]string{"/v3/portals/{portalId}/teams/{teamId}/members"})

	require.NoError(t, err)
	require.NotNil(t, result)

	schema := result.RequestBodies["CreatePortalTeamMember"]
	require.Len(t, schema.Dependencies, 2)

	// Dependencies should be in order of appearance in path
	assert.Equal(t, "portalId", schema.Dependencies[0].ParamName)
	assert.Equal(t, "Portal", schema.Dependencies[0].EntityName)

	assert.Equal(t, "teamId", schema.Dependencies[1].ParamName)
	assert.Equal(t, "Team", schema.Dependencies[1].EntityName)
}

func TestParsePaths_WithReadOnlyProperty(t *testing.T) {
	doc := &openapi3.T{
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/v3/portals", &openapi3.PathItem{
				Post: &openapi3.Operation{
					OperationID: "create-portal",
					RequestBody: &openapi3.RequestBodyRef{
						Value: &openapi3.RequestBody{
							Content: openapi3.Content{
								"application/json": &openapi3.MediaType{
									Schema: &openapi3.SchemaRef{
										Value: &openapi3.Schema{
											Type: &openapi3.Types{"object"},
											Properties: openapi3.Schemas{
												"name": &openapi3.SchemaRef{
													Value: &openapi3.Schema{
														Type: &openapi3.Types{"string"},
													},
												},
												"created_at": &openapi3.SchemaRef{
													Value: &openapi3.Schema{
														Type:     &openapi3.Types{"string"},
														Format:   "date-time",
														ReadOnly: true,
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
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{},
		},
	}

	parser := NewParser(doc)
	result, err := parser.ParsePaths([]string{"/v3/portals"})

	require.NoError(t, err)
	require.NotNil(t, result)

	schema := result.RequestBodies["CreatePortal"]
	require.Len(t, schema.Properties, 2)

	// Find the created_at property
	var createdAtProp *Property
	for _, prop := range schema.Properties {
		if prop.Name == "created_at" {
			createdAtProp = prop
			break
		}
	}
	require.NotNil(t, createdAtProp)
	assert.True(t, createdAtProp.ReadOnly)
}

func TestParsePaths_WithReferenceProperty(t *testing.T) {
	doc := &openapi3.T{
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/v3/portals", &openapi3.PathItem{
				Post: &openapi3.Operation{
					OperationID: "create-portal",
					RequestBody: &openapi3.RequestBodyRef{
						Value: &openapi3.RequestBody{
							Content: openapi3.Content{
								"application/json": &openapi3.MediaType{
									Schema: &openapi3.SchemaRef{
										Value: &openapi3.Schema{
											Type: &openapi3.Types{"object"},
											Properties: openapi3.Schemas{
												"default_application_auth_strategy_id": &openapi3.SchemaRef{
													Value: &openapi3.Schema{
														Type:        &openapi3.Types{"string"},
														Format:      "uuid",
														Description: "The default auth strategy ID",
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
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{},
		},
	}

	parser := NewParser(doc)
	result, err := parser.ParsePaths([]string{"/v3/portals"})

	require.NoError(t, err)
	require.NotNil(t, result)

	schema := result.RequestBodies["CreatePortal"]
	require.Len(t, schema.Properties, 1)

	authStrategyProp := schema.Properties[0]
	assert.Equal(t, "default_application_auth_strategy_id", authStrategyProp.Name)
	assert.True(t, authStrategyProp.IsReference, "property ending with _id and uuid format should be a reference")
}

func TestParsePaths_WithDefaultValue(t *testing.T) {
	doc := &openapi3.T{
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/v3/portals", &openapi3.PathItem{
				Post: &openapi3.Operation{
					OperationID: "create-portal",
					RequestBody: &openapi3.RequestBodyRef{
						Value: &openapi3.RequestBody{
							Content: openapi3.Content{
								"application/json": &openapi3.MediaType{
									Schema: &openapi3.SchemaRef{
										Value: &openapi3.Schema{
											Type: &openapi3.Types{"object"},
											Properties: openapi3.Schemas{
												"visibility": &openapi3.SchemaRef{
													Value: &openapi3.Schema{
														Type:    &openapi3.Types{"string"},
														Default: "private",
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
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{},
		},
	}

	parser := NewParser(doc)
	result, err := parser.ParsePaths([]string{"/v3/portals"})

	require.NoError(t, err)
	require.NotNil(t, result)

	schema := result.RequestBodies["CreatePortal"]
	require.Len(t, schema.Properties, 1)

	visibilityProp := schema.Properties[0]
	assert.Equal(t, "visibility", visibilityProp.Name)
	assert.Equal(t, "private", visibilityProp.Default)
}

func TestParsePaths_WithNullableProperty(t *testing.T) {
	doc := &openapi3.T{
		Paths: openapi3.NewPaths(
			openapi3.WithPath("/v3/portals", &openapi3.PathItem{
				Post: &openapi3.Operation{
					OperationID: "create-portal",
					RequestBody: &openapi3.RequestBodyRef{
						Value: &openapi3.RequestBody{
							Content: openapi3.Content{
								"application/json": &openapi3.MediaType{
									Schema: &openapi3.SchemaRef{
										Value: &openapi3.Schema{
											Type: &openapi3.Types{"object"},
											Properties: openapi3.Schemas{
												"description": &openapi3.SchemaRef{
													Value: &openapi3.Schema{
														Type:     &openapi3.Types{"string"},
														Nullable: true,
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
			}),
		),
		Components: &openapi3.Components{
			Schemas: openapi3.Schemas{},
		},
	}

	parser := NewParser(doc)
	result, err := parser.ParsePaths([]string{"/v3/portals"})

	require.NoError(t, err)
	require.NotNil(t, result)

	schema := result.RequestBodies["CreatePortal"]
	require.Len(t, schema.Properties, 1)

	descProp := schema.Properties[0]
	assert.Equal(t, "description", descProp.Name)
	assert.True(t, descProp.Nullable)
}

func TestExtractPathDependencies(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected []*Dependency
	}{
		{
			name:     "no dependencies",
			path:     "/v3/portals",
			expected: nil,
		},
		{
			name: "single dependency",
			path: "/v3/portals/{portalId}/teams",
			expected: []*Dependency{
				{
					ParamName:  "portalId",
					EntityName: "Portal",
					FieldName:  "PortalRef",
					JSONName:   "portal_ref",
				},
			},
		},
		{
			name: "multiple dependencies",
			path: "/v3/portals/{portalId}/teams/{teamId}/members",
			expected: []*Dependency{
				{
					ParamName:  "portalId",
					EntityName: "Portal",
					FieldName:  "PortalRef",
					JSONName:   "portal_ref",
				},
				{
					ParamName:  "teamId",
					EntityName: "Team",
					FieldName:  "TeamRef",
					JSONName:   "team_ref",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parser := NewParser(&openapi3.T{})
			deps := parser.extractPathDependencies(tc.path)

			if tc.expected == nil {
				assert.Nil(t, deps)
			} else {
				require.Len(t, deps, len(tc.expected))
				for i, expected := range tc.expected {
					assert.Equal(t, expected.ParamName, deps[i].ParamName)
					assert.Equal(t, expected.EntityName, deps[i].EntityName)
					assert.Equal(t, expected.FieldName, deps[i].FieldName)
					assert.Equal(t, expected.JSONName, deps[i].JSONName)
				}
			}
		})
	}
}

func TestGetEntityNameFromParam(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"portalId", "Portal"},
		{"teamId", "Team"},
		{"applicationID", "Application"},
		{"user_id", "User"},
		{"someEntity", "SomeEntity"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := getEntityNameFromParam(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Portal", "portal"},
		{"PortalTeam", "portal_team"},
		{"ApplicationAuthStrategy", "application_auth_strategy"},
		{"already_snake", "already_snake"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := toSnakeCase(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestDeriveSchemaNameFromPath(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		operationID string
		expected    string
	}{
		{
			name:        "from operation ID",
			path:        "/v3/portals",
			operationID: "create-portal",
			expected:    "CreatePortal",
		},
		{
			name:        "from operation ID with multiple parts",
			path:        "/v3/portals/{portalId}/teams",
			operationID: "create-portal-team",
			expected:    "CreatePortalTeam",
		},
		{
			name:        "fallback to path",
			path:        "/v3/portals",
			operationID: "",
			expected:    "CreatePortal",
		},
		{
			name:        "path ending with parameter",
			path:        "/v3/portals/{portalId}",
			operationID: "",
			expected:    "Unknown",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := deriveSchemaNameFromPath(tc.path, tc.operationID)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGetEntityNameFromType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"CreatePortal", "Portal"},
		{"UpdatePortal", "Portal"},
		{"PortalCreateTeam", "PortalTeam"},
		{"DeletePortal", "Portal"},
		{"Portal", "Portal"},
		{"AddDeveloperToTeam", "DeveloperToTeam"},
		{"AddSomething", "Something"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := GetEntityNameFromType(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestExtractCRDName(t *testing.T) {
	tests := []struct {
		name     string
		op       *openapi3.Operation
		expected string
	}{
		{
			name:     "nil operation",
			op:       nil,
			expected: "",
		},
		{
			name:     "no extensions",
			op:       &openapi3.Operation{},
			expected: "",
		},
		{
			name: "extension without crd-name",
			op: &openapi3.Operation{
				Extensions: map[string]any{
					"x-speakeasy-entity-operation": map[string]any{
						"terraform-resource": "PortalTeam#create",
					},
				},
			},
			expected: "",
		},
		{
			name: "extension with crd-name",
			op: &openapi3.Operation{
				Extensions: map[string]any{
					"x-speakeasy-entity-operation": map[string]any{
						"terraform-resource": "PortalTeamDeveloper#create",
						"crd-name":           "Developer",
					},
				},
			},
			expected: "Developer",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extractCRDName(tc.op)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGetRefEntityName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"default_application_auth_strategy_id", "ApplicationAuthStrategy"},
		{"portal_id", "Portal"},
		{"team_id", "Team"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := GetRefEntityName(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
