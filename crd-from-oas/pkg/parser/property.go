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
	prop.Title = schemaValue.Title
	prop.Description = schemaValue.Description
	prop.Nullable = schemaValue.Nullable
	prop.ReadOnly = schemaValue.ReadOnly

	// Check if this is a reference to another entity (ends with _id and has uuid format).
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
	} else if schemaValue.Const != nil {
		// OAS 3.1 const: treat as a single-element enum for kubebuilder validation.
		prop.Enum = []any{schemaValue.Const}
	}
	if schemaValue.Default != nil {
		prop.Default = schemaValue.Default
	}

	// Handle array types.
	if prop.Type == "array" {
		if schemaValue.MaxItems != nil {
			maxItems := int64(*schemaValue.MaxItems)
			prop.MaxItems = &maxItems
		}
		if schemaValue.Items != nil {
			prop.Items = ParseProperty("items", schemaValue.Items, depth+1, visited)
		}
	}

	// Handle nested object types.
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

	// Handle an object property composed from allOf instead of its own
	// properties (Kong's `x-flatten-allOf: true` convention: merge several
	// object schemas, typically a $ref plus inline additions, into one flat
	// object instead of nesting them). Also covers a type-less wrapper (no
	// `type: object` of its own, e.g. a bare `allOf` composing a base $ref
	// with a validation-only sibling): getSchemaType can't infer "object"
	// from allOf members, so without this the wrapper stays type-less and
	// falls through to the single-ref-plus-override handling below, which
	// only copies the ref's scalar constraints, not its properties. First
	// entry wins on name collisions, matching the schema-level merge in
	// parseSchema. Without this, the property has no properties and no
	// $ref, so it falls back to an untyped blob.
	if (prop.Type == "object" || prop.Type == "") && len(prop.Properties) == 0 && len(schemaValue.AllOf) > 0 {
		merged := make(map[string]*Property)
		var order []string
		for _, entry := range schemaValue.AllOf {
			v := entry.Value
			if v == nil {
				continue
			}
			if prop.Description == "" && entry.Ref != "" {
				prop.Description = v.Description
			}
			if len(v.Properties) == 0 {
				continue
			}
			for nestedName, nestedRef := range v.Properties {
				if _, exists := merged[nestedName]; exists {
					continue
				}
				order = append(order, nestedName)
				nestedProp := ParseProperty(nestedName, nestedRef, depth+1, visited)
				nestedProp.Required = slices.Contains(v.Required, nestedName)
				merged[nestedName] = nestedProp
			}
		}
		if len(order) > 0 {
			for _, nestedName := range order {
				prop.Properties = append(prop.Properties, merged[nestedName])
			}
			sort.Slice(prop.Properties, func(i, j int) bool {
				return prop.Properties[i].Name < prop.Properties[j].Name
			})
			prop.Type = "object"
		}
	}

	// Handle maxProperties (map size constraint)
	if schemaValue.MaxProps != nil {
		maxProps := int64(*schemaValue.MaxProps)
		prop.MaxProperties = &maxProps
	}

	// Handle additionalProperties (map types)
	if schemaValue.AdditionalProperties.Schema != nil {
		prop.AdditionalProperties = ParseProperty("value", schemaValue.AdditionalProperties.Schema, depth+1, visited)
	}

	// Handle allOf with a single $ref — the OAS "typed alias" pattern, where
	// a property wraps a named schema in allOf to attach extra constraints.
	// Treat it as a direct $ref so the generator emits the named type rather than
	// falling back to `any`.
	if len(schemaValue.AllOf) == 1 && prop.RefName == "" {
		if entry := schemaValue.AllOf[0]; entry.Ref != "" {
			prop.RefName = extractRefName(entry.Ref)
			if v := entry.Value; v != nil {
				if prop.Type == "" {
					prop.Type = getSchemaType(v)
				}
				if prop.MinLength == nil && v.MinLength > 0 {
					minLen := int64(v.MinLength)
					prop.MinLength = &minLen
				}
				if prop.MaxLength == nil && v.MaxLength != nil {
					maxLen := int64(*v.MaxLength)
					prop.MaxLength = &maxLen
				}
				if prop.Pattern == "" && v.Pattern != "" {
					prop.Pattern = v.Pattern
				}
			}
		}
	}

	// Handle allOf combining a single $ref with sibling override-only
	// fragments (e.g. `allOf: [{$ref: X}, {default: Y}]`, used to attach a
	// default to a shared enum without redeclaring it). Unlike the
	// single-entry case above, inline the referenced schema's type/enum/
	// constraints directly onto this property instead of pointing at the
	// named type: the sibling fragment only tweaks this one usage, it
	// doesn't turn every use of the shape into a shared type. Without this,
	// the property resolves to neither a type nor a $ref and falls back to
	// `any`.
	if len(schemaValue.AllOf) > 1 && prop.RefName == "" && prop.Type == "" {
		var refEntry *openapi3.SchemaRef
		refCount := 0
		for _, entry := range schemaValue.AllOf {
			if entry.Ref != "" {
				refCount++
				refEntry = entry
			}
		}
		if refCount == 1 && refEntry != nil && refEntry.Value != nil {
			v := refEntry.Value
			prop.Type = getSchemaType(v)
			if prop.Description == "" {
				prop.Description = v.Description
			}
			if len(prop.Enum) == 0 && len(v.Enum) > 0 {
				prop.Enum = v.Enum
			}
			if prop.MinLength == nil && v.MinLength > 0 {
				minLen := int64(v.MinLength)
				prop.MinLength = &minLen
			}
			if prop.MaxLength == nil && v.MaxLength != nil {
				maxLen := int64(*v.MaxLength)
				prop.MaxLength = &maxLen
			}
			if prop.Pattern == "" && v.Pattern != "" {
				prop.Pattern = v.Pattern
			}
			if prop.Default == nil {
				for _, entry := range schemaValue.AllOf {
					if entry.Ref != "" || entry.Value == nil {
						continue
					}
					if entry.Value.Default != nil {
						prop.Default = entry.Value.Default
						break
					}
				}
			}
		}
	}

	// Handle oneOf (union types).
	if len(schemaValue.OneOf) > 0 {
		for _, oneOfRef := range schemaValue.OneOf {
			// Extract the name from the $ref if available.
			variantName := "Variant"
			if oneOfRef.Ref != "" {
				variantName = extractRefName(oneOfRef.Ref)
			}
			variantProp := ParseProperty(variantName, oneOfRef, depth+1, visited)
			prop.OneOf = append(prop.OneOf, variantProp)
		}
	}

	// Handle anyOf (union types without discriminator).
	if len(schemaValue.AnyOf) > 0 {
		for _, anyOfRef := range schemaValue.AnyOf {
			variantName := "Variant"
			if anyOfRef.Ref != "" {
				variantName = extractRefName(anyOfRef.Ref)
			}
			variantProp := ParseProperty(variantName, anyOfRef, depth+1, visited)
			prop.AnyOf = append(prop.AnyOf, variantProp)
		}
	}

	// Capture discriminator info.
	if schemaValue.Discriminator != nil {
		prop.Discriminator = schemaValue.Discriminator.PropertyName
		if len(schemaValue.Discriminator.Mapping) > 0 {
			prop.DiscriminatorMapping = make(map[string]string, len(schemaValue.Discriminator.Mapping))
			for value, mappingRef := range schemaValue.Discriminator.Mapping {
				prop.DiscriminatorMapping[value] = extractRefName(mappingRef.Ref)
			}
		}
	}

	return prop
}
