package managedfields

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/structured-merge-diff/v4/typed"

	"github.com/kong/kong-operator/v2/pkg/generated"
)

// GetObjectType creates a ParseableType for the given object using the generated schema.
func GetObjectType(obj runtime.Object) (typed.ParseableType, error) {

	gvk := obj.GetObjectKind().GroupVersionKind()

	// Convert GVK to the schema name format used in generated schema.
	// Pattern: com.github.kong.kong-operator.api.<group>.<version>.<Kind>.
	schemaName, err := deriveSchemaName(gvk)
	if err != nil {
		return typed.ParseableType{}, fmt.Errorf("failed to derive schema name for %s: %w", gvk, err)
	}

	// Try to get the specific parseable type from the generated schema.
	if parseableType, exists := getParseableTypeByName(schemaName); exists {
		return parseableType, nil
	}

	// Return error if we can't find the specific type
	return typed.ParseableType{}, fmt.Errorf("schema type not found for %s (schema name: %s)", gvk, schemaName)
}

// deriveSchemaName converts a GroupVersionKind to the schema name format
func deriveSchemaName(gvk schema.GroupVersionKind) (string, error) {
	// Extract the main group name from the full group
	var groupName string

	// Handle specific Kong operator groups
	switch gvk.Group {
	case "configuration.konghq.com":
		groupName = "configuration"
	case "konnect.konghq.com":
		groupName = "konnect"
	case "gateway-operator.konghq.com":
		groupName = "gateway-operator"
	default:
		return "", fmt.Errorf("unsupported API group: %s", gvk.Group)
	}

	return fmt.Sprintf("com.github.kong.kong-operator.api.%s.%s.%s",
		groupName, gvk.Version, gvk.Kind), nil
}

// getParseableTypeByName attempts to get a specific ParseableType by schema name
func getParseableTypeByName(schemaName string) (typed.ParseableType, bool) {
	parser := generated.Parser()

	// Get the ParseableType - this always succeeds but may not be valid
	parseableType := parser.Type(schemaName)

	// Check if the type actually exists in the schema
	if !parseableType.IsValid() {
		return typed.ParseableType{}, false
	}

	return parseableType, true
}
