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
	// ManualGetForUIDEntities is the set of entity names for which a manual
	// getForUID function already exists and should still be included in the
	// generated cross-entity dispatcher.
	ManualGetForUIDEntities map[string]bool
}

// Generator generates Go CRD types from parsed OpenAPI schemas.
type Generator struct {
	config            Config
	opsCreateInfos    []*OpsCreateFileInfo
	opsUpdateInfos    []*OpsUpdateFileInfo
	opsDeleteInfos    []*OpsDeleteFileInfo
	opsGetForUIDInfos []*OpsGetForUIDFileInfo
	sdkFactoryInfos   []*SDKFactoryFileInfo
	watchInfos        []*WatchFileInfo
	// anyOfSchemaNames holds schema names whose Go type is an anyOf union struct.
	// Fields referencing these schemas must be pointers so omitempty omits zero values.
	anyOfSchemaNames map[string]bool
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

// SDKFactoryInfos returns metadata for every entity emitted by the most recent
// Generate call that has SDK factory config. Callers (e.g. the Runner) use it
// to assemble the cross-group SDK factory file after all group-versions have
// been generated.
func (g *Generator) SDKFactoryInfos() []*SDKFactoryFileInfo {
	return g.sdkFactoryInfos
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

	// Pre-compute the set of schema names whose Go type is an anyOf union struct.
	// These need pointer treatment at field sites so omitempty omits zero values.
	g.anyOfSchemaNames = make(map[string]bool)
	for name, schema := range parsed.Schemas {
		if len(schema.AnyOf) > 0 {
			g.anyOfSchemaNames[name] = true
		}
	}

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

		g.collectNamedReferencedSchemas(schema, referencedSchemas)
	}

	// Fixed-point expansion: schemas chosen for emission may themselves reference
	// further named schemas via their own properties / oneOf variants.
	worklist := make([]string, 0, len(referencedSchemas))
	for name := range referencedSchemas {
		worklist = append(worklist, name)
	}
	for len(worklist) > 0 {
		name := worklist[len(worklist)-1]
		worklist = worklist[:len(worklist)-1]
		schema, ok := parsed.Schemas[name]
		if !ok {
			continue
		}
		before := len(referencedSchemas)
		for _, prop := range schema.Properties {
			g.collectNamedRefsFromProperty(prop, referencedSchemas)
		}
		for _, variant := range schema.OneOf {
			g.collectNamedRefsFromProperty(variant, referencedSchemas)
		}
		if len(referencedSchemas) > before {
			for n := range referencedSchemas {
				worklist = append(worklist, n)
			}
		}
	}

	reconcilerFiles, err := g.generateReconcilerEntityFiles(reconcilerEntities, parsed)
	if err != nil {
		return nil, err
	}
	files = append(files, reconcilerFiles...)

	// Resolve nested CEL paths (e.g. tls.client_identity.certificate) into a
	// schema-type-level field config so that generateSchemaTypes can apply
	// user-provided markers to fields on referenced shared types.
	schemaTypeFieldConfig := g.buildSchemaTypeFieldConfig(parsed)

	sharedFiles, err := g.generateSharedFiles(parsed, referencedSchemas, schemaTypeFieldConfig)
	if err != nil {
		return nil, err
	}
	files = append(files, sharedFiles...)

	return files, nil
}

// buildSchemaTypeFieldConfig resolves nested CEL config paths from entity configs into
// a flat schema-type-keyed Config. For example, an entity config entry
// `EventGatewayBackendCluster.cel.tls.client_identity.certificate._validations`
// resolves — by walking the parsed OAS schemas via $ref chains — to
// `ClientIdentity.certificate._validations` in the returned Config, which
// generateSchemaTypes can then apply when emitting the shared ClientIdentity struct.
func (g *Generator) buildSchemaTypeFieldConfig(parsed *parser.ParsedSpec) *config.Config {
	if g.config.FieldConfig == nil {
		return nil
	}
	entities := make(map[string]*config.EntityConfig)
	for entityName, entityCfg := range g.config.FieldConfig.Entities {
		if entityCfg == nil || len(entityCfg.Fields) == 0 {
			continue
		}
		// Find the entity's schema in the parsed request bodies.
		var entitySchema *parser.Schema
		for _, schema := range parsed.RequestBodies {
			if parser.GetEntityNameFromType(schema.Name) == entityName {
				entitySchema = schema
				break
			}
		}
		if entitySchema == nil {
			continue
		}
		// Entity-level fields (e.g. "tls") that have sub-config but no own Validations
		// are path-segment nodes pointing into referenced schema types. Descend into
		// any referenced schema to collect leaf validations there.
		collectSchemaTypeValidations(entityCfg.Fields, entitySchema.Properties, parsed.Schemas, entities)

		// Also handle root-level oneOf/anyOf variants (entity is a discriminated union).
		// Config keys that match a variant ref name (e.g. "EventGatewayTLSListenerPolicy")
		// are treated as descents into that variant's schema.
		for _, variant := range append(entitySchema.OneOf, entitySchema.AnyOf...) {
			if variant.RefName == "" {
				continue
			}
			fieldCfg, ok := entityCfg.Fields[variant.RefName]
			if !ok || fieldCfg == nil || len(fieldCfg.Fields) == 0 {
				continue
			}
			variantSchema, ok := parsed.Schemas[variant.RefName]
			if !ok || variantSchema == nil {
				continue
			}
			// Use addSchemaFieldValidations (not collectSchemaTypeValidations) so that
			// leaf validations directly on variant properties are recorded.
			addSchemaFieldValidations(variant.RefName, fieldCfg.Fields, variantSchema.Properties, parsed.Schemas, entities)
		}
	}
	if len(entities) == 0 {
		return nil
	}
	return &config.Config{Entities: entities}
}

// collectSchemaTypeValidations walks cfgFields alongside the matching props slice.
// When a config field has sub-fields and its matching prop points to a referenced
// schema ($ref or array-of-$ref), it recurses into that schema via addSchemaFieldValidations.
// Entity-level validations (on top-level entity template fields) are NOT recorded
// here because they are already handled by the entity template config.
func collectSchemaTypeValidations(
	cfgFields map[string]*config.FieldConfig,
	props []*parser.Property,
	schemas map[string]*parser.Schema,
	out map[string]*config.EntityConfig,
) {
	for fieldName, fieldCfg := range cfgFields {
		if fieldCfg == nil || len(fieldCfg.Fields) == 0 {
			continue
		}
		prop := findPropertyByName(props, fieldName)
		if prop == nil {
			continue
		}
		switch {
		case prop.RefName != "":
			refSchema, ok := schemas[prop.RefName]
			if !ok || refSchema == nil {
				continue
			}
			addSchemaFieldValidations(prop.RefName, fieldCfg.Fields, refSchema.Properties, schemas, out)
		case prop.Type == "array" && prop.Items != nil && prop.Items.RefName != "":
			refSchema, ok := schemas[prop.Items.RefName]
			if !ok || refSchema == nil {
				continue
			}
			addSchemaFieldValidations(prop.Items.RefName, fieldCfg.Fields, refSchema.Properties, schemas, out)
		}
	}
}

// addSchemaFieldValidations records leaf validations from cfgFields into out,
// keyed by schemaName. Sub-fields that themselves point into further referenced
// schemas are recursed into.
func addSchemaFieldValidations(
	schemaName string,
	cfgFields map[string]*config.FieldConfig,
	props []*parser.Property,
	schemas map[string]*parser.Schema,
	out map[string]*config.EntityConfig,
) {
	for fieldName, fieldCfg := range cfgFields {
		if fieldCfg == nil {
			continue
		}
		prop := findPropertyByName(props, fieldName)
		if prop == nil {
			continue
		}
		if len(fieldCfg.Validations) > 0 {
			entityCfg, ok := out[schemaName]
			if !ok {
				entityCfg = &config.EntityConfig{Fields: make(map[string]*config.FieldConfig)}
				out[schemaName] = entityCfg
			}
			entityCfg.Fields[fieldName] = &config.FieldConfig{Validations: fieldCfg.Validations}
		}
		if len(fieldCfg.Fields) > 0 {
			switch {
			case prop.RefName != "":
				// $ref prop: descend into the referenced schema.
				refSchema, ok := schemas[prop.RefName]
				if ok && refSchema != nil {
					addSchemaFieldValidations(prop.RefName, fieldCfg.Fields, refSchema.Properties, schemas, out)
				}
			case prop.Type == "object" && len(prop.Properties) > 0:
				// Inline anonymous object: the generator emits it as a named Go struct
				// using the Pascal-case field name (e.g. client_identity → ClientIdentity).
				inlineTypeName := goFieldName(prop.Name)
				addSchemaFieldValidations(inlineTypeName, fieldCfg.Fields, prop.Properties, schemas, out)
			case prop.Type == "array" && prop.Items != nil && prop.Items.RefName != "":
				// Array of $ref items: descend into the item schema. Validations are keyed
				// by the item ref name, matching the generated Go slice element type name.
				refSchema, ok := schemas[prop.Items.RefName]
				if ok && refSchema != nil {
					addSchemaFieldValidations(prop.Items.RefName, fieldCfg.Fields, refSchema.Properties, schemas, out)
				}
			}
		}
	}
}

// findPropertyByName returns the property with the given name from a slice, or nil.
func findPropertyByName(props []*parser.Property, name string) *parser.Property {
	for _, p := range props {
		if p.Name == name {
			return p
		}
	}
	return nil
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

	if testsContent := g.generateCRDTypeTests(entityName, schema); testsContent != "" {
		files = append(files, GeneratedFile{
			Name:    commonGeneratedFilePrefix + EntityFilePrefix(entityName) + "_types_test.go",
			Content: testsContent,
		})
	}

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
	if opsResult.SDKFactoryInfo != nil {
		g.sdkFactoryInfos = append(g.sdkFactoryInfos, opsResult.SDKFactoryInfo)
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
// groupversion_info.go, doc.go, common_types.go, reconciler condition constants,
// and schema_types.go.
func (g *Generator) generateSharedFiles(parsed *parser.ParsedSpec, referencedSchemas map[string]bool, schemaTypeFieldConfig *config.Config) ([]GeneratedFile, error) {
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

	reconcilerConditionsFile, err := g.generateReconcilerConditions(parsed)
	if err != nil {
		return nil, fmt.Errorf("failed to generate reconciler condition constants: %w", err)
	}
	if reconcilerConditionsFile != nil {
		files = append(files, *reconcilerConditionsFile)
	}

	if len(referencedSchemas) > 0 {
		files = append(files, GeneratedFile{
			Name:    "schema_types.go",
			Content: g.generateSchemaTypes(referencedSchemas, parsed, schemaTypeFieldConfig),
		})

		if testsContent := g.generateSchemaTypesTests(referencedSchemas, parsed); testsContent != "" {
			files = append(files, GeneratedFile{
				Name:    "schema_types_test.go",
				Content: testsContent,
			})
		}
	}

	return files, nil
}

// collectNamedReferencedSchemas collects refs whose names will appear in
// generated Go code, skipping array-typed $ref properties that goType inlines
// as []<element> (leaving the alias unused).
func (g *Generator) collectNamedReferencedSchemas(schema *parser.Schema, refs map[string]bool) {
	for _, prop := range schema.Properties {
		g.collectNamedRefsFromProperty(prop, refs)
	}
	for _, variant := range schema.OneOf {
		g.collectNamedRefsFromProperty(variant, refs)
	}
	for _, variant := range schema.AnyOf {
		g.collectNamedRefsFromProperty(variant, refs)
	}
}

// collectNamedRefsFromProperty mirrors goType's naming decisions: array-typed
// $ref properties are inlined as []<element> so the array ref itself is not
// recorded — only its item ref (via recursion) is.
func (g *Generator) collectNamedRefsFromProperty(prop *parser.Property, refs map[string]bool) {
	if skipProperty(prop) {
		return
	}
	if prop.RefName != "" && !prop.IsReference {
		// goType inlines array-typed $refs as []<element>; the alias is never used.
		if prop.Type != "array" || prop.Items == nil {
			refs[prop.RefName] = true
		}
	}
	if prop.Items != nil {
		g.collectNamedRefsFromProperty(prop.Items, refs)
	}
	for _, nested := range prop.Properties {
		g.collectNamedRefsFromProperty(nested, refs)
	}
	if prop.AdditionalProperties != nil {
		g.collectNamedRefsFromProperty(prop.AdditionalProperties, refs)
	}
	for _, variant := range prop.OneOf {
		if variant.RefName != "" {
			refs[variant.RefName] = true
		}
		g.collectNamedRefsFromProperty(variant, refs)
	}
	for _, variant := range prop.AnyOf {
		if variant.RefName != "" {
			refs[variant.RefName] = true
		}
		g.collectNamedRefsFromProperty(variant, refs)
	}
}

func collectUnionMemberDiscriminators(parsed *parser.ParsedSpec) map[string]map[string]struct{} {
	unionMemberDiscriminators := make(map[string]map[string]struct{})

	for _, schema := range parsed.RequestBodies {
		collectSchemaUnionMemberDiscriminators(schema, unionMemberDiscriminators)
	}
	for _, schema := range parsed.Schemas {
		collectSchemaUnionMemberDiscriminators(schema, unionMemberDiscriminators)
	}

	return unionMemberDiscriminators
}

func collectSchemaUnionMemberDiscriminators(schema *parser.Schema, unionMemberDiscriminators map[string]map[string]struct{}) {
	if schema == nil {
		return
	}

	addUnionMemberDiscriminators(schema.Discriminator, schema.OneOf, unionMemberDiscriminators)
	addUnionMemberDiscriminators(schema.Discriminator, schema.AnyOf, unionMemberDiscriminators)

	for _, prop := range schema.Properties {
		collectPropertyUnionMemberDiscriminators(prop, unionMemberDiscriminators)
	}
	collectPropertyUnionMemberDiscriminators(schema.Items, unionMemberDiscriminators)
	collectPropertyUnionMemberDiscriminators(schema.AdditionalProperties, unionMemberDiscriminators)
}

func collectPropertyUnionMemberDiscriminators(prop *parser.Property, unionMemberDiscriminators map[string]map[string]struct{}) {
	if prop == nil {
		return
	}

	addUnionMemberDiscriminators(prop.Discriminator, prop.OneOf, unionMemberDiscriminators)
	addUnionMemberDiscriminators(prop.Discriminator, prop.AnyOf, unionMemberDiscriminators)

	for _, nested := range prop.Properties {
		collectPropertyUnionMemberDiscriminators(nested, unionMemberDiscriminators)
	}
	collectPropertyUnionMemberDiscriminators(prop.Items, unionMemberDiscriminators)
	collectPropertyUnionMemberDiscriminators(prop.AdditionalProperties, unionMemberDiscriminators)
	for _, variant := range prop.OneOf {
		collectPropertyUnionMemberDiscriminators(variant, unionMemberDiscriminators)
	}
	for _, variant := range prop.AnyOf {
		collectPropertyUnionMemberDiscriminators(variant, unionMemberDiscriminators)
	}
}

func addUnionMemberDiscriminators(discriminator string, variants []*parser.Property, unionMemberDiscriminators map[string]map[string]struct{}) {
	if discriminator == "" {
		return
	}

	for _, variant := range variants {
		if variant == nil || variant.RefName == "" {
			continue
		}
		if _, ok := unionMemberDiscriminators[variant.RefName]; !ok {
			unionMemberDiscriminators[variant.RefName] = make(map[string]struct{})
		}
		unionMemberDiscriminators[variant.RefName][discriminator] = struct{}{}
	}
}

func shouldSkipUnionMemberDiscriminator(refName string, prop *parser.Property, unionMemberDiscriminators map[string]map[string]struct{}) bool {
	if prop == nil || prop.Name == "" {
		return false
	}

	discriminators, ok := unionMemberDiscriminators[refName]
	if !ok {
		return false
	}

	_, ok = discriminators[prop.Name]
	return ok
}

// generateSchemaTypes generates Go type definitions for referenced schemas.
func (g *Generator) generateSchemaTypes(refs map[string]bool, parsed *parser.ParsedSpec, schemaTypeFieldConfig *config.Config) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "%s\n\npackage %s\n\n", sharedGeneratedFilePreamble, g.config.APIVersion)
	unionMemberDiscriminators := collectUnionMemberDiscriminators(parsed)

	// Sort keys to ensure deterministic output order
	refNames := make([]string, 0, len(refs))
	for refName := range refs {
		refNames = append(refNames, refName)
	}
	sort.Strings(refNames)

	if needsAPIExtJSON, needsIntStr, needsEncodingJSON, objectRefImport := g.schemaTypesImports(refNames, parsed); needsAPIExtJSON || needsIntStr || needsEncodingJSON || objectRefImport != nil {
		buf.WriteString("import (\n")
		if needsEncodingJSON {
			buf.WriteString("\t\"encoding/json\"\n")
			buf.WriteString("\t\"fmt\"\n")
		}
		if needsAPIExtJSON {
			buf.WriteString("\tapiextensionsv1 \"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1\"\n")
		}
		if needsIntStr {
			buf.WriteString("\tintstr \"k8s.io/apimachinery/pkg/util/intstr\"\n")
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

	// emittedNested tracks inline-object type names already emitted, so a field
	// shape reused across multiple parent schemas only produces one definition.
	emittedNested := make(map[string]bool)
	for _, refName := range refNames {
		emittedNested[fixInitialisms(refName)] = true
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
					if skipProperty(prop) || shouldSkipUnionMemberDiscriminator(refName, prop, unionMemberDiscriminators) {
						continue
					}
					g.writeSchemaTypeField(&buf, prop, goName, schemaTypeFieldConfig)
				}
				buf.WriteString("}\n\n")
				g.writeNestedInlineTypes(&buf, schema.Properties, emittedNested, schemaTypeFieldConfig)
				// Emit union type definitions for any property-level oneOf.
				for _, prop := range schema.Properties {
					if skipProperty(prop) || len(prop.OneOf) == 0 {
						continue
					}
					buf.WriteString(g.generateUnionType(prop, goName))
				}
				if wrapper := emitUnionWrapperUnmarshalJSON(goName, buildUnionFieldSpecs(schema.Properties, goName)); wrapper != "" {
					buf.WriteString(wrapper)
				}
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

			case isScalarStringIntOneOf(schema.OneOf):
				// Root-level oneOf of exactly {string, integer} — emit a Go type alias for
				// intstr.IntOrString. Alias (not named type) ensures controller-gen recognises
				// it as IntOrString and emits anyOf:[integer,string] in the CRD schema.
				buf.WriteString(comment)
				buf.WriteString("//\n// +kubebuilder:validation:XIntOrString\n")
				fmt.Fprintf(&buf, "type %s = intstr.IntOrString\n\n", goName)

			case hasRefVariants(schema.OneOf) && schema.Discriminator != "":
				// Root-level oneOf + OAS discriminator: emit a flat discriminated union
				// wrapper struct with custom MarshalJSON/UnmarshalJSON so the wire JSON
				// matches the SDK's expected flat shape.
				buf.WriteString(g.emitDiscriminatedUnionType(goName, schema))

			case hasRefVariants(schema.AnyOf):
				// Root-level anyOf without discriminator: emit a wrapper struct with one
				// optional pointer per variant, with MinProperties=1 / MaxProperties=1.
				buf.WriteString(g.emitAnyOfUnionType(goName, schema))

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

func (g *Generator) schemaTypesImports(refNames []string, parsed *parser.ParsedSpec) (needsAPIExtJSON, needsIntStr, needsEncodingJSON bool, objectRefImport *config.ImportConfig) {
	for _, refName := range refNames {
		schema, ok := parsed.Schemas[refName]
		if !ok {
			continue
		}

		if !needsAPIExtJSON && schemaUsesJSON(g, schema) {
			needsAPIExtJSON = true
		}
		if !needsIntStr && isScalarStringIntOneOf(schema.OneOf) {
			needsIntStr = true
		}
		if !needsEncodingJSON && (hasRefVariants(schema.OneOf) || hasRefVariants(schema.AnyOf)) {
			needsEncodingJSON = true
		}
		if objectRefImport == nil {
			objectRefImport = g.objectRefImportIfNeeded(schema)
		}

		if needsAPIExtJSON && needsIntStr && needsEncodingJSON && objectRefImport != nil {
			break
		}
	}
	return
}

func (g *Generator) writeSchemaTypeField(buf *strings.Builder, prop *parser.Property, typeName string, fieldConfig *config.Config) {
	buf.WriteString(formatComment(prop.Description))
	buf.WriteString("\n")
	buf.WriteString("\t//\n")
	for _, tag := range KubebuilderTags(prop, typeName, fieldConfig) {
		fmt.Fprintf(buf, "\t// %s\n", tag)
	}
	goType := g.goType(prop)
	if prop.RefName != "" && g.anyOfSchemaNames[prop.RefName] {
		goType = "*" + fixInitialisms(prop.RefName)
	}
	// For schema types, oneOf properties use entity-prefixed type names to avoid
	// package-scoped collisions (e.g. bare "Config" would clash across entities).
	if len(prop.OneOf) > 0 {
		goType = "*" + typeName + goFieldName(prop.Name)
	}
	fmt.Fprintf(buf, "\t%s %s `json:\"%s\"`\n", goFieldName(prop.Name), goType, jsonTag(prop))
}

// writeNestedInlineTypes emits Go type definitions for any property that is an
// inline object (Type=="object" with sub-Properties and no $ref). This covers
// schemas like BackendClusterTLS.client_identity, where the OpenAPI spec
// declares the nested object inline rather than via $ref. Without this,
// generateSchemaTypes would reference the type by name (e.g. ClientIdentity)
// without ever defining it, producing uncompilable Go.
//
// emitted is a shared set used to dedupe across all parent schemas: a type
// name is only emitted once per package. Recurses into nested objects so
// arbitrarily deep inline shapes are handled.
func (g *Generator) writeNestedInlineTypes(buf *strings.Builder, props []*parser.Property, emitted map[string]bool, schemaTypeFieldConfig *config.Config) {
	for _, prop := range props {
		if skipProperty(prop) || prop == nil {
			continue
		}
		if prop.Items != nil {
			g.writeNestedInlineTypes(buf, []*parser.Property{prop.Items}, emitted, schemaTypeFieldConfig)
		}
		if !isInlineObjectWithProperties(prop) {
			continue
		}
		typeName := goFieldName(prop.Name)
		if emitted[typeName] {
			g.writeNestedInlineTypes(buf, prop.Properties, emitted, schemaTypeFieldConfig)
			continue
		}
		emitted[typeName] = true

		buf.WriteString(formatSchemaComment(typeName, prop.Description))
		fmt.Fprintf(buf, "type %s struct {\n", typeName)
		for _, nested := range prop.Properties {
			if skipProperty(nested) {
				continue
			}
			g.writeSchemaTypeField(buf, nested, typeName, schemaTypeFieldConfig)
		}
		buf.WriteString("}\n\n")

		g.writeNestedInlineTypes(buf, prop.Properties, emitted, schemaTypeFieldConfig)
	}
}

// isInlineObjectWithProperties returns true for object-typed properties whose
// shape is declared inline (sub-properties present) rather than via a $ref or
// a oneOf union. These need a generated Go struct definition because goType
// returns the field name as the type name.
func isInlineObjectWithProperties(prop *parser.Property) bool {
	if prop.Type != "object" {
		return false
	}
	if prop.RefName != "" {
		return false
	}
	if len(prop.OneOf) > 0 {
		return false
	}
	if prop.AdditionalProperties != nil {
		return false
	}
	return len(prop.Properties) > 0
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
		if schema.Items != nil {
			if schema.Items.RefName != "" {
				return "[]" + fixInitialisms(schema.Items.RefName)
			}
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

// hasRefVariants reports whether the variant list contains at least one $ref variant.
// This distinguishes ref-bearing union types (which need a wrapper struct) from
// scalar unions (e.g. string|integer).
func hasRefVariants(variants []*parser.Property) bool {
	for _, v := range variants {
		if v.RefName != "" {
			return true
		}
	}
	return false
}

// isScalarStringIntOneOf reports whether variants form the narrow {string, integer}
// scalar union for which intstr.IntOrString is the idiomatic K8s representation.
// Returns true only when there are exactly two variants, their types are exactly
// "string" and "integer" (in either order), and neither has a $ref or sub-properties.
func isScalarStringIntOneOf(variants []*parser.Property) bool {
	if len(variants) != 2 {
		return false
	}
	types := make(map[string]bool, 2)
	for _, v := range variants {
		if v.RefName != "" || len(v.Properties) > 0 {
			return false
		}
		types[v.Type] = true
	}
	return types["string"] && types["integer"]
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
	// Also include root oneOf/anyOf variant ref names (discriminated unions).
	for _, v := range schema.OneOf {
		if v.RefName != "" {
			validFields[v.RefName] = struct{}{}
		}
	}
	for _, v := range schema.AnyOf {
		if v.RefName != "" {
			validFields[v.RefName] = struct{}{}
		}
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
	// anyOf union refs also need pointer treatment so omitempty omits zero values.
	goTypeInCRD := func(prop *parser.Property) string {
		if len(prop.OneOf) > 0 {
			return "*" + entityName + goFieldName(prop.Name)
		}
		if prop.RefName != "" && g.anyOfSchemaNames[prop.RefName] {
			return "*" + fixInitialisms(prop.RefName)
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
		hasRootReconciler = rc.GetIsRoot()
	}

	// Detect whether the entity schema has property-level oneOf unions (which
	// produce MarshalJSON/UnmarshalJSON methods that need encoding/json + fmt).
	hasUnionTypes := len(schema.OneOf) > 0
	if !hasUnionTypes {
		for _, prop := range schema.Properties {
			if len(prop.OneOf) > 0 {
				hasUnionTypes = true
				break
			}
		}
	}

	var buf strings.Builder
	data := struct {
		EntityName                string
		Schema                    *parser.Schema
		APIGroup                  string
		APIVersion                string
		NeedsJSONImport           bool
		HasUnionTypes             bool
		ObjectRefImport           *config.ImportConfig
		HasOptionalSecretRef      bool
		HasRootReconciler         bool
		ImmediateParentDependency *parser.Dependency
	}{
		EntityName:                entityName,
		Schema:                    schema,
		APIGroup:                  g.config.APIGroup,
		APIVersion:                g.config.APIVersion,
		NeedsJSONImport:           schemaUsesJSON(g, schema),
		HasUnionTypes:             hasUnionTypes,
		ObjectRefImport:           objectRefImport,
		HasOptionalSecretRef:      hasOptionalSecretRef,
		HasRootReconciler:         hasRootReconciler,
		ImmediateParentDependency: rootRefDependency(schema),
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

	apiSpecUnionFields := buildCRDAPISpecUnionFieldSpecs(schema)
	if wrapper := emitInlineUnionWrapperMarshalJSON(entityName+"APISpec", apiSpecUnionFields); wrapper != "" {
		buf.WriteString("\n")
		buf.WriteString(wrapper)
	}

	if wrapper := emitUnionWrapperUnmarshalJSON(entityName+"APISpec", apiSpecUnionFields); wrapper != "" {
		buf.WriteString("\n")
		buf.WriteString(wrapper)
	}

	// Post-process to remove trailing empty lines before closing braces in structs
	result := fixTrailingEmptyLines(buf.String())

	return result, nil
}

func (g *Generator) generateCRDTypeTests(entityName string, schema *parser.Schema) string {
	unionSpecs := buildCRDAPISpecUnionFieldSpecs(schema)
	if len(unionSpecs) == 0 {
		return ""
	}

	return emitUnionTests(g.config.APIVersion, unionSpecs, []unionWrapperTestSpec{{
		StructTypeName: entityName + "APISpec",
		Fields:         unionSpecs,
	}})
}

func (g *Generator) generateSchemaTypesTests(refs map[string]bool, parsed *parser.ParsedSpec) string {
	refNames := make([]string, 0, len(refs))
	for refName := range refs {
		refNames = append(refNames, refName)
	}
	sort.Strings(refNames)

	unionSpecs := make([]unionFieldSpec, 0)
	seenUnionTypes := make(map[string]struct{})
	wrapperSpecs := make([]unionWrapperTestSpec, 0)

	for _, refName := range refNames {
		schema, ok := parsed.Schemas[refName]
		if !ok {
			continue
		}

		goName := fixInitialisms(refName)
		if hasRefVariants(schema.OneOf) && schema.Discriminator != "" {
			rootSpec := buildUnionFieldSpec(goName, goName, refName, &parser.Property{
				Name:                 goName,
				OneOf:                schema.OneOf,
				Discriminator:        schema.Discriminator,
				DiscriminatorMapping: schema.DiscriminatorMapping,
			})
			unionSpecs = appendUniqueUnionFieldSpec(unionSpecs, seenUnionTypes, rootSpec)
		}

		fields := buildUnionFieldSpecs(schema.Properties, goName)
		if len(fields) == 0 {
			continue
		}
		for _, field := range fields {
			unionSpecs = appendUniqueUnionFieldSpec(unionSpecs, seenUnionTypes, field)
		}
		wrapperSpecs = append(wrapperSpecs, unionWrapperTestSpec{
			StructTypeName: goName,
			Fields:         fields,
		})
	}

	return emitUnionTests(g.config.APIVersion, unionSpecs, wrapperSpecs)
}

func (g *Generator) generateCRDFuncs(name string, schema *parser.Schema) (string, error) {
	entityName := parser.GetEntityNameFromType(name)
	isReconcilerRoot := false
	if rc := g.config.ReconcilerConfig[entityName]; rc != nil {
		isReconcilerRoot = rc.GetIsRoot()
	}
	rootRefDependency := rootRefDependency(schema)

	imports := make([]*config.ImportConfig, 0, 3)
	imports = appendUniqueImportConfig(imports, defaultKonnectStatusImport())
	imports = appendUniqueImportConfig(imports, &config.ImportConfig{
		Alias: "metav1",
		Path:  "k8s.io/apimachinery/pkg/apis/meta/v1",
	})
	if rootRefDependency != nil && g.objectRefImported() {
		imports = appendUniqueImportConfig(imports, g.config.CommonTypes.ObjectRef.Import)
	}
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
		RootRefDependency                  *parser.Dependency
		RootRefAccessorEntityName          string
		RootRefTypeName                    string
		IsReconcilerRoot                   bool
		KonnectAPIAuthConfigurationRefType string
	}{
		EntityName:                         entityName,
		APIVersion:                         g.config.APIVersion,
		Imports:                            imports,
		KonnectStatusType:                  defaultKonnectStatusQualifiedTypeName(),
		KonnectLabelsField:                 g.konnectLabelsField(schema),
		Dependencies:                       schema.Dependencies,
		RootRefDependency:                  rootRefDependency,
		RootRefAccessorEntityName:          rootRefAccessorEntityName(rootRefDependency),
		RootRefTypeName:                    g.objectRefTypeName(),
		IsReconcilerRoot:                   isReconcilerRoot,
		KonnectAPIAuthConfigurationRefType: defaultKonnectStatusAlias + ".ControlPlaneKonnectAPIAuthConfigurationRef",
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return fixTrailingEmptyLines(buf.String()), nil
}

// rootRefDependency returns the immediate (last) parent dependency — the one
// that drives Spec.<X>Ref, the GetXxxRef accessor, and the auth-ref lookup.
// For single-parent entities this is the only dependency. For multi-parent
// entities (e.g. EventGatewayListenerPolicy) this is the innermost parent
// (e.g. EventGatewayListener), NOT the outermost.
func rootRefDependency(schema *parser.Schema) *parser.Dependency {
	if len(schema.Dependencies) == 0 {
		return nil
	}
	return schema.Dependencies[len(schema.Dependencies)-1]
}

func rootRefAccessorEntityName(dep *parser.Dependency) string {
	if dep == nil {
		return ""
	}
	if dep.AccessorEntityName != "" {
		return dep.AccessorEntityName
	}
	return dep.EntityName
}

// refConditionEntityName picks the prefix used for generated ref-condition
// constants from a parent dependency. It prefers the accessor alias (e.g.
// "EventGateway" instead of the raw spec entity "Gateway"), but falls back to
// the entity name when the entity already embeds the accessor as a prefix or
// suffix (e.g. EntityName "EventGatewayListener" with accessor "Listener" stays
// as "EventGatewayListener").
func refConditionEntityName(dep *parser.Dependency) string {
	if dep.AccessorEntityName == "" || dep.AccessorEntityName == dep.EntityName {
		return dep.EntityName
	}
	if strings.HasSuffix(dep.EntityName, dep.AccessorEntityName) || strings.HasPrefix(dep.EntityName, dep.AccessorEntityName) {
		return dep.EntityName
	}
	return dep.AccessorEntityName
}

// EntityFilePrefix converts a PascalCase entity name to a lowercase file name
// prefix, inserting an underscore after a leading "Konnect" prefix.
// e.g. "KonnectEventGateway" → "konnect_eventcontrolplane",
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
	return g.generateUnionType(buildRootUnionProperty(schema), "")
}

// unionVariant holds the discriminator value and ref name for one union variant.
type unionVariant struct {
	discValue string // OAS discriminator value, e.g. "sasl_plain" — used for JSON tag and enum const
	refName   string // OAS component schema name, e.g. "BackendClusterAuthenticationSaslPlain"
}

// buildUnionVariants builds the ordered list of variants for a property-level oneOf
// union. Uses the OAS discriminator mapping when present (for correct snake_case
// values); falls back to extractVariantNames when no discriminator is available.
func buildUnionVariants(prop *parser.Property) []unionVariant {
	if len(prop.DiscriminatorMapping) > 0 {
		values := make([]string, 0, len(prop.DiscriminatorMapping))
		for v := range prop.DiscriminatorMapping {
			values = append(values, v)
		}
		sort.Strings(values)
		variants := make([]unionVariant, 0, len(values))
		for _, v := range values {
			variants = append(variants, unionVariant{discValue: v, refName: prop.DiscriminatorMapping[v]})
		}
		return variants
	}

	rawNames := make([]string, 0, len(prop.OneOf))
	for _, v := range prop.OneOf {
		name := v.Name
		if v.RefName != "" {
			name = v.RefName
		}
		rawNames = append(rawNames, name)
	}
	cleanNames := extractVariantNames(rawNames)
	variants := make([]unionVariant, 0, len(prop.OneOf))
	for i, v := range prop.OneOf {
		refName := v.Name
		if v.RefName != "" {
			refName = v.RefName
		}
		variants = append(variants, unionVariant{discValue: cleanNames[i], refName: refName})
	}
	return variants
}

// generateUnionType generates a single union type struct for a property-level oneOf.
// typeNamePrefix is prepended to the generated Go type name to avoid
// package-scoped collisions for common property names like "config".
func (g *Generator) generateUnionType(prop *parser.Property, typeNamePrefix string) string {
	typeName := generatedUnionTypeName(prop, typeNamePrefix)
	variants := buildUnionVariants(prop)
	return emitDiscriminatedUnionCode(typeName, prop.Name, variants)
}

type unionFieldVariant struct {
	DiscValue string
	FieldName string
	TypeConst string
}

type unionFieldSpec struct {
	FieldName string
	JSONName  string
	Inline    bool
	TypeName  string
	Variants  []unionFieldVariant
}

type unionWrapperTestSpec struct {
	StructTypeName string
	Fields         []unionFieldSpec
}

func (s unionWrapperTestSpec) inlineField() (unionFieldSpec, bool) {
	for _, field := range s.Fields {
		if field.Inline {
			return field, true
		}
	}
	return unionFieldSpec{}, false
}

func buildRootUnionProperty(schema *parser.Schema) *parser.Property {
	entityName := parser.GetEntityNameFromType(schema.Name)
	return &parser.Property{
		Name:                 entityName + "Config",
		OneOf:                schema.OneOf,
		Discriminator:        schema.Discriminator,
		DiscriminatorMapping: schema.DiscriminatorMapping,
	}
}

func generatedUnionTypeName(prop *parser.Property, typeNamePrefix string) string {
	return typeNamePrefix + goFieldName(prop.Name)
}

func buildUnionFieldVariants(variants []unionVariant, typeName string) []unionFieldVariant {
	rawRefNames := make([]string, 0, len(variants))
	for _, v := range variants {
		rawRefNames = append(rawRefNames, v.refName)
	}
	cleanFieldNames := extractVariantNames(rawRefNames)

	result := make([]unionFieldVariant, 0, len(variants))
	for i, v := range variants {
		fieldName := fixInitialisms(cleanFieldNames[i])
		result = append(result, unionFieldVariant{
			DiscValue: v.discValue,
			FieldName: fieldName,
			TypeConst: typeName + "Type" + fieldName,
		})
	}

	return result
}

func buildUnionFieldSpec(fieldName, typeName, jsonName string, prop *parser.Property) unionFieldSpec {
	return unionFieldSpec{
		FieldName: fieldName,
		JSONName:  jsonName,
		Inline:    jsonName == "",
		TypeName:  typeName,
		Variants:  buildUnionFieldVariants(buildUnionVariants(prop), typeName),
	}
}

func buildUnionFieldSpecs(props []*parser.Property, typeNamePrefix string) []unionFieldSpec {
	fields := make([]unionFieldSpec, 0)
	for _, prop := range props {
		if skipProperty(prop) || len(prop.OneOf) == 0 {
			continue
		}
		fields = append(fields, buildUnionFieldSpec(
			goFieldName(prop.Name),
			generatedUnionTypeName(prop, typeNamePrefix),
			prop.Name,
			prop,
		))
	}
	return fields
}

func buildCRDAPISpecUnionFieldSpecs(schema *parser.Schema) []unionFieldSpec {
	fields := make([]unionFieldSpec, 0, len(schema.Properties)+1)
	if len(schema.OneOf) > 0 {
		rootProp := buildRootUnionProperty(schema)
		typeName := generatedUnionTypeName(rootProp, "")
		fields = append(fields, buildUnionFieldSpec(typeName, typeName, "", rootProp))
	}
	fields = append(fields, buildUnionFieldSpecs(schema.Properties, parser.GetEntityNameFromType(schema.Name))...)
	return fields
}

func appendUniqueUnionFieldSpec(dst []unionFieldSpec, seen map[string]struct{}, spec unionFieldSpec) []unionFieldSpec {
	if _, ok := seen[spec.TypeName]; ok {
		return dst
	}
	seen[spec.TypeName] = struct{}{}
	return append(dst, spec)
}

func emitInlineUnionWrapperMarshalJSON(structTypeName string, fields []unionFieldSpec) string {
	var inlineField *unionFieldSpec
	for i := range fields {
		if fields[i].Inline {
			inlineField = &fields[i]
			break
		}
	}
	if inlineField == nil {
		return ""
	}

	var buf strings.Builder
	buf.WriteString("// MarshalJSON implements json.Marshaler.\n")
	fmt.Fprintf(&buf, "func (s *%s) MarshalJSON() ([]byte, error) {\n", structTypeName)
	buf.WriteString("\tif s == nil {\n")
	buf.WriteString("\t\treturn []byte(\"null\"), nil\n")
	buf.WriteString("\t}\n")
	fmt.Fprintf(&buf, "\tif s.%s == nil {\n", inlineField.FieldName)
	buf.WriteString("\t\treturn []byte(\"{}\"), nil\n")
	buf.WriteString("\t}\n")
	fmt.Fprintf(&buf, "\tdata, err := json.Marshal(s.%s)\n", inlineField.FieldName)
	buf.WriteString("\tif err != nil {\n")
	fmt.Fprintf(&buf, "\t\treturn nil, fmt.Errorf(\"marshaling %s: %%w\", err)\n", structTypeName)
	buf.WriteString("\t}\n")
	buf.WriteString("\treturn data, nil\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

func emitUnionWrapperUnmarshalJSON(structTypeName string, fields []unionFieldSpec) string {
	if len(fields) == 0 {
		return ""
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "// UnmarshalJSON implements json.Unmarshaler.\n")
	fmt.Fprintf(&buf, "func (s *%s) UnmarshalJSON(data []byte) error {\n", structTypeName)
	fmt.Fprintf(&buf, "\tif s == nil {\n")
	fmt.Fprintf(&buf, "\t\treturn fmt.Errorf(\"unmarshaling %s: nil receiver\")\n", structTypeName)
	buf.WriteString("\t}\n")
	fmt.Fprintf(&buf, "\ttype alias %s\n", structTypeName)
	buf.WriteString("\taux := alias{}\n")
	for _, field := range fields {
		fmt.Fprintf(&buf, "\taux.%s = &%s{}\n", field.FieldName, field.TypeName)
	}
	fmt.Fprintf(&buf, "\tif err := json.Unmarshal(data, &aux); err != nil {\n")
	fmt.Fprintf(&buf, "\t\treturn fmt.Errorf(\"unmarshaling %s: %%w\", err)\n", structTypeName)
	buf.WriteString("\t}\n")
	for _, field := range fields {
		fmt.Fprintf(&buf, "\tif aux.%s != nil && aux.%s.Type == \"\"", field.FieldName, field.FieldName)
		for _, variant := range field.Variants {
			fmt.Fprintf(&buf, " && aux.%s.%s == nil", field.FieldName, variant.FieldName)
		}
		buf.WriteString(" {\n")
		fmt.Fprintf(&buf, "\t\taux.%s = nil\n", field.FieldName)
		buf.WriteString("\t}\n")
	}
	fmt.Fprintf(&buf, "\t*s = %s(aux)\n", structTypeName)
	buf.WriteString("\treturn nil\n")
	buf.WriteString("}\n\n")

	return buf.String()
}

func emitUnionTests(pkgName string, unionSpecs []unionFieldSpec, wrapperSpecs []unionWrapperTestSpec) string {
	if len(unionSpecs) == 0 && len(wrapperSpecs) == 0 {
		return ""
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "%s\n\npackage %s\n\n", sharedGeneratedFilePreamble, pkgName)
	buf.WriteString("import (\n")
	if len(wrapperSpecs) > 0 {
		buf.WriteString("\t\"encoding/json\"\n")
	}
	buf.WriteString("\t\"testing\"\n")
	buf.WriteString(")\n\n")

	for _, unionSpec := range unionSpecs {
		fmt.Fprintf(&buf, "func Test%sUnmarshalJSON_NilReceiver(t *testing.T) {\n", unionSpec.TypeName)
		buf.WriteString("\tt.Parallel()\n\n")
		buf.WriteString("\ttests := []struct {\n")
		buf.WriteString("\t\tname    string\n")
		buf.WriteString("\t\tpayload []byte\n")
		buf.WriteString("\t}{\n")
		for _, variant := range unionSpec.Variants {
			payload := fmt.Sprintf(`{"type":%q,%q:{}}`, variant.DiscValue, variant.DiscValue)
			fmt.Fprintf(&buf, "\t\t{name: %q, payload: []byte(%q)},\n", variant.DiscValue, payload)
		}
		buf.WriteString("\t}\n\n")
		buf.WriteString("\tfor _, tt := range tests {\n")
		buf.WriteString("\t\ttt := tt\n")
		buf.WriteString("\t\tt.Run(tt.name, func(t *testing.T) {\n")
		buf.WriteString("\t\t\tt.Parallel()\n\n")
		fmt.Fprintf(&buf, "\t\t\tvar target *%s\n", unionSpec.TypeName)
		buf.WriteString("\t\t\terr := target.UnmarshalJSON(tt.payload)\n")
		buf.WriteString("\t\t\tif err == nil {\n")
		buf.WriteString("\t\t\t\tt.Fatal(\"expected error for nil receiver\")\n")
		buf.WriteString("\t\t\t}\n")
		fmt.Fprintf(&buf, "\t\t\tif got, want := err.Error(), %q; got != want {\n", "unmarshaling "+unionSpec.TypeName+": nil receiver")
		buf.WriteString("\t\t\t\tt.Fatalf(\"unexpected error: got %q want %q\", got, want)\n")
		buf.WriteString("\t\t\t}\n")
		buf.WriteString("\t\t})\n")
		buf.WriteString("\t}\n")
		buf.WriteString("}\n\n")
	}

	for _, wrapperSpec := range wrapperSpecs {
		if inlineField, ok := wrapperSpec.inlineField(); ok {
			fmt.Fprintf(&buf, "func Test%sMarshalJSON_NilInlineUnion(t *testing.T) {\n", wrapperSpec.StructTypeName)
			buf.WriteString("\tt.Parallel()\n\n")
			fmt.Fprintf(&buf, "\ttarget := &%s{}\n", wrapperSpec.StructTypeName)
			buf.WriteString("\tpayload, err := json.Marshal(target)\n")
			buf.WriteString("\tif err != nil {\n")
			buf.WriteString("\t\tt.Fatalf(\"json.Marshal() error = %v\", err)\n")
			buf.WriteString("\t}\n")
			buf.WriteString("\tif got, want := string(payload), \"{}\"; got != want {\n")
			buf.WriteString("\t\tt.Fatalf(\"unexpected payload: got %q want %q\", got, want)\n")
			buf.WriteString("\t}\n")
			fmt.Fprintf(&buf, "\tif target.%s != nil {\n", inlineField.FieldName)
			fmt.Fprintf(&buf, "\t\tt.Fatalf(%q)\n", inlineField.FieldName+" should remain nil")
			buf.WriteString("\t}\n")
			buf.WriteString("}\n\n")
		}

		fmt.Fprintf(&buf, "func Test%sUnmarshalJSON_DecodesUnionFields(t *testing.T) {\n", wrapperSpec.StructTypeName)
		buf.WriteString("\tt.Parallel()\n\n")
		buf.WriteString("\ttests := []struct {\n")
		buf.WriteString("\t\tname   string\n")
		buf.WriteString("\t\tpayload []byte\n")
		fmt.Fprintf(&buf, "\t\tassert func(*testing.T, %s)\n", wrapperSpec.StructTypeName)
		buf.WriteString("\t}{\n")
		for _, field := range wrapperSpec.Fields {
			for _, variant := range field.Variants {
				variantPayload := fmt.Sprintf(`{"type":%q,%q:{}}`, variant.DiscValue, variant.DiscValue)
				payload := variantPayload
				if !field.Inline {
					payload = fmt.Sprintf(`{%q:%s}`, field.JSONName, variantPayload)
				}
				fmt.Fprintf(&buf, "\t\t{\n")
				fmt.Fprintf(&buf, "\t\t\tname: %q,\n", field.FieldName+"/"+variant.DiscValue)
				fmt.Fprintf(&buf, "\t\t\tpayload: []byte(%q),\n", payload)
				fmt.Fprintf(&buf, "\t\t\tassert: func(t *testing.T, target %s) {\n", wrapperSpec.StructTypeName)
				buf.WriteString("\t\t\t\tt.Helper()\n")
				fmt.Fprintf(&buf, "\t\t\t\tif target.%s == nil {\n", field.FieldName)
				fmt.Fprintf(&buf, "\t\t\t\t\tt.Fatalf(%q)\n", field.FieldName+" should be allocated")
				buf.WriteString("\t\t\t\t}\n")
				fmt.Fprintf(&buf, "\t\t\t\tif got, want := target.%s.Type, %s; got != want {\n", field.FieldName, variant.TypeConst)
				buf.WriteString("\t\t\t\t\tt.Fatalf(\"unexpected type: got %q want %q\", got, want)\n")
				buf.WriteString("\t\t\t\t}\n")
				fmt.Fprintf(&buf, "\t\t\t\tif target.%s.%s == nil {\n", field.FieldName, variant.FieldName)
				fmt.Fprintf(&buf, "\t\t\t\t\tt.Fatalf(%q)\n", field.FieldName+"."+variant.FieldName+" should be allocated")
				buf.WriteString("\t\t\t\t}\n")
				buf.WriteString("\t\t\t},\n")
				buf.WriteString("\t\t},\n")
			}
		}
		buf.WriteString("\t}\n\n")
		buf.WriteString("\tfor _, tt := range tests {\n")
		buf.WriteString("\t\ttt := tt\n")
		buf.WriteString("\t\tt.Run(tt.name, func(t *testing.T) {\n")
		buf.WriteString("\t\t\tt.Parallel()\n\n")
		fmt.Fprintf(&buf, "\t\t\tvar target %s\n", wrapperSpec.StructTypeName)
		buf.WriteString("\t\t\tif err := json.Unmarshal(tt.payload, &target); err != nil {\n")
		buf.WriteString("\t\t\t\tt.Fatalf(\"json.Unmarshal() error = %v\", err)\n")
		buf.WriteString("\t\t\t}\n")
		buf.WriteString("\t\t\ttt.assert(t, target)\n")
		buf.WriteString("\t\t})\n")
		buf.WriteString("\t}\n")
		buf.WriteString("}\n\n")
	}

	return strings.TrimRight(buf.String(), "\n") + "\n"
}

// emitDiscriminatedUnionCode emits the Go source for a discriminated union wrapper:
//   - A struct with a Type discriminator field and one optional pointer per variant.
//   - A string type alias for the discriminator + constants.
//   - Custom MarshalJSON/UnmarshalJSON that produce/consume a nested JSON object
//     {"type":"<disc>","<disc>":{...variant fields...}} matching the CRD schema and
//     K8s/etcd wire format.
func emitDiscriminatedUnionCode(typeName, propName string, variants []unionVariant) string {
	// Compute short clean field names from all ref names together so the common
	// prefix/suffix is stripped (e.g. "VirtualClusterAuthentication" prefix).
	rawRefNames := make([]string, 0, len(variants))
	for _, v := range variants {
		rawRefNames = append(rawRefNames, v.refName)
	}
	cleanFieldNames := extractVariantNames(rawRefNames)

	discValues := make([]string, 0, len(variants))
	for _, v := range variants {
		discValues = append(discValues, v.discValue)
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "// %s represents a union type for %s.\n", typeName, propName)
	buf.WriteString("// Only one of the fields should be set based on the Type.\n")
	buf.WriteString("//\n")
	fmt.Fprintf(&buf, "type %s struct {\n", typeName)

	buf.WriteString("\t// Type designates the type of configuration.\n")
	buf.WriteString("\t//\n")
	buf.WriteString("\t// +required\n")
	buf.WriteString("\t// +kubebuilder:validation:MinLength=1\n")
	fmt.Fprintf(&buf, "\t// +kubebuilder:validation:Enum=%s\n", strings.Join(discValues, ";"))
	fmt.Fprintf(&buf, "\tType %sType `json:\"type,omitempty\"`\n\n", typeName)

	for i, v := range variants {
		fieldName := fixInitialisms(cleanFieldNames[i])
		refTypeName := fixInitialisms(v.refName)
		fmt.Fprintf(&buf, "\t// %s configuration.\n", fieldName)
		buf.WriteString("\t//\n")
		buf.WriteString("\t// +optional\n")
		fmt.Fprintf(&buf, "\t%s *%s `json:\"%s,omitempty\"`\n", fieldName, refTypeName, v.discValue)
	}
	buf.WriteString("}\n\n")

	fmt.Fprintf(&buf, "// %sType represents the type of %s.\n", typeName, propName)
	fmt.Fprintf(&buf, "type %sType string\n\n", typeName)

	fmt.Fprintf(&buf, "// %sType values.\n", typeName)
	buf.WriteString("const (\n")
	for i, v := range variants {
		fieldSuffix := fixInitialisms(cleanFieldNames[i])
		fmt.Fprintf(&buf, "\t%sType%s %sType = \"%s\"\n", typeName, fieldSuffix, typeName, v.discValue)
	}
	buf.WriteString(")\n\n")

	// MarshalJSON: produce the nested shape that matches the CRD schema and
	// K8s/etcd wire format: {"type":"<disc>","<disc>":{...variant fields...}}.
	fmt.Fprintf(&buf, "// MarshalJSON implements json.Marshaler.\n")
	fmt.Fprintf(&buf, "func (u %s) MarshalJSON() ([]byte, error) {\n", typeName)
	buf.WriteString("\tm := map[string]json.RawMessage{}\n")
	buf.WriteString("\ttypeBytes, _ := json.Marshal(string(u.Type))\n")
	buf.WriteString("\tm[\"type\"] = typeBytes\n")
	buf.WriteString("\tswitch u.Type {\n")
	for i, v := range variants {
		fieldName := fixInitialisms(cleanFieldNames[i])
		fmt.Fprintf(&buf, "\tcase \"%s\":\n", v.discValue)
		fmt.Fprintf(&buf, "\t\tif u.%s != nil {\n", fieldName)
		fmt.Fprintf(&buf, "\t\t\traw, err := json.Marshal(u.%s)\n", fieldName)
		buf.WriteString("\t\t\tif err != nil {\n")
		fmt.Fprintf(&buf, "\t\t\t\treturn nil, fmt.Errorf(\"marshaling %s %s: %%w\", err)\n", typeName, v.discValue)
		buf.WriteString("\t\t\t}\n")
		fmt.Fprintf(&buf, "\t\t\tm[\"%s\"] = raw\n", v.discValue)
		buf.WriteString("\t\t}\n")
	}
	buf.WriteString("\t}\n")
	buf.WriteString("\treturn json.Marshal(m)\n")
	buf.WriteString("}\n\n")

	// UnmarshalJSON: read the "type" discriminator, then decode the variant
	// payload from raw["<discValue>"] to match the nested K8s wire shape.
	fmt.Fprintf(&buf, "// UnmarshalJSON implements json.Unmarshaler.\n")
	fmt.Fprintf(&buf, "func (u *%s) UnmarshalJSON(data []byte) error {\n", typeName)
	fmt.Fprintf(&buf, "\tif u == nil {\n")
	fmt.Fprintf(&buf, "\t\treturn fmt.Errorf(\"unmarshaling %s: nil receiver\")\n", typeName)
	buf.WriteString("\t}\n")
	buf.WriteString("\tvar probe struct {\n")
	buf.WriteString("\t\tType string `json:\"type\"`\n")
	buf.WriteString("\t}\n")
	buf.WriteString("\tif err := json.Unmarshal(data, &probe); err != nil {\n")
	buf.WriteString("\t\treturn err\n")
	buf.WriteString("\t}\n")
	buf.WriteString("\tvar raw map[string]json.RawMessage\n")
	buf.WriteString("\tif err := json.Unmarshal(data, &raw); err != nil {\n")
	buf.WriteString("\t\treturn err\n")
	buf.WriteString("\t}\n")
	fmt.Fprintf(&buf, "\tu.Type = %sType(probe.Type)\n", typeName)
	buf.WriteString("\tswitch probe.Type {\n")
	for i, v := range variants {
		fieldName := fixInitialisms(cleanFieldNames[i])
		refTypeName := fixInitialisms(v.refName)
		fmt.Fprintf(&buf, "\tcase \"%s\":\n", v.discValue)
		fmt.Fprintf(&buf, "\t\tpayload, ok := raw[\"%s\"]\n", v.discValue)
		buf.WriteString("\t\tif !ok || len(payload) == 0 {\n")
		buf.WriteString("\t\t\treturn nil\n")
		buf.WriteString("\t\t}\n")
		fmt.Fprintf(&buf, "\t\tvar val %s\n", refTypeName)
		buf.WriteString("\t\tif err := json.Unmarshal(payload, &val); err != nil {\n")
		fmt.Fprintf(&buf, "\t\t\treturn fmt.Errorf(\"unmarshaling %s %s: %%w\", err)\n", typeName, v.discValue)
		buf.WriteString("\t\t}\n")
		fmt.Fprintf(&buf, "\t\tu.%s = &val\n", fieldName)
	}
	buf.WriteString("\t}\n")
	buf.WriteString("\treturn nil\n")
	buf.WriteString("}\n")

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

// emitDiscriminatedUnionType emits Go source for a ROOT-LEVEL oneOf schema that has
// an OAS discriminator.  It delegates to emitDiscriminatedUnionCode after building
// the variant list from the schema's DiscriminatorMapping.
func (g *Generator) emitDiscriminatedUnionType(goName string, schema *parser.Schema) string {
	values := make([]string, 0, len(schema.DiscriminatorMapping))
	for v := range schema.DiscriminatorMapping {
		values = append(values, v)
	}
	sort.Strings(values)
	variants := make([]unionVariant, 0, len(values))
	for _, v := range values {
		variants = append(variants, unionVariant{discValue: v, refName: schema.DiscriminatorMapping[v]})
	}
	return emitDiscriminatedUnionCode(goName, goName, variants)
}

// emitAnyOfUnionType emits Go source for a ROOT-LEVEL anyOf schema without a
// discriminator (e.g. BackendClusterReferenceModify which is anyOf id / name).
// When each variant contains exactly one property it inlines those properties
// directly on the wrapper struct instead of nesting pointer-to-variant types,
// so the wire JSON is flat:  {"id":"..."}  or  {"name":"..."}.
func (g *Generator) emitAnyOfUnionType(goName string, schema *parser.Schema) string {
	var buf strings.Builder

	fmt.Fprintf(&buf, "// %s is a type alias.\n", goName)
	buf.WriteString("//\n")
	buf.WriteString("// +kubebuilder:validation:MinProperties=1\n")
	buf.WriteString("// +kubebuilder:validation:MaxProperties=1\n")
	fmt.Fprintf(&buf, "type %s struct {\n", goName)

	for _, variant := range schema.AnyOf {
		refName := variant.RefName
		if refName == "" {
			continue
		}
		// Prefer inlining single-property variants directly.
		if len(variant.Properties) == 1 {
			p := variant.Properties[0]
			fieldGoName := goFieldName(p.Name)
			fieldGoType := g.goType(p)
			fieldGoType = "*" + fieldGoType
			fmt.Fprintf(&buf, "\t// +optional\n")
			fmt.Fprintf(&buf, "\t%s %s `json:\"%s,omitempty\"`\n", fieldGoName, fieldGoType, p.Name)
		} else {
			// Multi-property variant: embed as a pointer.
			fieldGoName := fixInitialisms(cleanSingleVariantName(refName))
			refTypeName := fixInitialisms(refName)
			fmt.Fprintf(&buf, "\t// +optional\n")
			fmt.Fprintf(&buf, "\t%s *%s `json:\"%s,omitempty\"`\n", fieldGoName, refTypeName, strings.ToLower(fieldGoName))
		}
	}
	buf.WriteString("}\n")
	return buf.String()
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
	sort.Strings(entityNames)

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

	// IsOperationsWrapped is true when the method's SDK type is in the operations
	// package (fully-wrapped request struct). In that case, variant member types and
	// their constructors live in the components package, and the return value must
	// wrap the body union in the operations request struct.
	IsOperationsWrapped   bool
	ComponentsImportAlias string
	BodyTypeName          string // e.g. "EventGatewayListenerPolicyCreate" or "...Update"
	BodyFieldName         string // field name on the request struct, same as BodyTypeName
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

	// For operations-wrapped requests: constructors live in components, not operations.
	// WrappedCreateConstructorName and WrappedUpdateConstructorName use the
	// components package and the body-union-type-specific constructor naming pattern:
	// "Create" + bodyTypeName + discValuePascal.
	WrappedCreateConstructorName string
	WrappedUpdateConstructorName string
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
		return g.generateRootUnionSDKOps(entityName, schema, opsConfig, imports, methods, boolFields)
	}

	standardMethods := make([]sdkOpsMethod, 0, len(methods))
	for _, method := range methods {
		method.NestedUnionFields = g.buildSDKOpsNestedUnionFields(schema, method)
		standardMethods = append(standardMethods, method)
	}

	tmpl := template.Must(template.New("sdkops").Parse(sdkOpsTemplate))
	var buf strings.Builder
	data := struct {
		APIVersion   string
		EntityName   string
		Imports      []*sdkOpsImport
		BoolFields   []sdkOpsBoolField
		Methods      []sdkOpsMethod
		NeedsClient  bool
		HasSecretRef bool
	}{
		APIVersion:   g.config.APIVersion,
		EntityName:   entityName,
		Imports:      imports,
		BoolFields:   boolFields,
		Methods:      standardMethods,
		NeedsClient:  opsConfig.RequireClient,
		HasSecretRef: g.config.SecretRefEntities[entityName],
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
	opsConfig *config.EntityOpsConfig,
	imports []*sdkOpsImport,
	methods []sdkOpsMethod,
	boolFields []sdkOpsBoolField,
) (string, error) {
	rootUnionTypeName := goFieldName(entityName + "Config")

	// Detect if any method uses an operations-wrapped request (import from /operations).
	// In that case, variant member types and constructors are in the components package.
	isOperationsWrapped := false
	for _, method := range methods {
		if strings.Contains(method.ImportAlias, "sdkkonnectoper") {
			isOperationsWrapped = true
			break
		}
	}

	// Build disc-value → refName inversion from the schema's discriminator mapping,
	// so we can compute the correct constructor names for operations-wrapped requests.
	discValueForRef := make(map[string]string, len(schema.DiscriminatorMapping))
	for discValue, refName := range schema.DiscriminatorMapping {
		discValueForRef[refName] = discValue
	}

	componentsImportAlias := ""
	if isOperationsWrapped {
		componentsPath := "github.com/Kong/sdk-konnect-go/models/components"
		componentsImportAlias = sdkImportAlias(componentsPath)
		// Add the components import if not already present.
		found := false
		for _, imp := range imports {
			if imp.Path == componentsPath {
				found = true
				break
			}
		}
		if !found {
			imports = append(imports, &sdkOpsImport{Alias: componentsImportAlias, Path: componentsPath})
			sort.Slice(imports, func(i, j int) bool { return imports[i].Path < imports[j].Path })
		}
	}

	rootUnionMethods := make([]sdkOpsRootUnionMethod, 0, len(methods))
	hasUpdateMethod := false
	for _, method := range methods {
		isCreate := strings.HasPrefix(method.MethodName, "ToCreate")
		if !isCreate {
			hasUpdateMethod = true
		}
		m := sdkOpsRootUnionMethod{
			sdkOpsMethod:          method,
			IsCreate:              isCreate,
			IsOperationsWrapped:   isOperationsWrapped,
			ComponentsImportAlias: componentsImportAlias,
		}
		if isOperationsWrapped {
			if isCreate {
				m.BodyTypeName = entityName + "Create"
			} else {
				m.BodyTypeName = entityName + "Update"
			}
			m.BodyFieldName = m.BodyTypeName
		}
		rootUnionMethods = append(rootUnionMethods, m)
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
		if hasUpdateMethod && !isOperationsWrapped {
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

		// For operations-wrapped: compute wrapped constructor names using disc value.
		wrappedCreateConstructorName := ""
		if isOperationsWrapped {
			discValue := discValueForRef[variantRefName]
			discPascal := fixInitialisms(pascalFromKebab(discValue))
			wrappedCreateConstructorName = "Create" + entityName + "Create" + discPascal
		}

		fieldName := fixInitialisms(variantNames[i])
		variants = append(variants, sdkOpsRootUnionVariant{
			FieldName:                    fieldName,
			JSONName:                     strings.ToLower(variantNames[i]),
			TypeConstName:                fmt.Sprintf("%sType%s", rootUnionTypeName, fieldName),
			CreateVariantTypeName:        fixInitialisms(variantRefName),
			CreateConstructorName:        "Create" + fixInitialisms(variantRefName),
			UpdatePayloadJSONName:        updatePayloadJSONName,
			UpdateTargetFieldName:        updateTargetFieldName,
			UpdateVariantTypeName:        updateVariantTypeName,
			UpdateConstructorName:        updateConstructorName,
			WrappedCreateConstructorName: wrappedCreateConstructorName,
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
		NeedsClient   bool
		HasSecretRef  bool
	}{
		APIVersion:    g.config.APIVersion,
		EntityName:    entityName,
		UnionTypeName: rootUnionTypeName,
		Imports:       imports,
		BoolFields:    boolFields,
		Methods:       rootUnionMethods,
		Variants:      variants,
		NeedsClient:   opsConfig.RequireClient,
		HasSecretRef:  g.config.SecretRefEntities[entityName],
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

	needsJSON := false
	for _, m := range testMethods {
		if !m.ExpectError {
			needsJSON = true
			break
		}
	}

	tmpl := template.Must(template.New("sdkopstest").Parse(sdkOpsTestTemplate))
	var buf strings.Builder
	data := struct {
		APIVersion string
		EntityName string
		Methods    []sdkOpsTestMethod
		NeedsJSON  bool
	}{
		APIVersion: g.config.APIVersion,
		EntityName: entityName,
		Methods:    testMethods,
		NeedsJSON:  needsJSON,
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
		// For array-typed refs, inline the slice so fields use []ElementType directly.
		if prop.Type == "array" && prop.Items != nil {
			return "[]" + g.goType(prop.Items)
		}
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
