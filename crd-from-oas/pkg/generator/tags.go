package generator

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
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
			// Always quote string defaults so empty strings produce `=""` not `=`.
			tags = append(tags, markerDefaultString(strconv.Quote(v)))
		case float64:
			tags = append(tags, markerDefaultString(formatNumericDefault(v)))
		case int:
			tags = append(tags, markerDefaultString(strconv.Itoa(v)))
		case int64:
			tags = append(tags, markerDefaultString(strconv.FormatInt(v, 10)))
		case json.Number:
			tags = append(tags, markerDefaultString(v.String()))
		case []any:
			tags = append(tags, markerDefaultString(formatArrayDefaultValue(v)))
		case map[string]any:
			b, err := json.Marshal(v)
			if err != nil {
				panic("unsupported default value type: " + fmt.Sprintf("%T", v))
			}
			tags = append(tags, markerDefaultString(string(b)))
		default:
			panic("unsupported default value type: " + fmt.Sprintf("%T", v))
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

func formatArrayDefaultValue(values []any) string {
	formatted := make([]string, 0, len(values))
	for _, value := range values {
		switch v := value.(type) {
		case string:
			formatted = append(formatted, strconv.Quote(v))
		case bool:
			formatted = append(formatted, strconv.FormatBool(v))
		case float64:
			formatted = append(formatted, formatNumericDefault(v))
		case int:
			formatted = append(formatted, strconv.Itoa(v))
		case int64:
			formatted = append(formatted, strconv.FormatInt(v, 10))
		case json.Number:
			formatted = append(formatted, v.String())
		default:
			panic("unsupported array default item type: " + fmt.Sprintf("%T", v))
		}
	}
	return "{" + strings.Join(formatted, ",") + "}"
}

func formatNumericDefault(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
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
