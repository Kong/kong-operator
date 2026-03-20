package generator

import (
	"fmt"
	"strings"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

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
			tags = append(tags, markerValidationMaxLength(256))
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

	// Default value
	if prop.Default != nil {
		switch v := prop.Default.(type) {
		case bool:
			tags = append(tags, markerDefaultBool(v))
		case string:
			tags = append(tags, markerDefaultString(v))
		default:
			panic("unsupported default value type: " + fmt.Sprintf("%T", v))
		}
	}

	// Map MaxProperties constraint (applies to both ref and inline map types)
	if prop.MaxProperties != nil {
		tags = append(tags, markerValidationMaxProperties(int(*prop.MaxProperties)))
	}

	// Add custom validations from config
	if fieldConfig != nil {
		customValidations := fieldConfig.GetFieldValidations(entityName, prop.Name)
		tags = append(tags, customValidations...)
	}

	return tags
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
