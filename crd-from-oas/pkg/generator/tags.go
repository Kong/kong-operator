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

	// String validations
	if prop.Type == "string" {
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

	// Add custom validations from config
	if fieldConfig != nil {
		customValidations := fieldConfig.GetFieldValidations(entityName, prop.Name)
		tags = append(tags, customValidations...)
	}

	return tags
}
