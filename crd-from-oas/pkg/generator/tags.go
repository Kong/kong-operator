package generator

import (
	"fmt"
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

	// Default value
	if prop.Default != nil {
		switch v := prop.Default.(type) {
		case bool:
			// Bool defaults map to Enabled/Disabled to match the string enum.
			if v {
				tags = append(tags, markerDefaultString("Enabled"))
			} else {
				tags = append(tags, markerDefaultString("Disabled"))
			}
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
