package generator

import (
	"fmt"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// Config holds generator configuration.
type Config struct {
	// API group for CRDs.
	APIGroup string
	// API version.
	APIVersion string
	// Whether to generate status subresource
	GenerateStatus bool
	// FieldConfig holds additional field configurations from YAML
	FieldConfig *config.Config
	// OpsConfig maps entity names to SDK operation configurations.
	// When set, conversion methods are generated on the entity's APISpec type.
	OpsConfig map[string]*config.EntityOpsConfig
	// CommonTypes holds configuration for shared types like ObjectRef.
	// When ObjectRef has an Import config, it will be imported from an external
	// package instead of being generated locally.
	CommonTypes *config.CommonTypesConfig
	// SecretRefEntities is the set of entity names that should generate an
	// optional secret reference on the Spec (SourceType discriminator + SecretRef field).
	SecretRefEntities map[string]bool
	// ReconcilerConfig maps entity names to reconciler generation configurations.
	// When set, reconciler wiring files are generated for the entity.
	ReconcilerConfig map[string]*config.ReconcilerConfig
	// GenerateGroupVersionInfo controls whether to generate groupversion_info.go
	// (with SchemeGroupVersion and Resource helper) instead of register.go.
	// Defaults to true.
	GenerateGroupVersionInfo bool
	// APIGroupPackagePath is the full Go import path for the generated API types package
	// (e.g. "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1").
	APIGroupPackagePath string
	// APIGroupPackageAlias is the import alias for the generated API types package
	// (e.g. "xkonnectv1alpha1").
	APIGroupPackageAlias string
	// SkipGetForUIDEntities is the set of entity names for which getForUID
	// generation should be skipped (e.g. because a hand-written implementation
	// already exists in an ops_<entity>_manual.go file).
	SkipGetForUIDEntities map[string]bool
}

// Generator generates Go CRD types from parsed OpenAPI schemas.
type Generator struct {
	config            Config
	opsCreateInfos    []*OpsCreateFileInfo
	opsUpdateInfos    []*OpsUpdateFileInfo
	opsDeleteInfos    []*OpsDeleteFileInfo
	opsGetForUIDInfos []*OpsGetForUIDFileInfo
	watchInfos        []*WatchFileInfo
}

// NewGenerator creates a new generator.
func NewGenerator(config Config) *Generator {
	return &Generator{config: config}
}

// OpsCreateInfos returns metadata for every create op file emitted by the most
// recent Generate call. Callers (e.g. the Runner) use it to assemble a single
// cross-group ops dispatcher after all group-versions have been generated.
func (g *Generator) OpsCreateInfos() []*OpsCreateFileInfo {
	return g.opsCreateInfos
}

// OpsUpdateInfos returns metadata for every update op file emitted by the most
// recent Generate call. Callers (e.g. the Runner) use it to assemble the
// cross-group update dispatcher after all group-versions have been generated.
func (g *Generator) OpsUpdateInfos() []*OpsUpdateFileInfo {
	return g.opsUpdateInfos
}

// OpsDeleteInfos returns metadata for every delete op file emitted by the most
// recent Generate call. Callers (e.g. the Runner) use it to assemble the
// cross-group delete dispatcher after all group-versions have been generated.
func (g *Generator) OpsDeleteInfos() []*OpsDeleteFileInfo {
	return g.opsDeleteInfos
}

// OpsGetForUIDInfos returns metadata for every getForUID op file emitted by
// the most recent Generate call. Callers (e.g. the Runner) use it to assemble
// the cross-group getForUID dispatcher after all group-versions have been
// generated.
func (g *Generator) OpsGetForUIDInfos() []*OpsGetForUIDFileInfo {
	return g.opsGetForUIDInfos
}

// WatchInfos returns metadata for every reconciler entity emitted by the most
// recent Generate call. Callers (e.g. the Runner) use it to assemble the
// cross-group watch dispatcher after all group-versions have been generated.
func (g *Generator) WatchInfos() []*WatchFileInfo {
	return g.watchInfos
}

// GeneratedFile represents a generated Go file.
type GeneratedFile struct {
	Name    string
	Content string
	// RelativeDir, when set, specifies the output directory relative to the
	// project root instead of the default API types directory.
	RelativeDir string
}

type konnectLabelsField struct {
	FieldName string
	FieldType string
	ValueType string
}

const (
	defaultKonnectStatusPackage = "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	defaultKonnectStatusAlias   = "konnectv1alpha2"
	defaultKonnectStatusType    = "KonnectEntityStatus"
)

// Generate generates Go CRD types from parsed schemas.
func (g *Generator) Generate(parsed *parser.ParsedSpec) ([]GeneratedFile, error) {
	var files []GeneratedFile
	referencedSchemas := make(map[string]bool)
	var reconcilerEntities []string

	// Generate types for each request body (these are the main CRD types).
	for name, schema := range parsed.RequestBodies {
		entityName := parser.GetEntityNameFromType(name)

		entityFiles, err := g.generateEntityFiles(name, entityName, schema)
		if err != nil {
			return nil, err
		}
		files = append(files, entityFiles...)

		if g.config.ReconcilerConfig != nil {
			if _, ok := g.config.ReconcilerConfig[entityName]; ok {
				reconcilerEntities = append(reconcilerEntities, entityName)
			}
		}

		g.collectReferencedSchemas(schema, referencedSchemas)
	}

	reconcilerFiles, err := g.generateReconcilerEntityFiles(reconcilerEntities, parsed)
	if err != nil {
		return nil, err
	}
	files = append(files, reconcilerFiles...)

	sharedFiles, err := g.generateSharedFiles(parsed, referencedSchemas)
	if err != nil {
		return nil, err
	}
	files = append(files, sharedFiles...)

	return files, nil
}

// generateEntityFiles produces all generated files for a single CRD entity:
// types, funcs, SDK ops, and ops create.
func (g *Generator) generateEntityFiles(name, entityName string, schema *parser.Schema) ([]GeneratedFile, error) {
	var files []GeneratedFile

	content, err := g.generateCRDType(name, schema)
	if err != nil {
		return nil, fmt.Errorf("failed to generate type for %s: %w", name, err)
	}
	files = append(files, GeneratedFile{
		Name:    commonGeneratedFilePrefix + EntityFilePrefix(entityName) + "_types.go",
		Content: content,
	})

	funcsContent, err := g.generateCRDFuncs(name, schema)
	if err != nil {
		return nil, fmt.Errorf("failed to generate funcs for %s: %w", name, err)
	}
	files = append(files, GeneratedFile{
		Name:    generatedFuncsFileName(entityName),
		Content: funcsContent,
	})

	sdkOpsFiles, err := g.generateEntitySDKOpsFiles(entityName, schema)
	if err != nil {
		return nil, err
	}
	files = append(files, sdkOpsFiles...)

	opsFile, err := g.generateEntityOpsFileForEntity(entityName, schema)
	if err != nil {
		return nil, err
	}
	if opsFile != nil {
		files = append(files, *opsFile)
	}

	return files, nil
}

// generateEntitySDKOpsFiles generates SDK ops conversion files for an entity
// when ops are configured.
func (g *Generator) generateEntitySDKOpsFiles(entityName string, schema *parser.Schema) ([]GeneratedFile, error) {
	if g.config.OpsConfig == nil {
		return nil, nil
	}
	opsConfig, ok := g.config.OpsConfig[entityName]
	if !ok || opsConfig == nil || len(opsConfig.Ops) == 0 {
		return nil, nil
	}

	opsContent, err := g.generateSDKOps(entityName, schema, opsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to generate SDK ops for %s: %w", entityName, err)
	}

	opsTestContent, err := g.generateSDKOpsTest(entityName, schema, opsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to generate SDK ops test for %s: %w", entityName, err)
	}

	prefix := commonGeneratedFilePrefix + EntityFilePrefix(entityName)
	return []GeneratedFile{
		{Name: prefix + "_sdkops.go", Content: opsContent},
		{Name: prefix + "_sdkops_test.go", Content: opsTestContent},
	}, nil
}

// generateEntityOpsFile generates the per-entity Konnect ops file containing
// create and/or update functions. The cross-group dispatchers are emitted
// separately by the Runner after all group-versions finish.
func (g *Generator) generateEntityOpsFileForEntity(entityName string, schema *parser.Schema) (*GeneratedFile, error) {
	if g.config.ReconcilerConfig == nil || g.config.OpsConfig == nil {
		return nil, nil
	}
	if _, hasReconciler := g.config.ReconcilerConfig[entityName]; !hasReconciler {
		return nil, nil
	}
	opsConfig, ok := g.config.OpsConfig[entityName]
	if !ok || opsConfig == nil {
		return nil, nil
	}

	opsResult, err := g.generateEntityOpsFile(entityName, schema, opsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ops file for %s: %w", entityName, err)
	}
	if opsResult.File == nil {
		return nil, nil
	}
	if opsResult.CreateInfo != nil {
		g.opsCreateInfos = append(g.opsCreateInfos, opsResult.CreateInfo)
	}
	if opsResult.UpdateInfo != nil {
		g.opsUpdateInfos = append(g.opsUpdateInfos, opsResult.UpdateInfo)
	}
	if opsResult.DeleteInfo != nil {
		g.opsDeleteInfos = append(g.opsDeleteInfos, opsResult.DeleteInfo)
	}
	if opsResult.GetForUIDInfo != nil {
		g.opsGetForUIDInfos = append(g.opsGetForUIDInfos, opsResult.GetForUIDInfo)
	}
	return opsResult.File, nil
}

// generateReconcilerEntityFiles generates reconciler wiring files for all
// entities that have reconciler config.
func (g *Generator) generateReconcilerEntityFiles(reconcilerEntities []string, parsed *parser.ParsedSpec) ([]GeneratedFile, error) {
	if len(reconcilerEntities) == 0 {
		return nil, nil
	}
	sort.Strings(reconcilerEntities)
	entitySchemas := make(map[string]*parser.Schema, len(parsed.RequestBodies))
	for name, schema := range parsed.RequestBodies {
		entitySchemas[parser.GetEntityNameFromType(name)] = schema
	}
	return g.generateReconcilerFiles(reconcilerEntities, entitySchemas)
}

// generateSharedFiles generates files shared across all entities:
// groupversion_info.go, doc.go, common_types.go, and schema_types.go.
func (g *Generator) generateSharedFiles(parsed *parser.ParsedSpec, referencedSchemas map[string]bool) ([]GeneratedFile, error) {
	var files []GeneratedFile

	if g.config.GenerateGroupVersionInfo {
		gviContent, err := g.generateGroupVersionInfo(parsed)
		if err != nil {
			return nil, fmt.Errorf("failed to generate groupversion_info file: %w", err)
		}
		files = append(files, GeneratedFile{
			Name:    "groupversion_info.go",
			Content: gviContent,
		})

		files = append(files, GeneratedFile{
			Name:    "doc.go",
			Content: g.generateDoc(),
		})
	}

	commonContent, err := g.generateCommonTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to generate common types: %w", err)
	}
	files = append(files, GeneratedFile{
		Name:    "common_types.go",
		Content: commonContent,
	})

	if len(referencedSchemas) > 0 {
		files = append(files, GeneratedFile{
			Name:    "schema_types.go",
			Content: g.generateSchemaTypes(referencedSchemas, parsed),
		})
	}

	return files, nil
}

// collectReferencedSchemas collects all schema names referenced by properties.
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

// generateSchemaTypes generates Go type definitions for referenced schemas.
func (g *Generator) generateSchemaTypes(refs map[string]bool, parsed *parser.ParsedSpec) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "%s\n\npackage %s\n\n", sharedGeneratedFilePreamble, g.config.APIVersion)

	// Sort keys to ensure deterministic output order
	refNames := make([]string, 0, len(refs))
	for refName := range refs {
		refNames = append(refNames, refName)
	}
	sort.Strings(refNames)

	if needsJSONImport, objectRefImport := g.schemaTypesImports(refNames, parsed); needsJSONImport || objectRefImport != nil {
		buf.WriteString("import (\n")
		if needsJSONImport {
			buf.WriteString("\tapiextensionsv1 \"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1\"\n")
		}
		if objectRefImport != nil {
			if objectRefImport.Alias != "" {
				fmt.Fprintf(&buf, "\t%s %q\n", objectRefImport.Alias, objectRefImport.Path)
			} else {
				fmt.Fprintf(&buf, "\t%q\n", objectRefImport.Path)
			}
		}
		buf.WriteString(")\n\n")
	}

	for _, refName := range refNames {
		if schema, ok := parsed.Schemas[refName]; ok {
			goName := fixInitialisms(refName)

			// Format the description as a proper comment
			comment := formatSchemaComment(goName, schema.Description)

			// Generate based on schema type
			switch {
			case len(schema.Properties) > 0:
				// It's an object type - generate a struct
				buf.WriteString(comment)
				fmt.Fprintf(&buf, "type %s struct {\n", goName)
				for _, prop := range schema.Properties {
					if skipProperty(prop) {
						continue
					}
					g.writeSchemaTypeField(&buf, prop)
				}
				buf.WriteString("}\n\n")
			case schema.Type == "boolean":
				// Per K8s API convention (nobools), boolean schemas become string
				// types with Enabled/Disabled enum constants.
				buf.WriteString(comment)
				buf.WriteString("//\n// +kubebuilder:validation:Enum=Enabled;Disabled\n")
				fmt.Fprintf(&buf, "type %s string\n\n", goName)
				fmt.Fprintf(&buf, "const (\n")
				fmt.Fprintf(&buf, "\t// %sEnabled sets %s as enabled.\n", goName, goName)
				fmt.Fprintf(&buf, "\t%sEnabled  %s = \"Enabled\"\n", goName, goName)
				fmt.Fprintf(&buf, "\t// %sDisabled sets %s as disabled.\n", goName, goName)
				fmt.Fprintf(&buf, "\t%sDisabled %s = \"Disabled\"\n", goName, goName)
				fmt.Fprintf(&buf, ")\n\n")

			case schema.AdditionalProperties != nil:
				// Map type with value constraints: generate a dedicated value type
				// with native kubebuilder markers, then define the map using it.
				valueTypeName := refName + "Value"
				valueBaseType := propertyToGoBaseType(schema.AdditionalProperties)

				fmt.Fprintf(&buf, "// %s is the value type for %s.\n", valueTypeName, refName)
				if markers := valueTypeMarkers(schema.AdditionalProperties); len(markers) > 0 {
					buf.WriteString("//\n")
					for _, marker := range markers {
						fmt.Fprintf(&buf, "// %s\n", marker)
					}
				}
				fmt.Fprintf(&buf, "type %s %s\n\n", valueTypeName, valueBaseType)

				buf.WriteString(comment)
				fmt.Fprintf(&buf, "type %s map[string]%s\n\n", refName, valueTypeName)

			default:
				// Generate based on the schema's actual type
				buf.WriteString(comment)
				goType := schemaToGoType(schema)
				fmt.Fprintf(&buf, "type %s %s\n\n", goName, goType)
			}
		} else {
			panic("Schema not found for reference: " + refName)
		}
	}

	return strings.TrimRight(buf.String(), "\n") + "\n"
}

func (g *Generator) schemaTypesImports(refNames []string, parsed *parser.ParsedSpec) (bool, *config.ImportConfig) {
	needsJSONImport := false
	var objectRefImport *config.ImportConfig

	for _, refName := range refNames {
		schema, ok := parsed.Schemas[refName]
		if !ok {
			continue
		}

		if !needsJSONImport && schemaUsesJSON(g, schema) {
			needsJSONImport = true
		}
		if objectRefImport == nil {
			objectRefImport = g.objectRefImportIfNeeded(schema)
		}

		if needsJSONImport && objectRefImport != nil {
			break
		}
	}

	return needsJSONImport, objectRefImport
}

func (g *Generator) writeSchemaTypeField(buf *strings.Builder, prop *parser.Property) {
	buf.WriteString(formatComment(prop.Description))
	buf.WriteString("\n")
	buf.WriteString("\t//\n")
	for _, tag := range KubebuilderTags(prop, "", nil) {
		fmt.Fprintf(buf, "\t// %s\n", tag)
	}
	fmt.Fprintf(buf, "\t%s %s `json:\"%s\"`\n", goFieldName(prop.Name), g.goType(prop), jsonTag(prop))
}

// schemaToGoType converts a parsed Schema's type info to the appropriate Go type string.
// This is used for referenced schemas that are simple types (not objects with properties).
func schemaToGoType(schema *parser.Schema) string {
	switch schema.Type {
	case "string":
		return "string"
	case "boolean":
		return "string"
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
	validFields := make(map[string]struct{})
	for _, prop := range schema.Properties {
		if !skipProperty(prop) {
			validFields[prop.Name] = struct{}{}
		}
	}
	// Also include dependency fields
	for _, dep := range schema.Dependencies {
		validFields[dep.JSONName] = struct{}{}
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

	hasOptionalSecretRef := g.config.SecretRefEntities[entityName]

	// In the CRD APISpec, property-level oneOf types are rendered as a pointer
	// to a generated union type. The union type is emitted in the same package,
	// so its name must be prefixed with the entity name to avoid package-scoped
	// collisions (e.g. a bare "Config" type would clash across entities).
	goTypeInCRD := func(prop *parser.Property) string {
		if len(prop.OneOf) > 0 {
			return "*" + entityName + goFieldName(prop.Name)
		}
		return g.goType(prop)
	}

	funcMap := template.FuncMap{
		"goType":                goTypeInCRD,
		"goFieldName":           goFieldName,
		"jsonTag":               jsonTag,
		"kubebuilderTags":       kubebuilderTagsWithConfig,
		"isRefProperty":         isRefProperty,
		"refEntityName":         parser.GetRefEntityName,
		"skipProperty":          skipProperty,
		"lower":                 strings.ToLower,
		"formatComment":         formatComment,
		"hasRootOneOf":          hasRootOneOf,
		"objectRefTypeName":     func() string { return g.objectRefTypeName() },
		"namespacedRefTypeName": func() string { return g.namespacedRefTypeName() },
	}

	tmpl := template.Must(template.New("crd").Funcs(funcMap).Parse(crdTypeTemplate))

	// Determine whether we need the ObjectRef import: either for dependencies/refs
	// or for the optional secret ref's NamespacedRef type.
	objectRefImport := g.objectRefImportIfNeeded(schema)
	if objectRefImport == nil && hasOptionalSecretRef && g.objectRefImported() {
		objectRefImport = g.config.CommonTypes.ObjectRef.Import
	}

	hasRootReconciler := false
	if rc := g.config.ReconcilerConfig[entityName]; rc != nil {
		hasRootReconciler = rc.IsRoot
	}

	var buf strings.Builder
	data := struct {
		EntityName           string
		Schema               *parser.Schema
		APIGroup             string
		APIVersion           string
		NeedsJSONImport      bool
		ObjectRefImport      *config.ImportConfig
		HasOptionalSecretRef bool
		HasRootReconciler    bool
	}{
		EntityName:           entityName,
		Schema:               schema,
		APIGroup:             g.config.APIGroup,
		APIVersion:           g.config.APIVersion,
		NeedsJSONImport:      schemaUsesJSON(g, schema),
		ObjectRefImport:      objectRefImport,
		HasOptionalSecretRef: hasOptionalSecretRef,
		HasRootReconciler:    hasRootReconciler,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	// Generate union types for any oneOf properties
	unionTypes := g.generateUnionTypes(schema, entityName)
	if unionTypes != "" {
		buf.WriteString("\n")
		buf.WriteString(unionTypes)
	}

	// Post-process to remove trailing empty lines before closing braces in structs
	result := fixTrailingEmptyLines(buf.String())

	return result, nil
}

func (g *Generator) generateCRDFuncs(name string, schema *parser.Schema) (string, error) {
	entityName := parser.GetEntityNameFromType(name)
	isReconcilerRoot := false
	if rc := g.config.ReconcilerConfig[entityName]; rc != nil {
		isReconcilerRoot = rc.IsRoot
	}

	imports := make([]*config.ImportConfig, 0, 3)
	imports = appendUniqueImportConfig(imports, defaultKonnectStatusImport())
	imports = appendUniqueImportConfig(imports, &config.ImportConfig{
		Alias: "metav1",
		Path:  "k8s.io/apimachinery/pkg/apis/meta/v1",
	})
	if isReconcilerRoot {
		imports = appendUniqueImportConfig(imports, &config.ImportConfig{
			Alias: defaultKonnectStatusAlias,
			Path:  defaultKonnectStatusPackage,
		})
	}

	tmpl := template.Must(template.New("crdFuncs").Parse(crdFuncsTemplate))

	var buf strings.Builder
	data := struct {
		EntityName                         string
		APIVersion                         string
		Imports                            []*config.ImportConfig
		KonnectStatusType                  string
		KonnectLabelsField                 *konnectLabelsField
		Dependencies                       []*parser.Dependency
		IsReconcilerRoot                   bool
		KonnectAPIAuthConfigurationRefType string
	}{
		EntityName:                         entityName,
		APIVersion:                         g.config.APIVersion,
		Imports:                            imports,
		KonnectStatusType:                  defaultKonnectStatusQualifiedTypeName(),
		KonnectLabelsField:                 g.konnectLabelsField(schema),
		Dependencies:                       schema.Dependencies,
		IsReconcilerRoot:                   isReconcilerRoot,
		KonnectAPIAuthConfigurationRefType: defaultKonnectStatusAlias + ".ControlPlaneKonnectAPIAuthConfigurationRef",
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return fixTrailingEmptyLines(buf.String()), nil
}

// EntityFilePrefix converts a PascalCase entity name to a lowercase file name
// prefix, inserting an underscore after a leading "Konnect" prefix.
// e.g. "KonnectEventControlPlane" → "konnect_eventcontrolplane",
//
//	"Portal" → "portal".
func EntityFilePrefix(entityName string) string {
	lower := strings.ToLower(entityName)
	if after, ok := strings.CutPrefix(lower, "konnect"); ok && after != "" {
		return "konnect_" + after
	}
	return lower
}

func generatedFuncsFileName(entityName string) string {
	return "zz_generated_" + EntityFilePrefix(entityName) + "_funcs.go"
}

func appendUniqueImportConfig(imports []*config.ImportConfig, imp *config.ImportConfig) []*config.ImportConfig {
	if imp == nil {
		return imports
	}
	for _, existing := range imports {
		if existing.Path == imp.Path && existing.Alias == imp.Alias {
			return imports
		}
	}
	return append(imports, &config.ImportConfig{
		Alias: imp.Alias,
		Path:  imp.Path,
	})
}

func (g *Generator) konnectLabelsField(schema *parser.Schema) *konnectLabelsField {
	if schema == nil {
		return nil
	}

	for _, prop := range schema.Properties {
		if skipProperty(prop) || prop.Name != "labels" {
			continue
		}

		valueType, ok := g.konnectLabelsValueType(prop)
		if !ok {
			continue
		}

		return &konnectLabelsField{
			FieldName: goFieldName(prop.Name),
			FieldType: g.goType(prop),
			ValueType: valueType,
		}
	}

	return nil
}

func (g *Generator) konnectLabelsValueType(prop *parser.Property) (string, bool) {
	if prop == nil {
		return "", false
	}

	if prop.RefName != "" {
		return fixInitialisms(prop.RefName + "Value"), true
	}

	if prop.AdditionalProperties == nil {
		return "", false
	}

	return g.goType(prop.AdditionalProperties), true
}

// generateUnionTypes generates Go union type structs for properties with oneOf.
// Property-level union type names are prefixed with entityName to avoid
// package-scoped collisions between entities that share a property name
// (e.g. "config").
func (g *Generator) generateUnionTypes(schema *parser.Schema, entityName string) string {
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
			buf.WriteString(g.generateUnionType(prop, entityName))
		}
	}

	return buf.String()
}

// generateRootUnionType generates a union type for a schema with root-level oneOf.
func (g *Generator) generateRootUnionType(schema *parser.Schema) string {
	// Create a synthetic property for the union type
	// Add "Config" suffix to differentiate from the main CRD type
	entityName := parser.GetEntityNameFromType(schema.Name)
	prop := &parser.Property{
		Name:  entityName + "Config",
		OneOf: schema.OneOf,
	}
	// The synthetic prop.Name already encodes the entity prefix, so pass empty.
	return g.generateUnionType(prop, "")
}

// generateUnionType generates a single union type struct.
// typeNamePrefix is prepended to the generated Go type name to avoid
// package-scoped collisions for common property names like "config".
func (g *Generator) generateUnionType(prop *parser.Property, typeNamePrefix string) string {
	var buf strings.Builder

	typeName := typeNamePrefix + goFieldName(prop.Name)

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
			refTypeName = fixInitialisms(variant.RefName)
		}

		fieldName := fixInitialisms(variantNames[i])

		// Generate JSON tag - convert to lowercase, using the original variant name
		// to preserve API compatibility.
		jsonTag := strings.ToLower(variantNames[i])

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
		goVariantName := fixInitialisms(variantName)
		constName := fmt.Sprintf("%sType%s", typeName, goVariantName)
		fmt.Fprintf(&buf, "\t%s %sType = \"%s\"\n", constName, typeName, variantName)
	}
	buf.WriteString(")\n")

	return buf.String()
}

// extractVariantNames extracts clean field names from a list of variant names
// by finding the common prefix and suffix, then extracting the unique middle part.
// e.g., ["ConfigureOIDCIdentityProviderConfig", "SAMLIdentityProviderConfig"] -> ["OIDC", "SAML"]
// e.g., ["CreateDcrProviderRequestAuth0", "CreateDcrProviderRequestAzureAd"] -> ["Auth0", "AzureAd"].
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

// commonPrefix finds the longest common prefix of two strings.
func commonPrefix(a, b string) string {
	minLen := min(len(b), len(a))
	i := 0
	for i < minLen && a[i] == b[i] {
		i++
	}
	return a[:i]
}

// commonSuffix finds the longest common suffix of two strings.
func commonSuffix(a, b string) string {
	minLen := min(len(b), len(a))
	i := 0
	for i < minLen && a[len(a)-1-i] == b[len(b)-1-i] {
		i++
	}
	return a[len(a)-i:]
}

// cleanSingleVariantName cleans a single variant name by removing common prefixes/suffixes.
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

// fixTrailingEmptyLines removes empty lines that appear right before a closing brace.
func fixTrailingEmptyLines(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	for i := range lines {
		// Skip empty lines that are followed by a line containing only "}"
		if strings.TrimSpace(lines[i]) == "" && i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == "}" {
			continue
		}
		result = append(result, lines[i])
	}
	return strings.Join(result, "\n")
}

func (g *Generator) generateGroupVersionInfo(parsed *parser.ParsedSpec) (string, error) {
	tmpl := template.Must(template.New("groupVersionInfo").Parse(groupVersionInfoTemplate))

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
	year := time.Now().Year()
	return fmt.Sprintf(`%s

/*
Copyright %d Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package %s contains API Schema definitions for the %s %s API group.
// +kubebuilder:object:generate=true
// +groupName=%s
// +apireference:kgo:include-all-types
package %s
`, sharedGeneratedFilePreamble, year, g.config.APIVersion, g.config.APIGroup, g.config.APIVersion, g.config.APIGroup, g.config.APIVersion)
}

func (g *Generator) generateCommonTypes() (string, error) {
	tmpl := template.Must(template.New("commonTypes").Parse(commonTypesTemplate))

	var buf strings.Builder
	data := struct {
		APIVersion           string
		KonnectStatusImport  *config.ImportConfig
		KonnectStatusType    string
		ObjectRefImported    bool
		Namespaced           bool
		HasSecretRefEntities bool
	}{
		APIVersion:           g.config.APIVersion,
		KonnectStatusImport:  defaultKonnectStatusImport(),
		KonnectStatusType:    defaultKonnectStatusQualifiedTypeName(),
		ObjectRefImported:    g.objectRefImported(),
		Namespaced:           g.objectRefNamespaced(),
		HasSecretRefEntities: len(g.config.SecretRefEntities) > 0,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func defaultKonnectStatusImport() *config.ImportConfig {
	return &config.ImportConfig{
		Path:  defaultKonnectStatusPackage,
		Alias: defaultKonnectStatusAlias,
	}
}

func defaultKonnectStatusQualifiedTypeName() string {
	return defaultKonnectStatusAlias + "." + defaultKonnectStatusType
}

// objectRefNamespaced returns true if the generated ObjectRef's NamespacedRef
// should include a Namespace field.
func (g *Generator) objectRefNamespaced() bool {
	return g.config.CommonTypes != nil &&
		g.config.CommonTypes.ObjectRef != nil &&
		g.config.CommonTypes.ObjectRef.Namespaced
}

// objectRefImported returns true if ObjectRef should be imported from an
// external package rather than generated locally.
func (g *Generator) objectRefImported() bool {
	return g.config.CommonTypes != nil &&
		g.config.CommonTypes.ObjectRef != nil &&
		g.config.CommonTypes.ObjectRef.Import != nil
}

// objectRefImportIfNeeded returns the ImportConfig for ObjectRef only when the
// schema actually uses ObjectRef (has dependencies or reference properties) and
// ObjectRef is configured as an external import. Returns nil otherwise.
func (g *Generator) objectRefImportIfNeeded(schema *parser.Schema) *config.ImportConfig {
	if !g.objectRefImported() {
		return nil
	}
	if schemaUsesObjectRef(schema) {
		return g.config.CommonTypes.ObjectRef.Import
	}
	return nil
}

// schemaUsesObjectRef returns true if the schema has dependencies or reference
// properties that will generate ObjectRef fields.
func schemaUsesObjectRef(schema *parser.Schema) bool {
	if len(schema.Dependencies) > 0 {
		return true
	}
	for _, prop := range schema.Properties {
		if !skipProperty(prop) && prop.IsReference {
			return true
		}
	}
	return false
}

// objectRefTypeName returns the Go type name for ObjectRef, qualified with the
// import alias when ObjectRef is imported from an external package.
func (g *Generator) objectRefTypeName() string {
	if g.objectRefImported() {
		return g.importedTypePrefix() + "ObjectRef"
	}
	return "ObjectRef"
}

// namespacedRefTypeName returns the Go type name for NamespacedRef, qualified
// with the import alias when ObjectRef (and therefore NamespacedRef) is
// imported from an external package.
func (g *Generator) namespacedRefTypeName() string {
	if g.objectRefImported() {
		return g.importedTypePrefix() + "NamespacedRef"
	}
	return "NamespacedRef"
}

// importedTypePrefix returns the package qualifier prefix for types imported
// from the ObjectRef external package (e.g. "commonv1alpha1.").
func (g *Generator) importedTypePrefix() string {
	imp := g.config.CommonTypes.ObjectRef.Import
	return importQualifier(imp)
}

func importQualifier(imp *config.ImportConfig) string {
	if imp == nil {
		return ""
	}
	if imp.Alias != "" {
		return imp.Alias + "."
	}
	parts := strings.Split(imp.Path, "/")
	return parts[len(parts)-1] + "."
}

// sdkOpsImport represents a single import needed for SDK ops generation.
type sdkOpsImport struct {
	Alias string
	Path  string
}

// sdkOpsMethod represents a single SDK conversion method to generate.
type sdkOpsMethod struct {
	MethodName  string
	TypeName    string
	ImportAlias string

	NestedUnionFields []sdkOpsNestedUnionField
}

type sdkOpsRootUnionMethod struct {
	sdkOpsMethod

	IsCreate bool
}

type sdkOpsTestMethod struct {
	sdkOpsMethod

	ExpectError bool
	TestFields  []sdkOpsTestField
}

// sdkOpsBoolField represents a boolean field path that needs normalization
// from the CRD's Enabled/Disabled enum strings back to JSON booleans.
type sdkOpsBoolField struct {
	Label string
	Path  []string
}

// sdkOpsTestField represents a field to populate in the generated test.
type sdkOpsTestField struct {
	FieldName     string
	JSONName      string
	TestValue     string
	ExpectedValue string
}

type sdkOpsNestedUnionField struct {
	FieldName       string
	JSONName        string
	TargetFieldName string
	Variants        []sdkOpsNestedUnionVariant
}

type sdkOpsNestedUnionVariant struct {
	FieldName       string
	JSONName        string
	TypeValue       string
	MemberTypeName  string
	ConstructorName string
}

type sdkOpsRootUnionVariant struct {
	FieldName             string
	JSONName              string
	TypeConstName         string
	CreateVariantTypeName string
	CreateConstructorName string
	UpdatePayloadJSONName string
	UpdateTargetFieldName string
	UpdateVariantTypeName string
	UpdateConstructorName string
}

// generateSDKOps generates a file with conversion methods from {Entity}APISpec
// to SDK request types using JSON marshal/unmarshal.
func (g *Generator) generateSDKOps(entityName string, schema *parser.Schema, opsConfig *config.EntityOpsConfig) (string, error) {
	imports, methods, err := g.buildSDKOpsMethods(opsConfig)
	if err != nil {
		return "", err
	}
	boolFields := g.collectSDKOpsBoolFields(schema)

	if hasRootOneOf(schema) {
		return g.generateRootUnionSDKOps(entityName, schema, imports, methods, boolFields)
	}

	standardMethods := make([]sdkOpsMethod, 0, len(methods))
	for _, method := range methods {
		method.NestedUnionFields = g.buildSDKOpsNestedUnionFields(schema, method)
		standardMethods = append(standardMethods, method)
	}

	tmpl := template.Must(template.New("sdkops").Parse(sdkOpsTemplate))
	var buf strings.Builder
	data := struct {
		APIVersion string
		EntityName string
		Imports    []*sdkOpsImport
		BoolFields []sdkOpsBoolField
		Methods    []sdkOpsMethod
	}{
		APIVersion: g.config.APIVersion,
		EntityName: entityName,
		Imports:    imports,
		BoolFields: boolFields,
		Methods:    standardMethods,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (g *Generator) buildSDKOpsNestedUnionFields(
	schema *parser.Schema,
	method sdkOpsMethod,
) []sdkOpsNestedUnionField {
	fields := make([]sdkOpsNestedUnionField, 0)
	for _, prop := range schema.Properties {
		if skipProperty(prop) || len(prop.OneOf) == 0 {
			continue
		}

		rawVariantNames := make([]string, 0, len(prop.OneOf))
		for _, variant := range prop.OneOf {
			variantName := variant.Name
			if variant.RefName != "" {
				variantName = variant.RefName
			}
			rawVariantNames = append(rawVariantNames, variantName)
		}
		variantNames := extractVariantNames(rawVariantNames)

		variants := make([]sdkOpsNestedUnionVariant, 0, len(prop.OneOf))
		for i, variant := range prop.OneOf {
			variantRefName := variant.Name
			if variant.RefName != "" {
				variantRefName = variant.RefName
			}
			variants = append(variants, sdkOpsNestedUnionVariant{
				FieldName:       fixInitialisms(variantNames[i]),
				JSONName:        strings.ToLower(variantNames[i]),
				TypeValue:       variantNames[i],
				MemberTypeName:  fixInitialisms(variantRefName),
				ConstructorName: "Create" + method.TypeName + goFieldName(prop.Name) + fixInitialisms(variantRefName),
			})
		}

		fields = append(fields, sdkOpsNestedUnionField{
			FieldName:       goFieldName(prop.Name),
			JSONName:        prop.Name,
			TargetFieldName: goFieldName(prop.Name),
			Variants:        variants,
		})
	}

	return fields
}

func (g *Generator) generateRootUnionSDKOps(
	entityName string,
	schema *parser.Schema,
	imports []*sdkOpsImport,
	methods []sdkOpsMethod,
	boolFields []sdkOpsBoolField,
) (string, error) {
	rootUnionTypeName := goFieldName(entityName + "Config")

	rootUnionMethods := make([]sdkOpsRootUnionMethod, 0, len(methods))
	hasUpdateMethod := false
	for _, method := range methods {
		isCreate := strings.HasPrefix(method.MethodName, "ToCreate")
		if !isCreate {
			hasUpdateMethod = true
		}
		rootUnionMethods = append(rootUnionMethods, sdkOpsRootUnionMethod{
			sdkOpsMethod: method,
			IsCreate:     isCreate,
		})
	}

	var rawVariantNames []string
	for _, variant := range schema.OneOf {
		variantName := variant.Name
		if variant.RefName != "" {
			variantName = variant.RefName
		}
		rawVariantNames = append(rawVariantNames, variantName)
	}
	variantNames := extractVariantNames(rawVariantNames)

	variants := make([]sdkOpsRootUnionVariant, 0, len(schema.OneOf))
	for i, variant := range schema.OneOf {
		variantRefName := variant.Name
		if variant.RefName != "" {
			variantRefName = variant.RefName
		}
		updatePayloadJSONName := ""
		updateTargetFieldName := ""
		updateVariantTypeName := ""
		updateConstructorName := ""
		if hasUpdateMethod {
			updatePayloadProp, err := findRootUnionUpdatePayloadProperty(variant.Properties)
			if err != nil {
				return "", fmt.Errorf("failed to infer update payload property for %s variant %q: %w", entityName, variantRefName, err)
			}
			if updatePayloadProp == nil {
				return "", fmt.Errorf("failed to infer update payload property for %s variant %q: no ref payload property found", entityName, variantRefName)
			}
			updatePayloadJSONName = updatePayloadProp.Name
			updateTargetFieldName = goFieldName(updatePayloadProp.Name)
			updateVariantTypeName = fixInitialisms(strings.Replace(updatePayloadProp.RefName, "Create", "Update", 1))
			updateConstructorName = "Create" + goFieldName(updatePayloadProp.Name) + updateVariantTypeName
		}
		fieldName := fixInitialisms(variantNames[i])
		variants = append(variants, sdkOpsRootUnionVariant{
			FieldName:             fieldName,
			JSONName:              strings.ToLower(variantNames[i]),
			TypeConstName:         fmt.Sprintf("%sType%s", rootUnionTypeName, fieldName),
			CreateVariantTypeName: fixInitialisms(variantRefName),
			CreateConstructorName: "Create" + fixInitialisms(variantRefName),
			UpdatePayloadJSONName: updatePayloadJSONName,
			UpdateTargetFieldName: updateTargetFieldName,
			UpdateVariantTypeName: updateVariantTypeName,
			UpdateConstructorName: updateConstructorName,
		})
	}

	tmpl := template.Must(template.New("sdkops-root-union").Parse(sdkOpsRootUnionTemplate))
	var buf strings.Builder
	data := struct {
		APIVersion    string
		EntityName    string
		UnionTypeName string
		Imports       []*sdkOpsImport
		BoolFields    []sdkOpsBoolField
		Methods       []sdkOpsRootUnionMethod
		Variants      []sdkOpsRootUnionVariant
	}{
		APIVersion:    g.config.APIVersion,
		EntityName:    entityName,
		UnionTypeName: rootUnionTypeName,
		Imports:       imports,
		BoolFields:    boolFields,
		Methods:       rootUnionMethods,
		Variants:      variants,
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// generateSDKOpsTest generates a test file for the SDK ops conversion methods.
func (g *Generator) generateSDKOpsTest(entityName string, schema *parser.Schema, opsConfig *config.EntityOpsConfig) (string, error) {
	_, methods, err := g.buildSDKOpsMethods(opsConfig)
	if err != nil {
		return "", err
	}

	testMethods := make([]sdkOpsTestMethod, 0, len(methods))
	for _, method := range methods {
		testFields := g.buildSDKOpsTestFields(schema.Properties, method)
		testMethods = append(testMethods, sdkOpsTestMethod{
			sdkOpsMethod: method,
			ExpectError:  len(testFields) == 0,
			TestFields:   testFields,
		})
	}

	tmpl := template.Must(template.New("sdkopstest").Parse(sdkOpsTestTemplate))
	var buf strings.Builder
	data := struct {
		APIVersion string
		EntityName string
		Methods    []sdkOpsTestMethod
	}{
		APIVersion: g.config.APIVersion,
		EntityName: entityName,
		Methods:    testMethods,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (g *Generator) buildSDKOpsTestFields(props []*parser.Property, method sdkOpsMethod) []sdkOpsTestField {
	testFields := make([]sdkOpsTestField, 0, len(props))
	for _, prop := range props {
		if skipProperty(prop) || prop.IsReference || shouldSkipSDKOpsTestField(prop, method) {
			continue
		}
		goType := g.goType(prop)
		testValue, expectedValue := testValuesForProperty(prop, goType)
		if testValue == "" || expectedValue == "" {
			continue
		}
		testFields = append(testFields, sdkOpsTestField{
			FieldName:     goFieldName(prop.Name),
			JSONName:      prop.Name,
			TestValue:     testValue,
			ExpectedValue: expectedValue,
		})
	}
	return testFields
}

func shouldSkipSDKOpsTestField(prop *parser.Property, method sdkOpsMethod) bool {
	return strings.HasPrefix(method.MethodName, "ToUpdate") && prop.Name == "type"
}

// buildSDKOpsMethods parses the ops config and returns sorted imports and methods.
func (g *Generator) buildSDKOpsMethods(opsConfig *config.EntityOpsConfig) ([]*sdkOpsImport, []sdkOpsMethod, error) {
	imports := make(map[string]*sdkOpsImport)
	var methods []sdkOpsMethod

	opNames := make([]string, 0, len(opsConfig.Ops))
	for opName := range opsConfig.Ops {
		opNames = append(opNames, opName)
	}
	sort.Strings(opNames)

	for _, opName := range opNames {
		// Delete ops have no request body type — skip SDK method generation.
		if opName == "delete" {
			continue
		}
		opCfg := opsConfig.Ops[opName]
		importPath, typeName, err := ParseSDKTypePath(opCfg.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("operation %q: %w", opName, err)
		}

		alias := sdkImportAlias(importPath)
		imports[importPath] = &sdkOpsImport{
			Alias: alias,
			Path:  importPath,
		}

		methods = append(methods, sdkOpsMethod{
			MethodName:  "To" + typeName,
			TypeName:    typeName,
			ImportAlias: alias,
		})
	}

	importPaths := make([]string, 0, len(imports))
	for p := range imports {
		importPaths = append(importPaths, p)
	}
	sort.Strings(importPaths)
	sortedImports := make([]*sdkOpsImport, 0, len(importPaths))
	for _, p := range importPaths {
		sortedImports = append(sortedImports, imports[p])
	}

	return sortedImports, methods, nil
}

func (g *Generator) collectSDKOpsBoolFields(schema *parser.Schema) []sdkOpsBoolField {
	if schema == nil {
		return nil
	}

	var fields []sdkOpsBoolField
	for _, prop := range schema.Properties {
		g.collectSDKOpsBoolFieldsFromProperty(prop, []string{prop.Name}, &fields)
	}
	if len(schema.OneOf) > 0 {
		rawVariantNames := make([]string, 0, len(schema.OneOf))
		for _, variant := range schema.OneOf {
			variantName := variant.Name
			if variant.RefName != "" {
				variantName = variant.RefName
			}
			rawVariantNames = append(rawVariantNames, variantName)
		}
		variantNames := extractVariantNames(rawVariantNames)
		for i, variant := range schema.OneOf {
			variantJSONName := strings.ToLower(variantNames[i])
			for _, nestedProp := range variant.Properties {
				g.collectSDKOpsBoolFieldsFromProperty(nestedProp, []string{variantJSONName, nestedProp.Name}, &fields)
			}
		}
	}

	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Label < fields[j].Label
	})

	return fields
}

func (g *Generator) collectSDKOpsBoolFieldsFromProperty(prop *parser.Property, path []string, fields *[]sdkOpsBoolField) {
	if prop == nil || skipProperty(prop) || prop.IsReference {
		return
	}

	if prop.Type == "boolean" {
		*fields = append(*fields, sdkOpsBoolField{
			Label: sdkOpsBoolFieldLabel(path),
			Path:  append([]string(nil), path...),
		})
	}

	if prop.Items != nil {
		g.collectSDKOpsBoolFieldsFromProperty(prop.Items, append(path, "[]"), fields)
	}
	for _, nestedProp := range prop.Properties {
		g.collectSDKOpsBoolFieldsFromProperty(nestedProp, append(path, nestedProp.Name), fields)
	}
	if prop.AdditionalProperties != nil {
		g.collectSDKOpsBoolFieldsFromProperty(prop.AdditionalProperties, append(path, "{}"), fields)
	}
}

func sdkOpsBoolFieldLabel(path []string) string {
	return strings.Join(path, ".")
}

func findRootUnionUpdatePayloadProperty(properties []*parser.Property) (*parser.Property, error) {
	requiredRefProps := make([]*parser.Property, 0, len(properties))
	refProps := make([]*parser.Property, 0, len(properties))

	for _, prop := range properties {
		if prop.RefName == "" {
			continue
		}
		refProps = append(refProps, prop)
		if prop.Required {
			requiredRefProps = append(requiredRefProps, prop)
		}
	}

	switch len(requiredRefProps) {
	case 1:
		return requiredRefProps[0], nil
	case 0:
	default:
		return nil, fmt.Errorf("multiple required ref payload properties found")
	}

	switch len(refProps) {
	case 1:
		return refProps[0], nil
	case 0:
		return nil, nil
	default:
		return nil, fmt.Errorf("multiple ref payload properties found")
	}
}

// sdkImportAlias generates a deterministic import alias from an SDK import path.
// For "github.com/Kong/sdk-konnect-go/models/components" it produces "sdkkonnectcomp".
func sdkImportAlias(importPath string) string {
	parts := strings.Split(importPath, "/")
	lastSegment := parts[len(parts)-1]

	short := lastSegment
	if len(short) > 4 {
		short = short[:4]
	}

	return "sdkkonnect" + short
}

// testValuesForProperty returns Go literals suitable for populating a generated
// test struct field and asserting the marshaled SDK payload value.
func testValuesForProperty(prop *parser.Property, goType string) (string, string) {
	if prop.Type == "boolean" {
		return `"Enabled"`, "true"
	}

	switch goType {
	case "string":
		return `"test-value"`, `"test-value"`
	case "*string":
		return `new("test-value")`, `"test-value"`
	case "bool":
		return "true", "true"
	case "*bool":
		return `new(true)`, "true"
	case "int", "int32", "int64":
		return "1", "float64(1)"
	case "float32", "float64":
		return "1.0", "1.0"
	}

	if prop.RefName != "" {
		switch prop.Type {
		case "string":
			if elementType, ok := strings.CutPrefix(goType, "*"); ok {
				return fmt.Sprintf(`new(%s("test-value"))`, elementType), `"test-value"`
			}
			return fmt.Sprintf(`%s("test-value")`, goType), `"test-value"`
		case "object":
			if prop.AdditionalProperties != nil && prop.AdditionalProperties.Type == "string" {
				return fmt.Sprintf(`%s{"test-key": "test-value"}`, goType), `map[string]any{"test-key": "test-value"}`
			}
		}
	}
	// Skip complex types (maps, slices, structs, etc.) in generated tests
	return "", ""
}

// goType converts OpenAPI type to Go type.
func (g *Generator) goType(prop *parser.Property) string {
	// Handle references to other entities - convert to ObjectRef
	if prop.IsReference {
		return "*" + g.objectRefTypeName()
	}

	// Handle $ref
	if prop.RefName != "" {
		return fixInitialisms(prop.RefName)
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
		baseType = "string"
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
// and wraps lines longer than 80 characters.
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
// and wraps lines longer than 80 characters.
func formatSchemaComment(name, desc string) string {
	if desc == "" {
		return fmt.Sprintf("// %s is a type alias.\n", name)
	}
	lines := strings.Split(desc, "\n")
	var result []string
	// First line includes the type name
	firstLine := strings.TrimSpace(lines[0])
	if firstLine != "" {
		// Avoid stuttering: if the description already starts with the type
		// name, don't prepend it again.
		var firstLineWithName string
		if strings.HasPrefix(firstLine, name+" ") || strings.HasPrefix(firstLine, name+".") || firstLine == name {
			firstLineWithName = firstLine
		} else {
			firstLineWithName = fmt.Sprintf("%s %s", name, firstLine)
		}
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
	// Remove trailing empty comment lines
	for len(result) > 0 && result[len(result)-1] == "//" {
		result = result[:len(result)-1]
	}
	return strings.Join(result, "\n") + "\n"
}

// goFieldName converts property name to Go field name (PascalCase).
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

// fixInitialisms corrects common initialisms in PascalCase type names to follow
// Go naming conventions (e.g., "Http" → "HTTP", "Url" → "URL", "Id" → "ID").
func fixInitialisms(name string) string {
	words := splitPascalCase(name)
	for i, word := range words {
		upper := strings.ToUpper(word)
		if isCommonAcronym(upper) {
			words[i] = upper
		}
	}
	return strings.Join(words, "")
}

// splitPascalCase splits a PascalCase string into individual words.
// e.g., "CreateDcrConfigHttpInRequest" → ["Create", "Dcr", "Config", "Http", "In", "Request"].
func splitPascalCase(s string) []string {
	var words []string
	start := 0
	for i := 1; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			words = append(words, s[start:i])
			start = i
		}
	}
	if start < len(s) {
		words = append(words, s[start:])
	}
	return words
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

// jsonTag generates the json struct tag.
func jsonTag(prop *parser.Property) string {
	tag := prop.Name
	// K8s API best practice: all fields should have omitempty
	tag += ",omitempty"
	return tag
}

// isRefProperty checks if a property is a reference.
func isRefProperty(prop *parser.Property) bool {
	return prop.IsReference
}

// hasRootOneOf returns true if the schema has root-level oneOf (i.e., the schema itself is a union type).
func hasRootOneOf(schema *parser.Schema) bool {
	return len(schema.OneOf) > 0
}

// skipProperty returns true if the property should be skipped in CRD generation.
func skipProperty(prop *parser.Property) bool {
	// Skip read-only properties (they're typically server-managed like id, created_at, updated_at).
	if prop.ReadOnly {
		return true
	}
	// Skip id field as it's managed by Kubernetes.
	if prop.Name == "id" {
		return true
	}
	// Skip timestamp fields.
	if prop.Name == "created_at" || prop.Name == "updated_at" {
		return true
	}
	return false
}

// schemaUsesJSON checks if any property in the schema will be generated as apiextensionsv1.JSON.
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
