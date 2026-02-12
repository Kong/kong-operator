package parser

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// Dependency represents a parent resource dependency from a path parameter
type Dependency struct {
	// ParamName is the original path parameter name (e.g., "portalId")
	ParamName string
	// EntityName is the entity name derived from the parameter (e.g., "Portal")
	EntityName string
	// FieldName is the Go field name for the reference (e.g., "PortalRef")
	FieldName string
	// JSONName is the JSON tag name (e.g., "portal_ref")
	JSONName string
}

// Property represents a parsed OpenAPI property with its validations
type Property struct {
	Name        string
	Type        string
	Format      string
	Description string
	Required    bool
	Nullable    bool
	ReadOnly    bool

	// Validations
	MinLength *int64
	MaxLength *int64
	Minimum   *float64
	Maximum   *float64
	Pattern   string
	Enum      []interface{}
	Default   interface{}

	// Reference info
	RefName     string // If this is a $ref, the referenced schema name
	IsReference bool   // True if this property references another object by ID

	// Nested types
	Items                *Property   // For array types
	Properties           []*Property // For object types
	AdditionalProperties *Property   // For map types

	// Union types (oneOf)
	OneOf []*Property // For oneOf types - each represents a variant
}

// Schema represents a parsed OpenAPI schema
type Schema struct {
	Name         string
	Description  string
	Type         string // The schema's type (string, boolean, integer, number, array, object)
	Format       string // The schema's format (url, uri, uuid, etc.)
	Properties   []*Property
	Required     []string
	IsEntity     bool // Has x-speakeasy-entity extension
	EntityName   string
	Dependencies []*Dependency // Parent resource dependencies from path parameters
	OneOf        []*Property   // Root-level oneOf variants (for union type schemas)
	Items        *Property     // For array-type schemas, the items type
}

// ParsedSpec is the result of parsing an OpenAPI spec via ParsePaths.
type ParsedSpec struct {
	// Schemas holds component schemas that are transitively referenced ($ref) by
	// the request body schemas. These are resolved from the spec's
	// components/schemas section and keyed by their component name.
	Schemas map[string]*Schema
	// RequestBodies holds schemas extracted directly from POST request bodies of
	// the target paths, keyed by schema name. Each schema includes parent resource
	// dependencies inferred from path parameters (e.g. {portalId} → Portal dependency).
	RequestBodies map[string]*Schema
}

// Parser parses OpenAPI specs
type Parser struct {
	doc     *openapi3.T
	visited map[string]bool // Track visited schemas to prevent infinite recursion
}

// NewParser creates a new parser
func NewParser(doc *openapi3.T) *Parser {
	return &Parser{
		doc:     doc,
		visited: make(map[string]bool),
	}
}

// ParsePaths parses the OpenAPI spec for each of the given API paths (e.g.
// "/v3/portals/{portalId}/teams") and returns a ParsedSpec containing:
//   - RequestBodies: the request body schema for each path's POST operation,
//     with parent resource dependencies inferred from path parameters.
//   - Schemas: any component schemas transitively referenced by the request bodies.
func (p *Parser) ParsePaths(targetPaths []string) (*ParsedSpec, error) {
	result := &ParsedSpec{
		Schemas:       make(map[string]*Schema),
		RequestBodies: make(map[string]*Schema),
	}

	// Collect referenced schema names
	referencedSchemas := make(map[string]bool)

	for _, targetPath := range targetPaths {
		name, schema, err := p.parsePath(targetPath)
		if err != nil {
			return nil, err
		}
		result.RequestBodies[name] = schema
		p.collectReferencedSchemas(schema, referencedSchemas)
	}

	// Parse all referenced component schemas
	for name := range referencedSchemas {
		if schemaRef, ok := p.doc.Components.Schemas[name]; ok && schemaRef.Value != nil {
			schema := p.parseSchema(name, schemaRef.Value)
			result.Schemas[name] = schema
		}
	}

	return result, nil
}

// parsePath processes a single API path, returning the schema name and parsed schema
// for the POST operation's request body.
func (p *Parser) parsePath(targetPath string) (string, *Schema, error) {
	// Find the path in the OpenAPI spec
	pathItem := p.doc.Paths.Find(targetPath)
	if pathItem == nil {
		return "", nil, fmt.Errorf("path not found: %s", targetPath)
	}

	// We're interested in POST operations (create operations)
	if pathItem.Post == nil {
		return "", nil, fmt.Errorf("path %s does not have a POST operation", targetPath)
	}

	// Get request body
	if pathItem.Post.RequestBody == nil || pathItem.Post.RequestBody.Value == nil {
		return "", nil, fmt.Errorf("path %s POST operation has no request body", targetPath)
	}

	reqBody := pathItem.Post.RequestBody.Value
	if reqBody.Content == nil {
		return "", nil, fmt.Errorf("path %s POST request body has no content", targetPath)
	}

	// Find the schema name from the request body reference
	var schemaName string
	if pathItem.Post.RequestBody.Ref != "" {
		schemaName = extractRefName(pathItem.Post.RequestBody.Ref)
	} else {
		schemaName = deriveSchemaNameFromPath(targetPath, pathItem.Post.OperationID)
	}

	// Extract path parameters as dependencies
	dependencies := p.extractPathDependencies(targetPath)

	// Parse the first media type that has a valid schema
	for _, mediaTypeObj := range reqBody.Content {
		if mediaTypeObj.Schema == nil || mediaTypeObj.Schema.Value == nil {
			continue
		}

		schema := p.parseSchema(schemaName, mediaTypeObj.Schema.Value)
		schema.Dependencies = dependencies
		return schemaName, schema, nil
	}

	return "", nil, fmt.Errorf("path %s POST request body has no valid schema", targetPath)
}

// extractPathDependencies extracts parent resource dependencies from path parameters
func (p *Parser) extractPathDependencies(path string) []*Dependency {
	var deps []*Dependency

	// Extract path parameters using regex (e.g., {portalId})
	paramRegex := regexp.MustCompile(`\{([^}]+)\}`)
	matches := paramRegex.FindAllStringSubmatch(path, -1)

	// All path parameters in a create path are dependencies
	// e.g., POST /v3/portals/{portalId}/teams creates a team under a portal
	// so portalId is a dependency
	for _, match := range matches {
		paramName := match[1] // e.g., "portalId"
		dep := &Dependency{
			ParamName:  paramName,
			EntityName: getEntityNameFromParam(paramName),
			FieldName:  getEntityNameFromParam(paramName) + "Ref",
			JSONName:   toSnakeCase(getEntityNameFromParam(paramName)) + "_ref",
		}
		deps = append(deps, dep)
	}

	return deps
}

// getEntityNameFromParam converts a path parameter name to an entity name
// e.g., "portalId" -> "Portal", "teamId" -> "Team"
func getEntityNameFromParam(paramName string) string {
	// Remove common suffixes
	name := paramName
	for _, suffix := range []string{"Id", "ID", "_id"} {
		name = strings.TrimSuffix(name, suffix)
	}

	// Convert camelCase to PascalCase
	if len(name) > 0 {
		name = strings.ToUpper(name[:1]) + name[1:]
	}

	return name
}

// toSnakeCase converts a string to snake_case
func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// toTitleCase converts the first character of a string to uppercase
func toTitleCase(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// deriveSchemaNameFromPath derives a schema name from the path and operation ID
func deriveSchemaNameFromPath(path string, operationID string) string {
	// Try to use operation ID first (e.g., "create-portal-team" -> "CreatePortalTeam")
	if operationID != "" {
		parts := strings.Split(operationID, "-")
		var result strings.Builder
		for _, part := range parts {
			if len(part) > 0 {
				result.WriteString(strings.ToUpper(part[:1]) + part[1:])
			}
		}
		return result.String()
	}

	// Fallback: derive from path segments
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) > 0 {
		lastSegment := segments[len(segments)-1]
		// Remove any path parameters
		if !strings.HasPrefix(lastSegment, "{") {
			return "Create" + toTitleCase(strings.TrimSuffix(lastSegment, "s"))
		}
	}

	return "Unknown"
}

// collectReferencedSchemas collects all schema names referenced by the schema's properties
func (p *Parser) collectReferencedSchemas(schema *Schema, refs map[string]bool) {
	for _, prop := range schema.Properties {
		p.collectRefsFromProperty(prop, refs)
	}
	// Also collect refs from root-level oneOf variants
	for _, variant := range schema.OneOf {
		p.collectRefsFromProperty(variant, refs)
	}
}

func (p *Parser) collectRefsFromProperty(prop *Property, refs map[string]bool) {
	// Don't collect refs for read-only properties (they won't be in the spec)
	if prop.ReadOnly {
		return
	}
	if prop.RefName != "" && !prop.IsReference {
		refs[prop.RefName] = true
	}
	if prop.Items != nil {
		p.collectRefsFromProperty(prop.Items, refs)
	}
	for _, nestedProp := range prop.Properties {
		p.collectRefsFromProperty(nestedProp, refs)
	}
	if prop.AdditionalProperties != nil {
		p.collectRefsFromProperty(prop.AdditionalProperties, refs)
	}
	// Collect refs from oneOf variants
	for _, variant := range prop.OneOf {
		if variant.RefName != "" {
			refs[variant.RefName] = true
		}
		p.collectRefsFromProperty(variant, refs)
	}
}

func (p *Parser) parseSchema(name string, schemaValue *openapi3.Schema) *Schema {
	schema := &Schema{
		Name:        name,
		Description: schemaValue.Description,
		Type:        getSchemaType(schemaValue),
		Format:      schemaValue.Format,
		Required:    schemaValue.Required,
		Properties:  make([]*Property, 0),
	}

	// Handle array items
	if schema.Type == "array" && schemaValue.Items != nil {
		schema.Items = ParseProperty("items", schemaValue.Items, 0, p.visited)
	}

	// Check for x-speakeasy-entity extension
	if entity, ok := schemaValue.Extensions["x-speakeasy-entity"]; ok {
		schema.IsEntity = true
		if entityStr, ok := entity.(string); ok {
			schema.EntityName = entityStr
		}
	}

	// Handle root-level oneOf (union type schemas)
	if len(schemaValue.OneOf) > 0 {
		for _, oneOfRef := range schemaValue.OneOf {
			variantName := fmt.Sprintf("variant%d", len(schema.OneOf))
			if oneOfRef.Ref != "" {
				variantName = extractRefName(oneOfRef.Ref)
			}
			variantProp := ParseProperty(variantName, oneOfRef, 0, p.visited)
			schema.OneOf = append(schema.OneOf, variantProp)
		}
	}

	// Parse properties
	for propName, propSchemaRef := range schemaValue.Properties {
		if propSchemaRef.Value == nil {
			continue
		}

		prop := ParseProperty(propName, propSchemaRef, 0, p.visited)
		prop.Required = slices.Contains(schemaValue.Required, propName)
		schema.Properties = append(schema.Properties, prop)
	}

	// Sort properties for consistent output
	sort.Slice(schema.Properties, func(i, j int) bool {
		return schema.Properties[i].Name < schema.Properties[j].Name
	})

	return schema
}

// getSchemaType extracts the type from a schema, handling OpenAPI 3.1 type arrays
func getSchemaType(schema *openapi3.Schema) string {
	if schema.Type == nil {
		return ""
	}
	types := schema.Type.Slice()
	if len(types) == 0 {
		return ""
	}
	// Return the first non-null type
	for _, t := range types {
		if t != "null" {
			return t
		}
	}
	return types[0]
}

// extractRefName extracts the schema name from a $ref string
func extractRefName(ref string) string {
	// #/components/schemas/SomeSchema -> SomeSchema
	parts := strings.Split(ref, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ref
}

// isReferenceProperty checks if a property is a reference to another entity
func isReferenceProperty(name string, schema *openapi3.Schema) bool {
	// Check if the property name ends with _id and has uuid format
	if strings.HasSuffix(name, "_id") && schema.Format == "uuid" {
		return true
	}
	return false
}

// GetEntityNameFromType extracts the entity name from a type name
// e.g., "CreatePortal" -> "Portal", "PortalCreateTeam" -> "PortalTeam"
func GetEntityNameFromType(name string) string {
	// Remove common prefixes
	result := name
	for _, prefix := range []string{"Create", "Update", "Delete", "Get", "List"} {
		result = strings.TrimPrefix(result, prefix)
	}

	// Also handle infix patterns like "PortalCreateTeam" -> "PortalTeam"
	for _, infix := range []string{"Create", "Update", "Delete"} {
		result = strings.ReplaceAll(result, infix, "")
	}

	return result
}

// GetRefEntityName extracts the entity name from a reference property name
// e.g., "default_application_auth_strategy_id" -> "ApplicationAuthStrategy"
func GetRefEntityName(propName string) string {
	// Remove _id suffix
	name := strings.TrimSuffix(propName, "_id")

	// Convert snake_case to PascalCase
	parts := strings.Split(name, "_")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}

	// Remove common prefixes like "default_"
	result := strings.Join(parts, "")
	result = strings.TrimPrefix(result, "Default")

	return result
}

// ValidateRefName validates and normalizes a reference name
var refNameRegex = regexp.MustCompile(`^[A-Z][a-zA-Z0-9]*$`)

func ValidateRefName(name string) error {
	if !refNameRegex.MatchString(name) {
		return fmt.Errorf("invalid reference name: %s", name)
	}
	return nil
}
