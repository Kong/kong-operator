package parser

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// Dependency represents a parent resource dependency from a path parameter.
type Dependency struct {
	// ParamName is the original path parameter name (e.g., "portalId")
	ParamName string
	// EntityName is the entity name derived from the parameter (e.g., "Portal")
	EntityName string
	// AccessorEntityName is the entity name inferred from the parent resource
	// segment for generated ref accessors (e.g. "event-gateways" ->
	// "EventGateway").
	AccessorEntityName string
	// FieldName is the Go field name for the reference (e.g., "PortalRef")
	FieldName string
	// JSONName is the JSON tag name (e.g., "portal_ref")
	JSONName string
}

// Property represents a parsed OpenAPI property with its validations.
type Property struct {
	Name        string
	Type        string
	Format      string
	Description string
	Required    bool
	Nullable    bool
	ReadOnly    bool

	// Validations
	MinLength     *int64
	MaxLength     *int64
	MaxProperties *int64
	Minimum       *float64
	Maximum       *float64
	Pattern       string
	Enum          []any
	Default       any

	// Reference info
	RefName     string // If this is a $ref, the referenced schema name
	IsReference bool   // True if this property references another object by ID

	// Nested types
	Items                *Property   // For array types
	Properties           []*Property // For object types
	AdditionalProperties *Property   // For map types

	// Union types (oneOf / anyOf)
	OneOf []*Property // For oneOf types - each represents a variant
	AnyOf []*Property // For anyOf types - each represents a variant

	// Discriminator info (OAS discriminator object, or derived from variant type consts)
	Discriminator        string            // discriminator.propertyName (e.g. "type")
	DiscriminatorMapping map[string]string // discriminator value → ref name (e.g. "anonymous" → "VirtualClusterAuthenticationAnonymous")
}

// Schema represents a parsed OpenAPI schema.
type Schema struct {
	Name         string
	SourcePath   string // The OpenAPI path this schema was extracted from
	Description  string
	Type         string // The schema's type (string, boolean, integer, number, array, object)
	Format       string // The schema's format (url, uri, uuid, etc.)
	Properties   []*Property
	Required     []string
	Dependencies []*Dependency // Parent resource dependencies from path parameters
	OneOf        []*Property   // Root-level oneOf variants (for union type schemas)
	AnyOf        []*Property   // Root-level anyOf variants (for union type schemas without discriminator)
	Items        *Property     // For array-type schemas, the items type

	// Discriminator info from the OAS discriminator object.
	Discriminator        string            // discriminator.propertyName (e.g. "type")
	DiscriminatorMapping map[string]string // discriminator value → ref name

	// Map type support
	AdditionalProperties *Property // For object types with additionalProperties (map value schema)
	MaxProperties        *int64    // Maximum number of map entries

	// POST operation hints used by downstream generators (e.g. Konnect SDK ops).
	OperationID          string   // POST operationId (e.g. "create-portal")
	Tags                 []string // POST tags (e.g. ["portals"])
	SuccessResponseRef   string   // ref name of the 2xx success response schema (e.g. "Portal", "EventGatewayInfo")
	RespIDIsPointer      bool     // true when the 2xx response schema's "id" field is not in required (i.e. *string in SDK codegen)
	CreateReqBodyPointer bool     // true when POST requestBody is not marked required (SDK emits pointer)

	// PATCH/PUT operation hints for update ops generation.
	UpdateOperationID        string   // PATCH/PUT operationId (e.g. "update-portal")
	UpdateTags               []string // PATCH/PUT tags (e.g. ["Portals"])
	UpdateSuccessResponseRef string   // ref name of the 2xx success response schema for update
	UpdateRespIDIsPointer    bool     // true when the update 2xx response schema's "id" is not required
	UpdatePathParams         []string // ordered path params from the PATCH/PUT path (e.g. ["portalId","id"])
	UpdateReqBodyPointer     bool     // true when PATCH/PUT requestBody is not marked required (SDK emits pointer)

	// DELETE operation hints for delete ops generation.
	DeleteOperationID     string   // DELETE operationId (e.g. "delete-portal")
	DeleteTags            []string // DELETE tags (e.g. ["Portals"])
	DeletePathParams      []string // ordered path params from the DELETE path (e.g. ["portalId","id"])
	DeleteQueryParamCount int      // number of query parameters on the DELETE operation (each becomes a nil arg)

	// GET operation hints for getForUID ops generation.
	ListOperationID        string   // GET operationId on the collection path (e.g. "list-portals")
	ListTags               []string // GET tags (used to derive SDK interface name)
	ListPathParams         []string // ordered path params from the GET collection path (parent params only)
	ListSuccessResponseRef string   // ref name of the 2xx success response for the list op (e.g. "ListBackendClustersResponse"); names the nested field on the SDK operations response wrapper
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

// Parser parses OpenAPI specs.
type Parser struct {
	doc     *openapi3.T
	visited map[string]bool // Track visited schemas to prevent infinite recursion
}

// NewParser creates a new parser.
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

	var operation *openapi3.Operation
	// We're interested in POST or alternatively PUT operations (create operations)
	switch {
	case pathItem.Post != nil:
		operation = pathItem.Post
	case pathItem.Put != nil:
		operation = pathItem.Put
	default:
		return "", nil, fmt.Errorf("path %s does not have a POST or PUT operation", targetPath)
	}

	// Get request body
	if operation.RequestBody == nil || operation.RequestBody.Value == nil {
		return "", nil, fmt.Errorf("path %s POST operation has no request body", targetPath)
	}

	reqBody := operation.RequestBody.Value
	if reqBody.Content == nil {
		return "", nil, fmt.Errorf("path %s POST request body has no content", targetPath)
	}

	// Find the schema name: prefer request body ref, then path derivation
	var schemaName string
	if operation.RequestBody.Ref != "" {
		schemaName = extractRefName(operation.RequestBody.Ref)
	} else {
		schemaName = deriveSchemaNameFromPath(targetPath, operation.OperationID)
	}

	// Extract path parameters as dependencies
	dependencies := p.extractPathDependencies(targetPath)

	// Parse the first media type that has a valid schema
	for _, mediaTypeObj := range reqBody.Content {
		if mediaTypeObj.Schema == nil || mediaTypeObj.Schema.Value == nil {
			continue
		}

		schema := p.parseSchema(schemaName, mediaTypeObj.Schema.Value)
		schema.SourcePath = targetPath
		schema.Dependencies = dependencies
		schema.OperationID = operation.OperationID
		schema.Tags = append([]string(nil), operation.Tags...)
		schema.SuccessResponseRef = extractSuccessResponseRef(operation.Responses)
		schema.RespIDIsPointer = p.successResponseIDIsPointer(operation.Responses)
		schema.CreateReqBodyPointer = !reqBody.Required
		p.extractUpdateOp(targetPath, schema)
		p.extractDeleteOp(targetPath, schema)
		p.extractListOp(targetPath, schema)
		return schemaName, schema, nil
	}

	return "", nil, fmt.Errorf("path %s POST request body has no valid schema", targetPath)
}

// extractUpdateOp finds the PATCH (or PUT fallback) operation for the entity —
// either on the same path or on a sibling path that extends targetPath with one
// `{<id>}` segment — and populates the Update* fields of the schema.
func (p *Parser) extractUpdateOp(targetPath string, schema *Schema) {
	updateOp, updatePath := p.findUpdateOperation(targetPath)
	if updateOp == nil {
		return
	}

	schema.UpdateOperationID = updateOp.OperationID
	schema.UpdateTags = append([]string(nil), updateOp.Tags...)
	schema.UpdateSuccessResponseRef = extractSuccessResponseRef(updateOp.Responses)
	schema.UpdateRespIDIsPointer = p.successResponseIDIsPointer(updateOp.Responses)
	schema.UpdatePathParams = extractPathParams(updatePath)
	if updateOp.RequestBody != nil && updateOp.RequestBody.Value != nil {
		schema.UpdateReqBodyPointer = !updateOp.RequestBody.Value.Required
	}
}

// findUpdateOperation returns the PUT (fallback PATCH) operation and its path
// for the given POST targetPath. It checks the same path first, then looks for
// a sibling path that extends targetPath with a single `{<param>}` segment.
func (p *Parser) findUpdateOperation(targetPath string) (*openapi3.Operation, string) {
	// Same path.
	if item := p.doc.Paths.Find(targetPath); item != nil {
		if item.Put != nil {
			return item.Put, targetPath
		}
		if item.Patch != nil {
			return item.Patch, targetPath
		}
	}

	// Sibling: <targetPath>/{<anything>} with exactly one additional segment.
	for pathKey, item := range p.doc.Paths.Map() {
		if !strings.HasPrefix(pathKey, targetPath+"/") {
			continue
		}
		rest := pathKey[len(targetPath)+1:]
		// Must be a single {param} segment — no nested slashes.
		if strings.HasPrefix(rest, "{") && strings.HasSuffix(rest, "}") && !strings.Contains(rest, "/") {
			if item.Put != nil {
				return item.Put, pathKey
			}
			if item.Patch != nil {
				return item.Patch, pathKey
			}
		}
	}
	return nil, ""
}

// extractDeleteOp finds the DELETE operation for the entity — either on the
// same path or on a sibling path that extends targetPath with one `{<id>}`
// segment — and populates the Delete* fields of the schema.
func (p *Parser) extractDeleteOp(targetPath string, schema *Schema) {
	deleteOp, deletePath := p.findDeleteOperation(targetPath)
	if deleteOp == nil {
		return
	}

	schema.DeleteOperationID = deleteOp.OperationID
	schema.DeleteTags = append([]string(nil), deleteOp.Tags...)
	schema.DeletePathParams = extractPathParams(deletePath)

	// Count query parameters so the generator can pass nil for each optional
	// query param that the SDK codegen promotes to a positional argument.
	for _, p := range deleteOp.Parameters {
		if p.Value != nil && p.Value.In == "query" {
			schema.DeleteQueryParamCount++
		}
	}
}

// findDeleteOperation returns the DELETE operation and its path for the given
// POST targetPath. It checks the same path first, then looks for a sibling
// path that extends targetPath with a single `{<param>}` segment.
func (p *Parser) findDeleteOperation(targetPath string) (*openapi3.Operation, string) {
	// Same path.
	if item := p.doc.Paths.Find(targetPath); item != nil {
		if item.Delete != nil {
			return item.Delete, targetPath
		}
	}

	// Sibling: <targetPath>/{<anything>} with exactly one additional segment.
	for pathKey, item := range p.doc.Paths.Map() {
		if !strings.HasPrefix(pathKey, targetPath+"/") {
			continue
		}
		rest := pathKey[len(targetPath)+1:]
		if strings.HasPrefix(rest, "{") && strings.HasSuffix(rest, "}") && !strings.Contains(rest, "/") {
			if item.Delete != nil {
				return item.Delete, pathKey
			}
		}
	}
	return nil, ""
}

// extractListOp finds the GET operation for the entity collection — i.e. GET
// on the same targetPath as the POST — and populates the List* fields of the
// schema.
func (p *Parser) extractListOp(targetPath string, schema *Schema) {
	listOp := p.findListOperation(targetPath)
	if listOp == nil {
		return
	}

	schema.ListOperationID = listOp.OperationID
	schema.ListTags = append([]string(nil), listOp.Tags...)
	schema.ListPathParams = extractPathParams(targetPath)
	schema.ListSuccessResponseRef = extractSuccessResponseRef(listOp.Responses)
}

// findListOperation returns the GET operation on targetPath (the collection
// endpoint), or nil if none exists.
func (p *Parser) findListOperation(targetPath string) *openapi3.Operation {
	item := p.doc.Paths.Find(targetPath)
	if item == nil {
		return nil
	}
	return item.Get
}

// extractPathParams returns the ordered list of path parameter names (without
// braces) from a URL path, e.g. "/v3/portals/{portalId}/teams/{id}" →
// ["portalId","id"].
func extractPathParams(path string) []string {
	paramRegex := regexp.MustCompile(`\{([^}]+)\}`)
	matches := paramRegex.FindAllStringSubmatch(path, -1)
	params := make([]string, 0, len(matches))
	for _, m := range matches {
		params = append(params, m[1])
	}
	return params
}

// extractPathDependencies extracts parent resource dependencies from path parameters.
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
		entityName := getEntityNameFromParam(paramName)
		dep := &Dependency{
			ParamName:          paramName,
			EntityName:         entityName,
			AccessorEntityName: getAccessorEntityNameFromPath(path, paramName),
			FieldName:          entityName + "Ref",
			JSONName:           strings.ToLower(entityName[:1]) + entityName[1:] + "Ref",
		}
		deps = append(deps, dep)
	}

	return deps
}

// getEntityNameFromParam converts a path parameter name to an entity name
// e.g., "portalId" -> "Portal", "teamId" -> "Team".
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

func getAccessorEntityNameFromPath(path, paramName string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	target := "{" + paramName + "}"
	for i, segment := range segments {
		if segment != target {
			continue
		}
		if i == 0 {
			break
		}
		if name := getEntityNameFromResourceSegment(segments[i-1]); name != "" {
			return name
		}
		break
	}
	return getEntityNameFromParam(paramName)
}

func getEntityNameFromResourceSegment(segment string) string {
	if segment == "" {
		return ""
	}

	parts := strings.Split(segment, "-")
	var result strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		word := singularizeResourceWord(part)
		result.WriteString(toTitleCase(word))
	}

	return result.String()
}

func singularizeResourceWord(word string) string {
	switch {
	case strings.HasSuffix(word, "ies") && len(word) > 3:
		return strings.TrimSuffix(word, "ies") + "y"
	case strings.HasSuffix(word, "sses") && len(word) > 4:
		return strings.TrimSuffix(word, "es")
	case strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss"):
		return strings.TrimSuffix(word, "s")
	default:
		return word
	}
}

// toSnakeCase converts a string to snake_case.
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

// toTitleCase converts the first character of a string to uppercase.
func toTitleCase(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// deriveSchemaNameFromPath derives a schema name from the path and operation ID.
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

// collectReferencedSchemas collects all schema names referenced by the schema's properties.
func (p *Parser) collectReferencedSchemas(schema *Schema, refs map[string]bool) {
	for _, prop := range schema.Properties {
		p.collectRefsFromProperty(prop, refs)
	}
	// Also collect refs from root-level oneOf variants
	for _, variant := range schema.OneOf {
		p.collectRefsFromProperty(variant, refs)
	}
	// Also collect refs from root-level anyOf variants
	for _, variant := range schema.AnyOf {
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
	// Collect refs from anyOf variants
	for _, variant := range prop.AnyOf {
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

	// Handle additionalProperties (map value schema)
	if schemaValue.AdditionalProperties.Schema != nil {
		schema.AdditionalProperties = ParseProperty("value", schemaValue.AdditionalProperties.Schema, 0, p.visited)
	}

	// Handle maxProperties
	if schemaValue.MaxProps != nil {
		maxProps := int64(*schemaValue.MaxProps)
		schema.MaxProperties = &maxProps
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

	// Handle root-level anyOf (union type schemas without discriminator)
	if len(schemaValue.AnyOf) > 0 {
		for _, anyOfRef := range schemaValue.AnyOf {
			variantName := fmt.Sprintf("variant%d", len(schema.AnyOf))
			if anyOfRef.Ref != "" {
				variantName = extractRefName(anyOfRef.Ref)
			}
			variantProp := ParseProperty(variantName, anyOfRef, 0, p.visited)
			schema.AnyOf = append(schema.AnyOf, variantProp)
		}
	}

	// Capture discriminator info from the OAS discriminator object.
	if schemaValue.Discriminator != nil {
		schema.Discriminator = schemaValue.Discriminator.PropertyName
		if len(schemaValue.Discriminator.Mapping) > 0 {
			schema.DiscriminatorMapping = make(map[string]string, len(schemaValue.Discriminator.Mapping))
			for value, mappingRef := range schemaValue.Discriminator.Mapping {
				schema.DiscriminatorMapping[value] = extractRefName(mappingRef.Ref)
			}
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

// getSchemaType extracts the type from a schema, handling OpenAPI 3.1 type arrays.
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

// successResponseIDIsPointer reports whether the 2xx response schema's "id"
// field is absent from the schema's required array, which causes SDK codegen to
// emit it as *string rather than string. Returns false when "id" is required or
// when the response schema cannot be resolved.
func (p *Parser) successResponseIDIsPointer(responses *openapi3.Responses) bool {
	if responses == nil {
		return false
	}
	for _, code := range []string{"201", "200"} {
		respRef := responses.Value(code)
		if respRef == nil || respRef.Value == nil {
			continue
		}
		for _, mt := range respRef.Value.Content {
			if mt == nil || mt.Schema == nil {
				continue
			}
			schemaVal := mt.Schema.Value
			if mt.Schema.Ref != "" {
				refName := extractRefName(mt.Schema.Ref)
				if s, ok := p.doc.Components.Schemas[refName]; ok && s != nil && s.Value != nil {
					schemaVal = s.Value
				}
			}
			if schemaVal == nil {
				continue
			}
			if slices.Contains(schemaVal.Required, "id") {
				return false
			}
			// "id" field exists but is not required → SDK emits *string
			if _, ok := schemaVal.Properties["id"]; ok {
				return true
			}
			return false
		}
	}
	return false
}

// extractSuccessResponseRef returns the ref name of the 2xx success response
// schema (preferring 201, then 200) for a POST operation, or "" if none is set.
// It handles both response-level $refs (e.g. #/components/responses/Foo) and
// nested content-schema $refs (e.g. #/components/schemas/Foo). When both are
// present the content-schema ref is preferred because it names the actual
// payload type produced by SDK codegen.
func extractSuccessResponseRef(responses *openapi3.Responses) string {
	if responses == nil {
		return ""
	}
	for _, code := range []string{"201", "200"} {
		respRef := responses.Value(code)
		if respRef == nil {
			continue
		}
		if respRef.Value != nil {
			for _, mt := range respRef.Value.Content {
				if mt == nil || mt.Schema == nil {
					continue
				}
				if mt.Schema.Ref != "" {
					return extractRefName(mt.Schema.Ref)
				}
			}
		}
		if respRef.Ref != "" {
			return extractRefName(respRef.Ref)
		}
	}
	return ""
}

// extractRefName extracts the schema name from a $ref string.
func extractRefName(ref string) string {
	// #/components/schemas/SomeSchema -> SomeSchema
	parts := strings.Split(ref, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ref
}

// isReferenceProperty checks if a property is a reference to another entity.
func isReferenceProperty(name string, schema *openapi3.Schema) bool {
	// Check if the property name ends with _id and has uuid format
	if strings.HasSuffix(name, "_id") && schema.Format == "uuid" {
		return true
	}
	return false
}

// GetEntityNameFromType extracts the entity name from a type name
// e.g., "CreatePortal" -> "Portal", "PortalCreateTeam" -> "PortalTeam".
func GetEntityNameFromType(name string) string {
	// Remove common prefixes
	result := name
	for _, prefix := range []string{"Add", "Create", "Update", "Delete", "Get", "List"} {
		result = strings.TrimPrefix(result, prefix)
	}

	// Also handle infix patterns like "PortalCreateTeam" -> "PortalTeam"
	for _, infix := range []string{"Create", "Update", "Delete"} {
		result = strings.ReplaceAll(result, infix, "")
	}

	return result
}

// GetRefEntityName extracts the entity name from a reference property name
// e.g., "default_application_auth_strategy_id" -> "ApplicationAuthStrategy".
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
