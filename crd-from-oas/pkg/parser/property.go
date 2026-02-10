package parser

import (
	"slices"
	"sort"

	"github.com/getkin/kin-openapi/openapi3"
)

// ParseProperty parses an OpenAPI schema reference into a Property struct.
// It handles nested objects, arrays, maps, and tracks visited schemas to prevent cycles.
// The depth parameter limits recursion to prevent infinite loops.
func ParseProperty(name string, schemaRef *openapi3.SchemaRef, depth int, visited map[string]bool) *Property {
	prop := &Property{
		Name: name,
	}

	// Prevent infinite recursion with a depth limit
	if depth > 10 {
		return prop
	}

	// Handle $ref - check for cycles
	if schemaRef.Ref != "" {
		refName := extractRefName(schemaRef.Ref)
		prop.RefName = refName

		// Don't recurse into already visited schemas
		if visited[refName] {
			return prop
		}
	}

	schemaValue := schemaRef.Value
	if schemaValue == nil {
		return prop
	}

	// Basic type info
	prop.Type = getSchemaType(schemaValue)
	prop.Format = schemaValue.Format
	prop.Description = schemaValue.Description
	prop.Nullable = schemaValue.Nullable
	prop.ReadOnly = schemaValue.ReadOnly

	// Check if this is a reference to another entity (ends with _id and has uuid format)
	prop.IsReference = isReferenceProperty(name, schemaValue)

	// Validations
	if schemaValue.MinLength > 0 {
		minLen := int64(schemaValue.MinLength)
		prop.MinLength = &minLen
	}
	if schemaValue.MaxLength != nil {
		maxLen := int64(*schemaValue.MaxLength)
		prop.MaxLength = &maxLen
	}
	if schemaValue.Min != nil {
		prop.Minimum = schemaValue.Min
	}
	if schemaValue.Max != nil {
		prop.Maximum = schemaValue.Max
	}
	if schemaValue.Pattern != "" {
		prop.Pattern = schemaValue.Pattern
	}
	if len(schemaValue.Enum) > 0 {
		prop.Enum = schemaValue.Enum
	}
	if schemaValue.Default != nil {
		prop.Default = schemaValue.Default
	}

	// Handle array types
	if prop.Type == "array" && schemaValue.Items != nil {
		prop.Items = ParseProperty("items", schemaValue.Items, depth+1, visited)
	}

	// Handle nested object types
	if prop.Type == "object" && len(schemaValue.Properties) > 0 {
		for nestedName, nestedRef := range schemaValue.Properties {
			nestedProp := ParseProperty(nestedName, nestedRef, depth+1, visited)
			nestedProp.Required = slices.Contains(schemaValue.Required, nestedName)
			prop.Properties = append(prop.Properties, nestedProp)
		}
		sort.Slice(prop.Properties, func(i, j int) bool {
			return prop.Properties[i].Name < prop.Properties[j].Name
		})
	}

	// Handle additionalProperties (map types)
	if schemaValue.AdditionalProperties.Schema != nil {
		prop.AdditionalProperties = ParseProperty("value", schemaValue.AdditionalProperties.Schema, depth+1, visited)
	}

	// Handle oneOf (union types)
	if len(schemaValue.OneOf) > 0 {
		for _, oneOfRef := range schemaValue.OneOf {
			// Extract the name from the $ref if available
			variantName := "Variant"
			if oneOfRef.Ref != "" {
				variantName = extractRefName(oneOfRef.Ref)
			}
			variantProp := ParseProperty(variantName, oneOfRef, depth+1, visited)
			prop.OneOf = append(prop.OneOf, variantProp)
		}
	}

	return prop
}
