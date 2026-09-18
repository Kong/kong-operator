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
	} else if prop.Type == "object" && len(schemaValue.AllOf) > 1 {
		// Composite allOf (e.g. a base schema $ref combined with an anyOf of
		// matcher variants): flatten the members' properties so the schema
		// generates a plain struct instead of degrading to map[string]string.
		prop.Properties = flattenAllOfProperties(schemaValue, depth+1, visited)
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

// flattenAllOfProperties flattens a composite allOf schema (e.g. a base schema
// $ref combined with an anyOf of matcher variants) into a single property list,
// so composite schemas generate plain structs instead of degrading to
// map[string]string.
//
// Members are processed in declaration order and properties dedupe by name with
// the first declaration winning (base members carry the full descriptions).
// A property is required only if it is required in every allOf member that
// declares it; within an anyOf/oneOf member it must additionally be declared
// and required in every variant.
func flattenAllOfProperties(schemaValue *openapi3.Schema, depth int, visited map[string]bool) []*Property {
	if depth > 10 {
		return nil
	}

	var (
		props []*Property
		seen  = map[string]int{} // property name -> index into props
	)
	// addProp keeps the first declaration of a property and narrows
	// required-ness on later declarations.
	addProp := func(prop *Property, required bool) {
		if prop == nil {
			return
		}
		if idx, ok := seen[prop.Name]; ok {
			if !required {
				props[idx].Required = false
			}
			return
		}
		prop.Required = required
		seen[prop.Name] = len(props)
		props = append(props, prop)
	}

	for _, member := range schemaValue.AllOf {
		memberValue := member.Value
		if memberValue == nil {
			continue
		}
		switch {
		case len(memberValue.Properties) > 0:
			for name, nestedRef := range memberValue.Properties {
				addProp(ParseProperty(name, nestedRef, depth+1, visited), slices.Contains(memberValue.Required, name))
			}
		case len(memberValue.AllOf) > 0:
			for _, nested := range flattenAllOfProperties(memberValue, depth+1, visited) {
				addProp(nested, nested.Required)
			}
		case len(memberValue.AnyOf) > 0 || len(memberValue.OneOf) > 0:
			variants := memberValue.AnyOf
			if len(variants) == 0 {
				variants = memberValue.OneOf
			}
			parsed := make([][]*Property, 0, len(variants))
			for _, variantRef := range variants {
				if variantRef.Value == nil {
					continue
				}
				variant := ParseProperty("variant", variantRef, depth+1, visited)
				if len(variant.Properties) == 0 && len(variantRef.Value.AllOf) > 0 {
					variant.Properties = flattenAllOfProperties(variantRef.Value, depth+1, visited)
				}
				parsed = append(parsed, variant.Properties)
			}
			// A property is required only if declared and required in every
			// variant.
			declCount := map[string]int{}
			reqCount := map[string]int{}
			for _, variantProps := range parsed {
				for _, vp := range variantProps {
					declCount[vp.Name]++
					if vp.Required {
						reqCount[vp.Name]++
					}
				}
			}
			requiredByName := map[string]bool{}
			for name, count := range declCount {
				requiredByName[name] = count == len(parsed) && reqCount[name] == len(parsed)
			}
			for _, variantProps := range parsed {
				for _, vp := range variantProps {
					addProp(vp, requiredByName[vp.Name])
				}
			}
		}
	}

	sort.Slice(props, func(i, j int) bool {
		return props[i].Name < props[j].Name
	})
	return props
}
