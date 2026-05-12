package generator

import (
	"slices"
	"strings"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// defaultMaxLength is the fallback MaxLength applied to string fields when
// the OpenAPI spec does not declare an explicit maxLength constraint.
const defaultMaxLength = 253

// KubebuilderTags generates kubebuilder validation tags for a property.
// It takes an optional fieldConfig for custom validations.
func KubebuilderTags(prop *parser.Property, entityName string, fieldConfig *config.Config) []string {
	var tags []string

	// Required validation
	if prop.Required && !prop.Nullable {
		tags = append(tags, markerRequired())
	} else {
		tags = append(tags, markerOptional())
	}

	// String validations (skip for reference properties which become ObjectRef)
	if prop.Type == "string" && !prop.IsReference {
		if prop.MinLength != nil {
			tags = append(tags, markerValidationMinLength(int(*prop.MinLength)))
		} else if prop.Required && !prop.Nullable {
			// Add MinLength=1 for required strings without explicit minLength
			tags = append(tags, markerValidationMinLength(1))
		}
		if prop.MaxLength != nil {
			tags = append(tags, markerValidationMaxLength(int(*prop.MaxLength)))
		} else {
			// Add default MaxLength for strings without explicit maxLength
			tags = append(tags, markerValidationMaxLength(defaultMaxLength))
		}
		if prop.Pattern != "" {
			tags = append(tags, markerValidationPattern(prop.Pattern))
		}
	}

	// Numeric validations
	if prop.Minimum != nil {
		tags = append(tags, markerValidationMinimum(*prop.Minimum))
	}
	if prop.Maximum != nil {
		tags = append(tags, markerValidationMaximum(*prop.Maximum))
	}

	// Boolean-to-string enum validation for inline boolean properties (no RefName).
	// Type-aliased booleans get their Enum marker on the type definition instead.
	if prop.Type == "boolean" && prop.RefName == "" {
		tags = append(tags, markerValidationEnum("Enabled;Disabled"))
	}

	// Enum validation
	if len(prop.Enum) > 0 {
		var enumValues []string
		for _, e := range prop.Enum {
			if s, ok := e.(string); ok {
				enumValues = append(enumValues, s)
			}
		}
		if len(enumValues) > 0 {
			tags = append(tags, markerValidationEnum(strings.Join(enumValues, ";")))
		}
	}

	// Map MaxProperties constraint (applies to both ref and inline map types)
	if prop.MaxProperties != nil {
		tags = append(tags, markerValidationMaxProperties(int(*prop.MaxProperties)))
	}

	// Apply custom validations from config, overriding any auto-generated
	// marker that shares the same key (text before the first '=').
	if fieldConfig != nil {
		customValidations := fieldConfig.GetFieldValidations(entityName, prop.Name)
		if len(customValidations) > 0 {
			overrideKeys := make(map[string]struct{}, len(customValidations))
			for _, v := range customValidations {
				overrideKeys[markerKey(v)] = struct{}{}
			}
			tags = slices.DeleteFunc(tags, func(t string) bool {
				_, replaced := overrideKeys[markerKey(t)]
				return replaced
			})
			tags = append(tags, customValidations...)
		}
	}

	return tags
}

// markerKey returns the portion of a kubebuilder marker before the first '=',
// which acts as the unique key for override matching.
// For markers without '=' the whole string is the key (e.g. "+optional").
func markerKey(marker string) string {
	key, _, _ := strings.Cut(marker, "=")
	return key
}

// valueTypeMarkers generates kubebuilder validation markers for a map value type
// based on the additionalProperties constraints from the OpenAPI spec.
func valueTypeMarkers(ap *parser.Property) []string {
	var markers []string
	if ap.Type == "string" {
		if ap.MinLength != nil {
			markers = append(markers, markerValidationMinLength(int(*ap.MinLength)))
		}
		if ap.MaxLength != nil {
			markers = append(markers, markerValidationMaxLength(int(*ap.MaxLength)))
		}
		if ap.Pattern != "" {
			markers = append(markers, markerValidationPattern(ap.Pattern))
		}
	}
	return markers
}

// propertyToGoBaseType returns the Go base type for a simple property.
func propertyToGoBaseType(prop *parser.Property) string {
	switch prop.Type {
	case "string":
		return "string"
	case "integer":
		return "int"
	case "boolean":
		return "bool"
	case "number":
		return "float64"
	default:
		return "string"
	}
}
