package generator

import (
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// Config holds generator configuration
type Config struct {
	// API group for CRDs.
	APIGroup string
	// API version.
	APIVersion string
	// Whether to generate status subresource
	GenerateStatus bool
	// FieldConfig holds additional field configurations from YAML
	FieldConfig *config.Config
}

// Generator generates Go CRD types from parsed OpenAPI schemas
type Generator struct {
	config Config
}

// NewGenerator creates a new generator
func NewGenerator(config Config) *Generator {
	return &Generator{config: config}
}

// GeneratedFile represents a generated Go file
type GeneratedFile struct {
	Name    string
	Content string
}

// Generate generates Go CRD types from parsed schemas
func (g *Generator) Generate(parsed *parser.ParsedSpec) ([]GeneratedFile, error) {
	var files []GeneratedFile

	// Collect all referenced schema names from the CRD types
	referencedSchemas := make(map[string]bool)

	// Generate types for each request body (these are the main CRD types)
	for name, schema := range parsed.RequestBodies {
		content, err := g.generateCRDType(name, schema)
		if err != nil {
			return nil, fmt.Errorf("failed to generate type for %s: %w", name, err)
		}

		entityName := parser.GetEntityNameFromType(name)
		fileName := strings.ToLower(entityName) + "_types.go"
		files = append(files, GeneratedFile{
			Name:    fileName,
			Content: content,
		})

		// Collect referenced schemas
		g.collectReferencedSchemas(schema, referencedSchemas)
	}

	// Generate a register file
	registerContent, err := g.generateRegister(parsed)
	if err != nil {
		return nil, fmt.Errorf("failed to generate register file: %w", err)
	}
	files = append(files, GeneratedFile{
		Name:    "register.go",
		Content: registerContent,
	})

	// Generate a doc.go file
	docContent := g.generateDoc()
	files = append(files, GeneratedFile{
		Name:    "doc.go",
		Content: docContent,
	})

	// Generate common types (ObjectRef, etc.) including referenced schemas
	commonContent := g.generateCommonTypes()
	files = append(files, GeneratedFile{
		Name:    "common_types.go",
		Content: commonContent,
	})

	// Generate type aliases for referenced schemas
	if len(referencedSchemas) > 0 {
		schemaTypesContent := g.generateSchemaTypes(referencedSchemas, parsed)
		files = append(files, GeneratedFile{
			Name:    "schema_types.go",
			Content: schemaTypesContent,
		})
	}

	return files, nil
}

// collectReferencedSchemas collects all schema names referenced by properties
func (g *Generator) collectReferencedSchemas(schema *parser.Schema, refs map[string]bool) {
	for _, prop := range schema.Properties {
		g.collectRefsFromProperty(prop, refs)
	}
	// Also collect refs from root-level oneOf variants
	for _, variant := range schema.OneOf {
		g.collectRefsFromProperty(variant, refs)
	}
}

func (g *Generator) collectRefsFromProperty(prop *parser.Property, refs map[string]bool) {
	// Don't collect refs for properties that will be skipped
	if skipProperty(prop) {
		return
	}
	if prop.RefName != "" && !prop.IsReference {
		refs[prop.RefName] = true
	}
	if prop.Items != nil {
		g.collectRefsFromProperty(prop.Items, refs)
	}
	for _, nestedProp := range prop.Properties {
		g.collectRefsFromProperty(nestedProp, refs)
	}
	if prop.AdditionalProperties != nil {
		g.collectRefsFromProperty(prop.AdditionalProperties, refs)
	}
	// Collect refs from oneOf variants
	for _, variant := range prop.OneOf {
		if variant.RefName != "" {
			refs[variant.RefName] = true
		}
		g.collectRefsFromProperty(variant, refs)
	}
}

// generateSchemaTypes generates Go type definitions for referenced schemas
func (g *Generator) generateSchemaTypes(refs map[string]bool, parsed *parser.ParsedSpec) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "package %s\n\n", g.config.APIVersion)

	// Sort keys to ensure deterministic output order
	refNames := make([]string, 0, len(refs))
	for refName := range refs {
		refNames = append(refNames, refName)
	}
	sort.Strings(refNames)

	for _, refName := range refNames {
		if schema, ok := parsed.Schemas[refName]; ok {
			// Format the description as a proper comment
			comment := formatSchemaComment(refName, schema.Description)

			// Generate based on schema type
			if len(schema.Properties) > 0 {
				// It's an object type - generate a struct
				buf.WriteString(comment)
				fmt.Fprintf(&buf, "type %s struct {\n", refName)
				for _, prop := range schema.Properties {
					if skipProperty(prop) {
						continue
					}
					fmt.Fprintf(&buf, "\t%s %s `json:\"%s\"`\n", goFieldName(prop.Name), g.goType(prop), jsonTag(prop))
				}
				buf.WriteString("}\n\n")
			} else {
				// Generate based on the schema's actual type
				buf.WriteString(comment)
				goType := schemaToGoType(schema)
				fmt.Fprintf(&buf, "type %s %s\n\n", refName, goType)
			}
		} else {
			// Schema not found in parsed schemas, generate a placeholder
			fmt.Fprintf(&buf, "// %s is a referenced type (definition not found in spec)\n", refName)
			fmt.Fprintf(&buf, "type %s map[string]string\n\n", refName)
		}
	}

	return buf.String()
}

// schemaToGoType converts a parsed Schema's type info to the appropriate Go type string.
// This is used for referenced schemas that are simple types (not objects with properties).
func schemaToGoType(schema *parser.Schema) string {
	switch schema.Type {
	case "string":
		return "string"
	case "boolean":
		return "bool"
	case "integer":
		return "int"
	case "number":
		return "float64"
	case "array":
		if schema.Items != nil && schema.Items.Type != "" {
			switch schema.Items.Type {
			case "string":
				return "[]string"
			case "integer":
				return "[]int"
			case "boolean":
				return "[]bool"
			default:
				return "[]any"
			}
		}
		return "[]any"
	default:
		// For object types without properties or unknown types, default to map[string]string
		return "map[string]string"
	}
}

func (g *Generator) generateCRDType(name string, schema *parser.Schema) (string, error) {
	entityName := parser.GetEntityNameFromType(name)

	// Build a map of valid field names for validation
	validFields := make(map[string]bool)
	for _, prop := range schema.Properties {
		if !skipProperty(prop) {
			validFields[prop.Name] = true
		}
	}
	// Also include dependency fields
	for _, dep := range schema.Dependencies {
		validFields[dep.JSONName] = true
	}

	// Validate that all configured fields exist
	if g.config.FieldConfig != nil {
		if err := g.config.FieldConfig.ValidateAgainstSchema(entityName, validFields); err != nil {
			return "", err
		}
	}

	// Create a closure that captures entityName and fieldConfig for KubebuilderTags
	kubebuilderTagsWithConfig := func(prop *parser.Property) []string {
		return KubebuilderTags(prop, entityName, g.config.FieldConfig)
	}

	funcMap := template.FuncMap{
		"goType":          g.goType,
		"goFieldName":     goFieldName,
		"jsonTag":         jsonTag,
		"kubebuilderTags": kubebuilderTagsWithConfig,
		"isRefProperty":   isRefProperty,
		"refEntityName":   parser.GetRefEntityName,
		"skipProperty":    skipProperty,
		"lower":           strings.ToLower,
		"formatComment":   formatComment,
		"hasRootOneOf":    hasRootOneOf,
	}

	tmpl := template.Must(template.New("crd").Funcs(funcMap).Parse(crdTypeTemplate))

	var buf strings.Builder
	data := struct {
		EntityName      string
		Schema          *parser.Schema
		APIGroup        string
		APIVersion      string
		NeedsJSONImport bool
	}{
		EntityName:      entityName,
		Schema:          schema,
		APIGroup:        g.config.APIGroup,
		APIVersion:      g.config.APIVersion,
		NeedsJSONImport: schemaUsesJSON(g, schema),
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	// Generate union types for any oneOf properties
	unionTypes := g.generateUnionTypes(schema)
	if unionTypes != "" {
		buf.WriteString("\n")
		buf.WriteString(unionTypes)
	}

	// Post-process to remove trailing empty lines before closing braces in structs
	result := fixTrailingEmptyLines(buf.String())

	return result, nil
}

// generateUnionTypes generates Go union type structs for properties with oneOf
func (g *Generator) generateUnionTypes(schema *parser.Schema) string {
	var buf strings.Builder

	// Handle root-level oneOf (the schema itself is a union type)
	if len(schema.OneOf) > 0 {
		buf.WriteString(g.generateRootUnionType(schema))
	}

	// Handle property-level oneOf
	for _, prop := range schema.Properties {
		if skipProperty(prop) {
			continue
		}
		if len(prop.OneOf) > 0 {
			buf.WriteString(g.generateUnionType(prop))
		}
	}

	return buf.String()
}

// generateRootUnionType generates a union type for a schema with root-level oneOf
func (g *Generator) generateRootUnionType(schema *parser.Schema) string {
	// Create a synthetic property for the union type
	// Add "Config" suffix to differentiate from the main CRD type
	entityName := parser.GetEntityNameFromType(schema.Name)
	prop := &parser.Property{
		Name:  entityName + "Config",
		OneOf: schema.OneOf,
	}
	return g.generateUnionType(prop)
}

// generateUnionType generates a single union type struct
func (g *Generator) generateUnionType(prop *parser.Property) string {
	var buf strings.Builder

	typeName := goFieldName(prop.Name)

	// Collect raw variant names (ref names or variant names)
	var rawVariantNames []string
	for _, variant := range prop.OneOf {
		variantName := variant.Name
		if variant.RefName != "" {
			variantName = variant.RefName
		}
		rawVariantNames = append(rawVariantNames, variantName)
	}

	// Extract clean variant names by finding common prefix/suffix
	variantNames := extractVariantNames(rawVariantNames)

	// Generate the union type comment
	fmt.Fprintf(&buf, "// %s represents a union type for %s.\n", typeName, prop.Name)
	buf.WriteString("// Only one of the fields should be set based on the Type.\n")
	buf.WriteString("//\n")
	fmt.Fprintf(&buf, "type %s struct {\n", typeName)

	// Generate the Type discriminator field
	buf.WriteString("\t// Type designates the type of configuration.\n")
	buf.WriteString("\t//\n")
	buf.WriteString("\t// +required\n")
	buf.WriteString("\t// +kubebuilder:validation:MinLength=1\n")
	fmt.Fprintf(&buf, "\t// +kubebuilder:validation:Enum=%s\n", strings.Join(variantNames, ";"))
	fmt.Fprintf(&buf, "\tType %sType `json:\"type,omitempty\"`\n\n", typeName)

	// Generate a field for each variant
	for i, variant := range prop.OneOf {
		refTypeName := variant.Name
		if variant.RefName != "" {
			refTypeName = variant.RefName
		}

		fieldName := variantNames[i]

		// Generate JSON tag - convert to lowercase
		jsonTag := strings.ToLower(fieldName)

		fmt.Fprintf(&buf, "\t// %s configuration.\n", fieldName)
		buf.WriteString("\t//\n")
		buf.WriteString("\t// +optional\n")
		fmt.Fprintf(&buf, "\t%s *%s `json:\"%s,omitempty\"`\n", fieldName, refTypeName, jsonTag)
	}

	buf.WriteString("}\n\n")

	// Generate the Type type alias with constants
	fmt.Fprintf(&buf, "// %sType represents the type of %s.\n", typeName, prop.Name)
	fmt.Fprintf(&buf, "type %sType string\n\n", typeName)

	fmt.Fprintf(&buf, "// %sType values.\n", typeName)
	buf.WriteString("const (\n")
	for _, variantName := range variantNames {
		constName := fmt.Sprintf("%sType%s", typeName, variantName)
		fmt.Fprintf(&buf, "\t%s %sType = \"%s\"\n", constName, typeName, variantName)
	}
	buf.WriteString(")\n")

	return buf.String()
}

// extractVariantNames extracts clean field names from a list of variant names
// by finding the common prefix and suffix, then extracting the unique middle part.
// e.g., ["ConfigureOIDCIdentityProviderConfig", "SAMLIdentityProviderConfig"] -> ["OIDC", "SAML"]
// e.g., ["CreateDcrProviderRequestAuth0", "CreateDcrProviderRequestAzureAd"] -> ["Auth0", "AzureAd"]
func extractVariantNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	if len(names) == 1 {
		// Single variant - just clean up common prefixes/suffixes
		return []string{cleanSingleVariantName(names[0])}
	}

	// Find common prefix
	prefix := names[0]
	for _, name := range names[1:] {
		prefix = commonPrefix(prefix, name)
	}

	// Find common suffix
	suffix := names[0]
	for _, name := range names[1:] {
		suffix = commonSuffix(suffix, name)
	}

	// Extract the unique middle part from each name
	result := make([]string, len(names))
	for i, name := range names {
		middle := name
		if len(prefix) > 0 {
			middle = strings.TrimPrefix(middle, prefix)
		}
		if len(suffix) > 0 {
			middle = strings.TrimSuffix(middle, suffix)
		}
		// If nothing was extracted, fall back to cleaning the whole name
		if middle == "" {
			middle = cleanSingleVariantName(name)
		} else {
			// Also clean up common prefixes from the extracted name
			middle = cleanSingleVariantName(middle)
		}
		result[i] = middle
	}

	return result
}

// commonPrefix finds the longest common prefix of two strings
func commonPrefix(a, b string) string {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	i := 0
	for i < minLen && a[i] == b[i] {
		i++
	}
	return a[:i]
}

// commonSuffix finds the longest common suffix of two strings
func commonSuffix(a, b string) string {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	i := 0
	for i < minLen && a[len(a)-1-i] == b[len(b)-1-i] {
		i++
	}
	return a[len(a)-i:]
}

// cleanSingleVariantName cleans a single variant name by removing common prefixes/suffixes
func cleanSingleVariantName(name string) string {
	result := name
	for _, suffix := range []string{"Config", "Configuration", "Provider", "Request", "IdentityProvider"} {
		result = strings.TrimSuffix(result, suffix)
	}
	for _, prefix := range []string{"Configure", "Create", "Update"} {
		result = strings.TrimPrefix(result, prefix)
	}
	return result
}

// fixTrailingEmptyLines removes empty lines that appear right before a closing brace
func fixTrailingEmptyLines(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	for i := 0; i < len(lines); i++ {
		// Skip empty lines that are followed by a line containing only "}"
		if strings.TrimSpace(lines[i]) == "" && i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == "}" {
			continue
		}
		result = append(result, lines[i])
	}
	return strings.Join(result, "\n")
}

func (g *Generator) generateRegister(parsed *parser.ParsedSpec) (string, error) {
	tmpl := template.Must(template.New("register").Parse(registerTemplate))

	var entityNames []string
	for name := range parsed.RequestBodies {
		entityNames = append(entityNames, parser.GetEntityNameFromType(name))
	}

	var buf strings.Builder
	data := struct {
		APIGroup    string
		APIVersion  string
		EntityNames []string
	}{
		APIGroup:    g.config.APIGroup,
		APIVersion:  g.config.APIVersion,
		EntityNames: entityNames,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (g *Generator) generateDoc() string {
	return fmt.Sprintf(`// +kubebuilder:object:generate=true
// +groupName=%s
package %s
`, g.config.APIGroup, g.config.APIVersion)
}

func (g *Generator) generateCommonTypes() string {
	return fmt.Sprintf(`package %s

// ObjectRef is a reference to a Kubernetes object in the same namespace
type ObjectRef struct {
	// Name is the name of the referenced object
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Name string `+"`json:\"name,omitempty\"`"+`
}

// NamespacedObjectRef is a reference to a Kubernetes object, optionally in another namespace
type NamespacedObjectRef struct {
	// Name is the name of the referenced object
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Name string `+"`json:\"name,omitempty\"`"+`

	// Namespace is the namespace of the referenced object
	// If empty, the same namespace as the referencing object is used
	//
	// +optional
	// +kubebuilder:validation:MaxLength=63
	Namespace string `+"`json:\"namespace,omitempty\"`"+`
}

// SecretKeyRef is a reference to a key in a Secret
type SecretKeyRef struct {
	// Name is the name of the Secret
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Name string `+"`json:\"name,omitempty\"`"+`

	// Key is the key within the Secret
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Key string `+"`json:\"key,omitempty\"`"+`

	// Namespace is the namespace of the Secret
	//
	// +optional
	// +kubebuilder:validation:MaxLength=63
	Namespace string `+"`json:\"namespace,omitempty\"`"+`
}

// ConfigMapKeyRef is a reference to a key in a ConfigMap
type ConfigMapKeyRef struct {
	// Name is the name of the ConfigMap
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Name string `+"`json:\"name,omitempty\"`"+`

	// Key is the key within the ConfigMap
	//
	// +required
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	Key string `+"`json:\"key,omitempty\"`"+`

	// Namespace is the namespace of the ConfigMap
	//
	// +optional
	// +kubebuilder:validation:MaxLength=63
	Namespace string `+"`json:\"namespace,omitempty\"`"+`
}

// KonnectEntityStatus represents the status of a Konnect entity.
type KonnectEntityStatus struct {
	// ID is the unique identifier of the Konnect entity as assigned by Konnect API.
	// If it's unset (empty string), it means the Konnect entity hasn't been created yet.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	ID string `+"`json:\"id,omitempty\"`"+`

	// ServerURL is the URL of the Konnect server in which the entity exists.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=512
	ServerURL string `+"`json:\"serverURL,omitempty\"`"+`

	// OrgID is ID of Konnect Org that this entity has been created in.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	OrgID string `+"`json:\"organizationID,omitempty\"`"+`
}

// KonnectEntityRef is a reference to a Konnect entity.
type KonnectEntityRef struct {
	// ID is the unique identifier of the Konnect entity as assigned by Konnect API.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	ID string `+"`json:\"id,omitempty\"`"+`
}
`, g.config.APIVersion)
}

// goType converts OpenAPI type to Go type
func (g *Generator) goType(prop *parser.Property) string {
	// Handle references to other entities - convert to ObjectRef
	if prop.IsReference {
		return "*ObjectRef"
	}

	// Handle $ref
	if prop.RefName != "" {
		return prop.RefName
	}

	// Handle oneOf - this becomes a union type
	if len(prop.OneOf) > 0 {
		// Generate a union type name based on the property name
		return "*" + goFieldName(prop.Name)
	}

	var baseType string
	switch prop.Type {
	case "string":
		baseType = "string"
	case "integer":
		switch prop.Format {
		case "int32":
			baseType = "int32"
		case "int64":
			baseType = "int64"
		default:
			baseType = "int"
		}
	case "number":
		switch prop.Format {
		case "float":
			baseType = "float32"
		case "double":
			baseType = "float64"
		default:
			baseType = "float64"
		}
	case "boolean":
		// Required bools have a valid zero value (false), so they need to be pointers
		if prop.Required && !prop.Nullable {
			return "*bool"
		}
		baseType = "bool"
	case "array":
		if prop.Items != nil {
			itemType := g.goType(prop.Items)
			return "[]" + itemType
		}
		return "[]any"
	case "object":
		// Check for oneOf inside object type
		if len(prop.OneOf) > 0 {
			return "*" + goFieldName(prop.Name)
		}
		if prop.AdditionalProperties != nil {
			valueType := g.goType(prop.AdditionalProperties)
			return "map[string]" + valueType
		}
		if len(prop.Properties) > 0 {
			// This will be handled by generating an inline struct
			return goFieldName(prop.Name)
		}
		// Use apiextensionsv1.JSON for arbitrary JSON objects - controller-gen can handle this
		return "apiextensionsv1.JSON"
	default:
		return "any"
	}

	// Handle nullable types with pointers
	if prop.Nullable && !prop.Required {
		return "*" + baseType
	}

	return baseType
}

// formatComment formats a description string for use as a Go comment
// It handles multiline descriptions by prefixing each line with //
// and wraps lines longer than 80 characters
func formatComment(desc string) string {
	if desc == "" {
		return "\t//"
	}
	lines := strings.Split(desc, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			result = append(result, "\t//")
		} else {
			// Wrap lines longer than 80 characters (accounting for "\t// " prefix = 4 chars)
			wrapped := WrapLine(trimmed, 76)
			for _, wrappedLine := range wrapped {
				result = append(result, "\t// "+wrappedLine)
			}
		}
	}
	return strings.Join(result, "\n")
}

// formatSchemaComment formats a description for a top-level schema type
// and wraps lines longer than 80 characters
func formatSchemaComment(name, desc string) string {
	if desc == "" {
		return fmt.Sprintf("// %s is a type alias.\n", name)
	}
	lines := strings.Split(desc, "\n")
	var result []string
	// First line includes the type name
	firstLine := strings.TrimSpace(lines[0])
	if firstLine != "" {
		firstLineWithName := fmt.Sprintf("%s %s", name, firstLine)
		// Wrap if needed (accounting for "// " prefix = 3 chars)
		wrapped := WrapLine(firstLineWithName, 77)
		for _, wrappedLine := range wrapped {
			result = append(result, "// "+wrappedLine)
		}
	} else {
		result = append(result, fmt.Sprintf("// %s", name))
	}
	// Remaining lines
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			result = append(result, "//")
		} else {
			// Wrap lines longer than 80 characters
			wrapped := WrapLine(trimmed, 77)
			for _, wrappedLine := range wrapped {
				result = append(result, "// "+wrappedLine)
			}
		}
	}
	return strings.Join(result, "\n") + "\n"
}

// goFieldName converts property name to Go field name (PascalCase)
func goFieldName(name string) string {
	parts := strings.Split(name, "_")
	for i, part := range parts {
		if len(part) > 0 {
			// Handle common acronyms
			upper := strings.ToUpper(part)
			if isCommonAcronym(upper) {
				parts[i] = upper
			} else {
				parts[i] = strings.ToUpper(part[:1]) + part[1:]
			}
		}
	}
	return strings.Join(parts, "")
}

func isCommonAcronym(s string) bool {
	acronyms := map[string]bool{
		"ID":    true,
		"URL":   true,
		"URI":   true,
		"API":   true,
		"HTTP":  true,
		"HTTPS": true,
		"JSON":  true,
		"XML":   true,
		"UUID":  true,
		"RBAC":  true,
		"DNS":   true,
		"TLS":   true,
		"SSL":   true,
		"IP":    true,
	}
	return acronyms[s]
}

// jsonTag generates the json struct tag
func jsonTag(prop *parser.Property) string {
	tag := prop.Name
	// K8s API best practice: all fields should have omitempty
	tag += ",omitempty"
	return tag
}

// isRefProperty checks if a property is a reference
func isRefProperty(prop *parser.Property) bool {
	return prop.IsReference
}

// hasRootOneOf returns true if the schema has root-level oneOf (i.e., the schema itself is a union type)
func hasRootOneOf(schema *parser.Schema) bool {
	return len(schema.OneOf) > 0
}

// skipProperty returns true if the property should be skipped in CRD generation
func skipProperty(prop *parser.Property) bool {
	// Skip read-only properties (they're typically server-managed like id, created_at, updated_at)
	if prop.ReadOnly {
		return true
	}
	// Skip id field as it's managed by Kubernetes
	if prop.Name == "id" {
		return true
	}
	// Skip timestamp fields
	if prop.Name == "created_at" || prop.Name == "updated_at" {
		return true
	}
	return false
}

// schemaUsesJSON checks if any property in the schema will be generated as apiextensionsv1.JSON
func schemaUsesJSON(g *Generator, schema *parser.Schema) bool {
	for _, prop := range schema.Properties {
		if skipProperty(prop) {
			continue
		}
		if g.goType(prop) == "apiextensionsv1.JSON" {
			return true
		}
	}
	return false
}
