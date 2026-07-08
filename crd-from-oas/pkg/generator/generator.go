package generator

import (
	"fmt"
	"go/format"
	"reflect"
	"sort"
	"strings"
	"text/template"
	"time"
	"unicode"

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
	// SchemaFieldOmissions maps generated schema type names to JSON field names
	// that should be omitted when emitting schema_types.go.
	SchemaFieldOmissions map[string]map[string]bool
	// SecretReferences maps entity names to their per-path secret reference configurations.
	// When set, the designated OAS-derived string fields are replaced with SensitiveDataSource
	// structs and per-entity resolvers are generated in the sdkops file.
	SecretReferences map[string][]config.SecretReferenceConfig
	// ReconcilerConfig maps entity names to reconciler generation configurations.
	// When set, reconciler wiring files are generated for the entity.
	ReconcilerConfig map[string]*config.ReconcilerConfig
	// GenerateGroupVersionInfo controls whether to generate groupversion_info.go
	// (with SchemeGroupVersion and Resource helper) instead of register.go.
	// Defaults to true.
	GenerateGroupVersionInfo bool
	// APIGroupPackagePath is the full Go import path for the generated API types package
	// (e.g. "github.com/kong/kong-operator/v2/api/konnect/v1alpha1").
	APIGroupPackagePath string
	// APIGroupPackageAlias is the import alias for the generated API types package
	// (e.g. "xkonnectv1alpha1").
	APIGroupPackageAlias string
	// Categories are kubebuilder resource categories applied to root CRD
	// types via +kubebuilder:resource:categories=...
	Categories []string
	// SkipGetForUIDEntities is the set of entity names for which getForUID
	// generation should be skipped (e.g. because a hand-written implementation
	// already exists in an ops_<entity>_manual.go file).
	SkipGetForUIDEntities map[string]bool
	// ManualGetForUIDEntities is the set of entity names for which a manual
	// getForUID function already exists and should still be included in the
	// generated cross-entity dispatcher.
	ManualGetForUIDEntities map[string]bool
	// References maps entity names to their inter-CR reference configurations.
	// When set, the matching spec field is replaced with *commonv1alpha1.ObjectRef
	// and a resolved-ID status field is emitted.
	References map[string][]config.ReferenceConfig
}

// Generator generates Go CRD types from parsed OpenAPI schemas.
type Generator struct {
	config            Config
	parsed            *parser.ParsedSpec
	opsCreateInfos    []*OpsCreateFileInfo
	opsUpdateInfos    []*OpsUpdateFileInfo
	opsDeleteInfos    []*OpsDeleteFileInfo
	opsGetForUIDInfos []*OpsGetForUIDFileInfo
	sdkFactoryInfos   []*SDKFactoryFileInfo
	watchInfos        []*WatchFileInfo
	// anyOfSchemaNames holds schema names whose Go type is an anyOf union struct.
	// Fields referencing these schemas must be pointers so omitempty omits zero values.
	anyOfSchemaNames map[string]bool
	// sensitiveSchemaLeaves maps schema Go type name → JSON field name → secret reference config
	// for leaf fields inside $ref'd schema types that become SensitiveDataSource.
	sensitiveSchemaLeaves map[string]map[string]config.SecretReferenceConfig
	// entityDirectSensitiveLeaves maps entity name → JSON field name → secret reference config
	// for leaf fields that are direct children of the entity's apiSpec (depth 1 paths).
	entityDirectSensitiveLeaves map[string]map[string]config.SecretReferenceConfig
	// sensitiveObjectFieldParents maps entity name → object field path prefix
	// (e.g. Spec.APISpec.TLS.ClientIdentity) → generated Go type name for the
	// parent struct that contains the sensitive leaf.
	sensitiveObjectFieldParents map[string]map[string]string
	// sensitiveLeafSelectors maps entity name → config path → pre-computed
	// SecretReferenceForTemplate data (including slice/union-aware Go selectors).
	// A path maps to more than one entry when it fans out across a "*" wildcard
	// union (one entry per matching variant).
	sensitiveLeafSelectors map[string]map[string][]SecretReferenceForTemplate
	// ambiguousInlineTypeNames holds inline object type base names that would
	// collide with another generated package type and therefore need a parent
	// prefix when emitted.
	ambiguousInlineTypeNames map[string]bool
}

const sensitiveDataSourceTypeName = "SensitiveDataSource"

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

// hasSecretRefs returns true if the entity has at least one configured SecretReference.
func (g *Generator) hasSecretRefs(entityName string) bool {
	return len(g.config.SecretReferences[entityName]) > 0
}

// hasAnySecretRefs returns true if any entity in the config has SecretReferences.
func (g *Generator) hasAnySecretRefs() bool {
	return len(g.config.SecretReferences) > 0
}

// buildSensitiveLeaves pre-computes per-schema and per-entity maps of sensitive
// leaf fields so writeSchemaTypeField and generateCRDType can substitute the
// correct SensitiveDataSource type for the right fields.
func (g *Generator) buildSensitiveLeaves(parsed *parser.ParsedSpec) error {
	g.ensureInlineTypeNames(parsed)
	g.sensitiveSchemaLeaves = make(map[string]map[string]config.SecretReferenceConfig)
	g.entityDirectSensitiveLeaves = make(map[string]map[string]config.SecretReferenceConfig)
	g.sensitiveObjectFieldParents = make(map[string]map[string]string)

	for entityName, refs := range g.config.SecretReferences {
		for _, ref := range refs {
			remainder := strings.TrimPrefix(ref.Path, "spec.apiSpec.")
			segments := strings.Split(remainder, ".")

			if len(segments) == 1 {
				// Direct apiSpec field (e.g. "certificate")
				if g.entityDirectSensitiveLeaves[entityName] == nil {
					g.entityDirectSensitiveLeaves[entityName] = make(map[string]config.SecretReferenceConfig)
				}
				g.entityDirectSensitiveLeaves[entityName][segments[0]] = ref
				continue
			}

			// Nested field — walk entity schema to find the containing schema type
			entitySchema := findEntitySchema(parsed, entityName)
			if entitySchema == nil {
				return fmt.Errorf("entity %q: schema not found for secretReferences path %q", entityName, ref.Path)
			}
			if err := g.walkSensitiveLeafPath(entityName, entityName+"APISpec", "Spec.APISpec", entitySchema, segments, ref, parsed.Schemas, selectorAccumulator{}); err != nil {
				return fmt.Errorf("entity %q path %q: %w", entityName, ref.Path, err)
			}
		}
	}
	return nil
}

// walkSensitiveLeafPath walks the OAS schema tree to find the schema type that
// contains the leaf field, then records it in g.sensitiveSchemaLeaves.
// schemaGoTypeName is the Go type name of the current schema level.
// acc accumulates the Go selector parts encountered during descent.
func (g *Generator) walkSensitiveLeafPath(
	entityName string,
	schemaGoTypeName string,
	objectFieldPath string,
	schema *parser.Schema,
	segments []string,
	ref config.SecretReferenceConfig,
	schemas map[string]*parser.Schema,
	acc selectorAccumulator,
) error {
	targetJSON := segments[0]

	// Handle root-level oneOf: entity schema is a discriminated union with no direct
	// properties — variants are listed in OneOf with a discriminator mapping.
	if len(schema.Properties) == 0 && len(schema.OneOf) > 0 {
		containerGoName := entityName + "Config"
		containerObjectFieldPath := objectFieldPath + "." + containerGoName
		containerAcc := acc.withUnionContainer(containerGoName)

		// "*" fans out across every variant instead of selecting one by name — used
		// when a secret leaf lives at the same relative path in multiple (but not
		// necessarily all) variants, e.g. AIGatewayProvider's per-provider auth config.
		// Variants where the remaining path doesn't resolve are skipped rather than
		// failing generation, since a single wildcard path is expected to match only
		// a subset of variants.
		if targetJSON == "*" {
			matched := 0
			// Map iteration order is randomized; sort variant refs so the fan-out
			// (and therefore the generated code) is stable across runs.
			variantRefs := make([]string, 0, len(schema.DiscriminatorMapping))
			for _, variantRef := range schema.DiscriminatorMapping {
				variantRefs = append(variantRefs, variantRef)
			}
			sort.Strings(variantRefs)
			for _, variantRef := range variantRefs {
				if err := g.walkOneOfVariant(entityName, containerObjectFieldPath, schema.OneOf, variantRef, segments, ref, schemas, containerAcc); err == nil {
					matched++
				}
			}
			if matched == 0 {
				return fmt.Errorf("wildcard path %q matched no variant of schema %q", ref.Path, schemaGoTypeName)
			}
			return nil
		}
		// Discriminator mapping keys are OAS snake_case; path segments are camelCase.
		// Match by converting each key via jsonName().
		variantRef := ""
		for k, v := range schema.DiscriminatorMapping {
			if jsonName(k) == targetJSON || k == targetJSON {
				variantRef = v
				break
			}
		}
		if variantRef == "" {
			return fmt.Errorf("variant %q not found in discriminator mapping of schema %q", targetJSON, schemaGoTypeName)
		}
		return g.walkOneOfVariant(entityName, containerObjectFieldPath, schema.OneOf, variantRef, segments, ref, schemas, containerAcc)
	}

	// Strip optional array marker from the segment name.
	lookupJSON := targetJSON
	isArraySegment := strings.HasSuffix(targetJSON, "[]")
	if isArraySegment {
		lookupJSON = targetJSON[:len(targetJSON)-2]
	}

	// Find the property matching the first segment.
	var targetProp *parser.Property
	for i, prop := range schema.Properties {
		if jsonName(prop.Name) == lookupJSON || prop.Name == lookupJSON {
			targetProp = schema.Properties[i]
			break
		}
	}
	if targetProp == nil {
		return fmt.Errorf("field %q not found in schema %q", lookupJSON, schemaGoTypeName)
	}

	// Array descent: step into array items before the remaining path segments.
	if isArraySegment {
		if targetProp.Type != "array" || targetProp.Items == nil {
			return fmt.Errorf("field %q in schema %q is not an array type", lookupJSON, schemaGoTypeName)
		}
		if len(segments) == 1 {
			return fmt.Errorf("array field %q cannot be a secret reference leaf", lookupJSON)
		}
		nextObjectFieldPath := objectFieldPath + "." + goFieldName(targetProp.Name)
		newAcc := acc.withSlice(goFieldName(targetProp.Name))
		if targetProp.Items.RefName != "" {
			itemSchema := schemas[targetProp.Items.RefName]
			if itemSchema == nil {
				return fmt.Errorf("schema %q not found for array items of %q", targetProp.Items.RefName, lookupJSON)
			}
			return g.walkSensitiveLeafPath(entityName, fixInitialisms(targetProp.Items.RefName), nextObjectFieldPath, itemSchema, segments[1:], ref, schemas, newAcc)
		}
		if len(targetProp.Items.Properties) > 0 {
			inlineSchema := &parser.Schema{Properties: targetProp.Items.Properties}
			inlineTypeName := g.inlineTypeName(entityName, schemaGoTypeName, targetProp.Name)
			return g.walkSensitiveLeafPath(entityName, inlineTypeName, nextObjectFieldPath, inlineSchema, segments[1:], ref, schemas, newAcc)
		}
		return fmt.Errorf("cannot navigate through array items of %q in schema %q", lookupJSON, schemaGoTypeName)
	}

	// Mid-path discriminated union on this property (e.g. a provider's "auth"
	// field with basic|aws|azure|gcp variants). The next path segment selects
	// the variant by discriminator value.
	if len(targetProp.OneOf) > 0 && len(targetProp.DiscriminatorMapping) > 0 {
		if len(segments) < 2 {
			return fmt.Errorf("field %q in schema %q is a union and cannot be a secret reference leaf directly", lookupJSON, schemaGoTypeName)
		}
		variantSegment := segments[1]
		variantRef, ok := targetProp.DiscriminatorMapping[variantSegment]
		if !ok {
			return fmt.Errorf("variant %q not found in discriminator mapping of field %q in schema %q", variantSegment, lookupJSON, schemaGoTypeName)
		}
		unionFieldGoName := goFieldName(targetProp.Name)
		unionObjectFieldPath := objectFieldPath + "." + unionFieldGoName
		unionAcc := acc.withUnionContainer(unionFieldGoName)
		return g.walkOneOfVariant(entityName, unionObjectFieldPath, targetProp.OneOf, variantRef, segments[1:], ref, schemas, unionAcc)
	}

	if len(segments) == 1 {
		// Leaf — record against the containing schema.
		if g.sensitiveSchemaLeaves[schemaGoTypeName] == nil {
			g.sensitiveSchemaLeaves[schemaGoTypeName] = make(map[string]config.SecretReferenceConfig)
		}
		g.sensitiveSchemaLeaves[schemaGoTypeName][lookupJSON] = ref
		if objectFieldPath != "Spec.APISpec" {
			if g.sensitiveObjectFieldParents[entityName] == nil {
				g.sensitiveObjectFieldParents[entityName] = make(map[string]string)
			}
			g.sensitiveObjectFieldParents[entityName][objectFieldPath] = schemaGoTypeName
		}
		// Record the structured selector for template generation.
		leafGoField := goFieldName(targetProp.Name)
		g.recordSensitiveLeafSelector(entityName, ref.Path, acc, leafGoField)
		return nil
	}

	nextObjectFieldPath := objectFieldPath + "." + goFieldName(targetProp.Name)
	newAcc := acc.withField(goFieldName(targetProp.Name))

	// Navigate deeper.
	if targetProp.RefName != "" && !targetProp.IsReference {
		refSchema := schemas[targetProp.RefName]
		if refSchema == nil {
			return fmt.Errorf("schema %q not found", targetProp.RefName)
		}
		return g.walkSensitiveLeafPath(entityName, fixInitialisms(targetProp.RefName), nextObjectFieldPath, refSchema, segments[1:], ref, schemas, newAcc)
	}

	if len(targetProp.Properties) > 0 {
		// Inline object — type name follows the generated inline type naming rule.
		inlineSchema := &parser.Schema{Properties: targetProp.Properties}
		inlineTypeName := g.inlineTypeName(entityName, schemaGoTypeName, targetProp.Name)
		return g.walkSensitiveLeafPath(entityName, inlineTypeName, nextObjectFieldPath, inlineSchema, segments[1:], ref, schemas, newAcc)
	}

	return fmt.Errorf("cannot navigate through non-ref, non-inline field %q", lookupJSON)
}

// walkOneOfVariant resolves one variant of a discriminated union (root-level or
// mid-path) identified by variantRef: it derives the variant's Go field name
// from its sibling ref names, then continues walking the remaining path
// (segments[1:]) into the variant's schema. objectFieldPath and acc must
// already include the union container's own selector/guard (the field holding
// the union itself); this only appends the variant's selector/guard.
func (g *Generator) walkOneOfVariant(
	entityName string,
	objectFieldPath string,
	variants []*parser.Property,
	variantRef string,
	segments []string,
	ref config.SecretReferenceConfig,
	schemas map[string]*parser.Schema,
	acc selectorAccumulator,
) error {
	variantSchema := schemas[variantRef]
	if variantSchema == nil {
		return fmt.Errorf("schema %q not found for variant %q", variantRef, variantRef)
	}
	rawRefNames := make([]string, 0, len(variants))
	for _, v := range variants {
		if v.RefName != "" {
			rawRefNames = append(rawRefNames, v.RefName)
		}
	}
	cleanFieldNames := extractVariantNames(rawRefNames)
	variantGoField := ""
	for i, refName := range rawRefNames {
		if refName == variantRef {
			variantGoField = fixInitialisms(cleanFieldNames[i])
			break
		}
	}
	if variantGoField == "" {
		return fmt.Errorf("Go field name not found for variant %q", variantRef)
	}
	nextObjectFieldPath := objectFieldPath + "." + variantGoField
	newAcc := acc.withUnionVariant(variantGoField)
	return g.walkSensitiveLeafPath(entityName, fixInitialisms(variantRef), nextObjectFieldPath, variantSchema, segments[1:], ref, schemas, newAcc)
}

// recordSensitiveLeafSelector appends a selector for the given config path. A
// single path can resolve to multiple selectors when it fans out across a "*"
// wildcard union (one selector per matching variant).
func (g *Generator) recordSensitiveLeafSelector(entityName, path string, acc selectorAccumulator, leafGoField string) {
	if g.sensitiveLeafSelectors == nil {
		g.sensitiveLeafSelectors = make(map[string]map[string][]SecretReferenceForTemplate)
	}
	if g.sensitiveLeafSelectors[entityName] == nil {
		g.sensitiveLeafSelectors[entityName] = make(map[string][]SecretReferenceForTemplate)
	}
	g.sensitiveLeafSelectors[entityName][path] = append(g.sensitiveLeafSelectors[entityName][path], acc.buildTemplate(path, leafGoField))
}

// findEntitySchema finds the request body schema for the given entity name.
func findEntitySchema(parsed *parser.ParsedSpec, entityName string) *parser.Schema {
	for name, schema := range parsed.RequestBodies {
		if parser.GetEntityNameFromType(name) == entityName {
			return schema
		}
	}
	return nil
}

// isSchemaFieldSensitiveLeaf returns true if the given JSON field name within
// the given schema Go type name is a configured sensitive leaf.
func (g *Generator) isSchemaFieldSensitiveLeaf(schemaGoTypeName, jsonFieldName string) bool {
	leaves, ok := g.sensitiveSchemaLeaves[schemaGoTypeName]
	if !ok {
		return false
	}
	_, found := leaves[jsonFieldName]
	return found
}

// isEntityAPISpecFieldSensitiveLeaf returns true if the given JSON field name
// is a direct apiSpec-level sensitive leaf for the given entity.
func (g *Generator) isEntityAPISpecFieldSensitiveLeaf(entityName, jsonFieldName string) bool {
	return g.entityAPISpecSensitiveLeaf(entityName, jsonFieldName)
}

// isSensitiveMatchField returns true if the given getForUID objectField path
// (e.g. "Spec.APISpec.Certificate") resolves to a SensitiveDataSource leaf
// for the given entity, so the template can emit matchSensitiveDataSourceField
// instead of matchStringField.
func (g *Generator) isSensitiveMatchField(entityName, objectField string) bool {
	// Strip "Spec.APISpec." prefix and convert last segment to JSON name.
	const prefix = "Spec.APISpec."
	if !strings.HasPrefix(objectField, prefix) {
		return false
	}
	remainder := strings.TrimPrefix(objectField, prefix)
	segments := strings.Split(remainder, ".")
	if len(segments) == 1 {
		return g.isEntityAPISpecFieldSensitiveLeaf(entityName, jsonName(segments[0]))
	}
	// Nested: last segment is the leaf field; parent is a schema type.
	// Prefer the parent type recorded while walking secret-reference paths so
	// qualified inline type names resolve correctly.
	parentPath := prefix + strings.Join(segments[:len(segments)-1], ".")
	parentGoType := ""
	if parents, ok := g.sensitiveObjectFieldParents[entityName]; ok {
		parentGoType = parents[parentPath]
	}
	if parentGoType == "" {
		parentGoType = goFieldName(segments[len(segments)-2])
	}
	leafJSON := jsonName(segments[len(segments)-1])
	return g.isSchemaFieldSensitiveLeaf(parentGoType, leafJSON)
}

func (g *Generator) ensureInlineTypeNames(parsed *parser.ParsedSpec) {
	if g.ambiguousInlineTypeNames != nil {
		return
	}

	counts := make(map[string]int)
	record := func(name string) {
		if name == "" {
			return
		}
		counts[name]++
	}

	for name := range parsed.RequestBodies {
		record(parser.GetEntityNameFromType(name))
	}
	for name := range parsed.Schemas {
		record(fixInitialisms(name))
	}

	var visitProperty func(*parser.Property)
	visitProperty = func(prop *parser.Property) {
		if prop == nil {
			return
		}

		if isInlineObjectWithProperties(prop) {
			record(goFieldName(prop.Name))
			for _, nested := range prop.Properties {
				visitProperty(nested)
			}
		} else {
			for _, nested := range prop.Properties {
				visitProperty(nested)
			}
		}

		if prop.Type == "array" && prop.Items != nil {
			if isInlineObjectWithProperties(prop.Items) {
				record(goFieldName(prop.Name))
				for _, nested := range prop.Items.Properties {
					visitProperty(nested)
				}
			} else {
				visitProperty(prop.Items)
			}
		}

		visitProperty(prop.AdditionalProperties)
		for _, variant := range prop.OneOf {
			visitProperty(variant)
		}
		for _, variant := range prop.AnyOf {
			visitProperty(variant)
		}
	}

	visitSchema := func(schema *parser.Schema) {
		if schema == nil {
			return
		}
		for _, prop := range schema.Properties {
			visitProperty(prop)
		}
		visitProperty(schema.Items)
		visitProperty(schema.AdditionalProperties)
		for _, variant := range schema.OneOf {
			visitProperty(variant)
		}
		for _, variant := range schema.AnyOf {
			visitProperty(variant)
		}
	}

	for _, schema := range parsed.RequestBodies {
		visitSchema(schema)
	}
	for _, schema := range parsed.Schemas {
		visitSchema(schema)
	}

	g.ambiguousInlineTypeNames = make(map[string]bool)
	for name, count := range counts {
		if count > 1 {
			g.ambiguousInlineTypeNames[name] = true
		}
	}
}

func inlineTypeParentName(entityName, schemaGoTypeName string) string {
	if entityName != "" && schemaGoTypeName == entityName+"APISpec" {
		return entityName
	}
	return schemaGoTypeName
}

func (g *Generator) inlineTypeName(entityName, schemaGoTypeName, propName string) string {
	baseName := goFieldName(propName)
	if g.ambiguousInlineTypeNames == nil || !g.ambiguousInlineTypeNames[baseName] {
		return baseName
	}
	return inlineTypeParentName(entityName, schemaGoTypeName) + baseName
}

// pathToGoSelector converts a secretReference path (e.g. "spec.apiSpec.tls.clientIdentity.certificate")
// into a Go field selector string (e.g. "TLS.ClientIdentity.Certificate") by stripping the
// "spec.apiSpec." prefix and applying goFieldName to each remaining segment.
func pathToGoSelector(path string) string {
	remainder := strings.TrimPrefix(path, "spec.apiSpec.")
	segments := strings.Split(remainder, ".")
	for i, seg := range segments {
		segments[i] = goFieldName(seg)
	}
	return strings.Join(segments, ".")
}

// SecretReferenceForTemplate holds per-path secret reference data rendered inside
// the sdkOpsTemplate and sdkOpsRootUnionTemplate.
type SecretReferenceForTemplate struct {
	// GoFieldSelector is the Go selector string relative to obj.Spec.APISpec,
	// e.g. "TLS.ClientIdentity.Certificate". Empty when IsSlice is true.
	GoFieldSelector string
	// Path is the original dot-separated config path, e.g. "spec.apiSpec.tls.clientIdentity.certificate".
	Path string
	// IsSlice is true when the secret leaf lives inside a slice field and the
	// generated code must use a for-range loop to resolve each element.
	IsSlice bool
	// PointerGuards is the list of Go selectors (relative to obj.Spec.APISpec)
	// that must be nil-checked before accessing the leaf: pointer-typed union
	// container/variant fields encountered while walking to it (e.g. an embedded
	// union field, or a discriminated variant field). Populated for both slice
	// and non-slice leaves; empty when the path passes through no such fields.
	PointerGuards []string
	// SliceParentSelector is the Go selector of the slice field relative to obj.Spec.APISpec.
	// Used only when IsSlice is true.
	SliceParentSelector string
	// SliceLeafField is the Go field name of the sensitive leaf inside each slice element.
	// Used only when IsSlice is true.
	SliceLeafField string
}

// selectorPart records one step in a Go field selector during sensitive-leaf path walking.
type selectorPart struct {
	goName           string
	isUnionContainer bool // embedded union type pointer — needs nil guard
	isUnionVariant   bool // union variant pointer field — needs nil guard
	isSlice          bool // slice/array field — needs for-range loop
}

// selectorAccumulator accumulates selectorPart entries as walkSensitiveLeafPath descends.
type selectorAccumulator struct {
	parts []selectorPart
}

func (acc selectorAccumulator) with(p selectorPart) selectorAccumulator {
	parts := make([]selectorPart, len(acc.parts)+1)
	copy(parts, acc.parts)
	parts[len(acc.parts)] = p
	return selectorAccumulator{parts: parts}
}

func (acc selectorAccumulator) withField(goName string) selectorAccumulator {
	return acc.with(selectorPart{goName: goName})
}

func (acc selectorAccumulator) withUnionContainer(goName string) selectorAccumulator {
	return acc.with(selectorPart{goName: goName, isUnionContainer: true})
}

func (acc selectorAccumulator) withUnionVariant(goName string) selectorAccumulator {
	return acc.with(selectorPart{goName: goName, isUnionVariant: true})
}

func (acc selectorAccumulator) withSlice(goName string) selectorAccumulator {
	return acc.with(selectorPart{goName: goName, isSlice: true})
}

// buildTemplate constructs a SecretReferenceForTemplate from the accumulated path parts.
func (acc selectorAccumulator) buildTemplate(path, leafGoField string) SecretReferenceForTemplate {
	sliceIdx := -1
	for i, p := range acc.parts {
		if p.isSlice {
			sliceIdx = i
			break
		}
	}

	if sliceIdx < 0 {
		names := make([]string, 0, len(acc.parts)+1)
		var pointerGuards []string
		var runningPath []string
		for _, p := range acc.parts {
			names = append(names, p.goName)
			runningPath = append(runningPath, p.goName)
			if p.isUnionContainer || p.isUnionVariant {
				pointerGuards = append(pointerGuards, strings.Join(runningPath, "."))
			}
		}
		names = append(names, leafGoField)
		return SecretReferenceForTemplate{
			GoFieldSelector: strings.Join(names, "."),
			Path:            path,
			PointerGuards:   pointerGuards,
		}
	}

	var pointerGuards []string
	var runningPath []string
	for i := range sliceIdx {
		p := acc.parts[i]
		runningPath = append(runningPath, p.goName)
		if p.isUnionContainer || p.isUnionVariant {
			pointerGuards = append(pointerGuards, strings.Join(runningPath, "."))
		}
	}

	parentParts := make([]string, sliceIdx+1)
	for i := 0; i <= sliceIdx; i++ {
		parentParts[i] = acc.parts[i].goName
	}

	return SecretReferenceForTemplate{
		Path:                path,
		IsSlice:             true,
		PointerGuards:       pointerGuards,
		SliceParentSelector: strings.Join(parentParts, "."),
		SliceLeafField:      leafGoField,
	}
}

// templateSecretReferences returns the list of SecretReferenceForTemplate for the
// given entity, ready for use inside Go text/templates. A single configured
// path can expand to more than one entry when it fans out across a "*"
// wildcard union (one entry per matching variant).
func (g *Generator) templateSecretReferences(entityName string) []SecretReferenceForTemplate {
	refs := g.config.SecretReferences[entityName]
	var result []SecretReferenceForTemplate
	for _, ref := range refs {
		if selectors, ok := g.sensitiveLeafSelectors[entityName]; ok {
			if tmpls, ok := selectors[ref.Path]; ok && len(tmpls) > 0 {
				result = append(result, tmpls...)
				continue
			}
		}
		result = append(result, SecretReferenceForTemplate{
			GoFieldSelector: pathToGoSelector(ref.Path),
			Path:            ref.Path,
		})
	}
	return result
}

func (g *Generator) entityAPISpecSensitiveLeaf(entityName, jsonFieldName string) bool {
	leaves, ok := g.entityDirectSensitiveLeaves[entityName]
	if !ok {
		return false
	}
	_, found := leaves[jsonFieldName]
	return found
}

// Generate generates Go CRD types from parsed schemas.
func (g *Generator) Generate(parsed *parser.ParsedSpec) ([]GeneratedFile, error) {
	var files []GeneratedFile
	referencedSchemas := make(map[string]bool)
	var reconcilerEntities []string
	g.parsed = parsed
	g.ensureInlineTypeNames(parsed)

	// Pre-compute the set of schema names whose Go type is an anyOf union struct.
	// These need pointer treatment at field sites so omitempty omits zero values.
	g.anyOfSchemaNames = make(map[string]bool)
	for name, schema := range parsed.Schemas {
		if len(schema.AnyOf) > 0 {
			g.anyOfSchemaNames[name] = true
		}
	}

	// Pre-compute sensitive leaf maps so field emission can substitute types.
	if err := g.buildSensitiveLeaves(parsed); err != nil {
		return nil, fmt.Errorf("failed to build sensitive leaf maps: %w", err)
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

	// Build per-type cursors by walking each entity's CEL config against the
	// parsed schema tree using JSON-tag paths, so shared generated types can
	// apply user-provided markers to their nested fields.
	schemaCursors, err := g.buildSchemaCursors(parsed)
	if err != nil {
		return nil, err
	}

	sharedFiles, err := g.generateSharedFiles(parsed, referencedSchemas, schemaCursors)
	if err != nil {
		return nil, err
	}
	files = append(files, sharedFiles...)

	return files, nil
}

// getAPISpecCursor returns the *config.FieldConfig cursor narrowed to the
// spec.apiSpec level for the given CRD root entity name. Returns nil when no
// CEL config is defined for the entity or when the spec/apiSpec path is absent.
func (g *Generator) getAPISpecCursor(entityName string) *config.FieldConfig {
	if g.config.FieldConfig == nil {
		return nil
	}
	entityCfg := g.config.FieldConfig.Entities[entityName]
	if entityCfg == nil {
		return nil
	}
	root := &config.FieldConfig{Fields: entityCfg.Fields}
	return root.Sub("spec").Sub("apiSpec")
}

// buildSchemaCursors walks each entity's CEL config from the spec.apiSpec cursor
// downward through the parsed OAS schema tree, building a map from each referenced
// schema's Go type name to the *config.FieldConfig cursor at that schema level.
// The cursor is later passed to writeSchemaTypeField so KubebuilderTags can look
// up per-field custom validations. Returns an error for any CEL path that does
// not correspond to a valid JSON-tag field at the resolved schema level.
func (g *Generator) buildSchemaCursors(parsed *parser.ParsedSpec) (map[string]*config.FieldConfig, error) {
	g.ensureInlineTypeNames(parsed)
	if g.config.FieldConfig == nil {
		return nil, nil
	}
	cursors := make(map[string]*config.FieldConfig)
	origins := make(map[string]string)
	for name, schema := range parsed.RequestBodies {
		entityName := parser.GetEntityNameFromType(name)
		cursor := g.getAPISpecCursor(entityName)
		if cursor == nil {
			continue
		}
		if err := g.collectSchemaCursors(entityName, entityName+"APISpec", "spec.apiSpec", schema, cursor, parsed.Schemas, cursors, origins); err != nil {
			return nil, fmt.Errorf("entity %q: %w", entityName, err)
		}
	}
	return cursors, nil
}

// recordSchemaCursor stores the cursor for a generated shared type name and
// rejects conflicting duplicate writes from different entity/path traversals.
// Identical cursor trees are allowed so shared generated types can be reused
// safely.
func (g *Generator) recordSchemaCursor(
	typeName string,
	entityName string,
	path string,
	cursor *config.FieldConfig,
	out map[string]*config.FieldConfig,
	origins map[string]string,
) error {
	source := fmt.Sprintf("entity %q path %q", entityName, path)
	existing, ok := out[typeName]
	if !ok {
		out[typeName] = cursor
		origins[typeName] = source
		return nil
	}
	if fieldConfigsEqual(existing, cursor) {
		return nil
	}
	return fmt.Errorf(
		"schema cursor conflict for %q: %s conflicts with %s",
		typeName,
		origins[typeName],
		source,
	)
}

// fieldConfigsEqual reports whether two field-config trees are semantically
// equivalent, treating nil and recursively-empty configs as the same value.
func fieldConfigsEqual(a, b *config.FieldConfig) bool {
	if isEmptyFieldConfig(a) && isEmptyFieldConfig(b) {
		return true
	}
	return reflect.DeepEqual(a, b)
}

// isEmptyFieldConfig reports whether a field config carries no validations and
// no non-empty descendants, which makes it equivalent to an absent cursor.
func isEmptyFieldConfig(fc *config.FieldConfig) bool {
	if fc == nil {
		return true
	}
	if len(fc.Validations) > 0 {
		return false
	}
	for _, child := range fc.Fields {
		if !isEmptyFieldConfig(child) {
			return false
		}
	}
	return true
}

// childFieldConfig returns a deep copy of fc containing only descendant field
// configuration. Direct validations on the current field are intentionally
// dropped because they are applied at the field site, not on the nested shared
// type reached through that field.
func childFieldConfig(fc *config.FieldConfig) *config.FieldConfig {
	if fc == nil || len(fc.Fields) == 0 {
		return nil
	}

	fields := cloneFieldConfigMap(fc.Fields)
	if len(fields) == 0 {
		return nil
	}

	return &config.FieldConfig{Fields: fields}
}

func cloneFieldConfigMap(fields map[string]*config.FieldConfig) map[string]*config.FieldConfig {
	if len(fields) == 0 {
		return nil
	}

	cloned := make(map[string]*config.FieldConfig, len(fields))
	for name, fc := range fields {
		fcClone := cloneFieldConfig(fc)
		if fcClone == nil {
			continue
		}
		cloned[name] = fcClone
	}
	if len(cloned) == 0 {
		return nil
	}
	return cloned
}

func cloneFieldConfig(fc *config.FieldConfig) *config.FieldConfig {
	if fc == nil {
		return nil
	}

	clone := &config.FieldConfig{}
	if len(fc.Validations) > 0 {
		clone.Validations = append([]string(nil), fc.Validations...)
	}
	if len(fc.Fields) > 0 {
		clone.Fields = cloneFieldConfigMap(fc.Fields)
	}
	if isEmptyFieldConfig(clone) {
		return nil
	}
	return clone
}

func sensitiveDataSourceSchema() *parser.Schema {
	return &parser.Schema{
		Properties: []*parser.Property{
			{Name: "type", Type: "string"},
			{Name: "value", Type: "string"},
			{Name: "secret_ref", Type: "object"},
		},
	}
}

func (g *Generator) isSensitiveLeafAtLevel(entityName, schemaGoTypeName, jsonFieldName string) bool {
	if schemaGoTypeName == entityName+"APISpec" {
		return g.isEntityAPISpecFieldSensitiveLeaf(entityName, jsonFieldName)
	}
	return g.isSchemaFieldSensitiveLeaf(schemaGoTypeName, jsonFieldName)
}

// collectSchemaCursors recursively walks schema properties and union variants,
// recording the cursor for each referenced or inline schema type. It also
// validates that all config keys at each level correspond to real JSON-tag fields.
func (g *Generator) collectSchemaCursors(
	entityName string, //nolint:unparam
	schemaGoTypeName string,
	path string,
	schema *parser.Schema,
	cursor *config.FieldConfig,
	schemas map[string]*parser.Schema,
	out map[string]*config.FieldConfig,
	origins map[string]string,
) error {
	if err := g.validateCursorAtLevel(path, cursor, schema); err != nil {
		return err
	}

	// Walk regular properties.
	for _, prop := range schema.Properties {
		if skipProperty(prop) {
			continue
		}
		jsonTag := jsonTagForProperty(prop)
		propCursor := cursor.Sub(jsonTag)
		propPath := path + "." + jsonTag

		if g.isSensitiveLeafAtLevel(entityName, schemaGoTypeName, jsonTag) {
			sensitiveCursor := childFieldConfig(propCursor)
			if sensitiveCursor == nil {
				continue
			}
			if err := g.recordSchemaCursor(sensitiveDataSourceTypeName, entityName, propPath, sensitiveCursor, out, origins); err != nil {
				return err
			}
			if err := g.collectSchemaCursors(entityName, sensitiveDataSourceTypeName, propPath, sensitiveDataSourceSchema(), sensitiveCursor, schemas, out, origins); err != nil {
				return err
			}
			continue
		}

		switch {
		case prop.RefName != "" && !prop.IsReference:
			refSchema := schemas[prop.RefName]
			if refSchema == nil {
				continue
			}
			refCursor := childFieldConfig(propCursor)
			if refCursor == nil {
				continue
			}
			refTypeName := fixInitialisms(prop.RefName)
			if err := g.recordSchemaCursor(refTypeName, entityName, propPath, refCursor, out, origins); err != nil {
				return err
			}
			if err := g.collectSchemaCursors(entityName, refTypeName, propPath, refSchema, refCursor, schemas, out, origins); err != nil {
				return err
			}

		case isInlineObjectWithProperties(prop):
			inlineTypeName := g.inlineTypeName(entityName, schemaGoTypeName, prop.Name)
			inlineCursor := childFieldConfig(propCursor)
			if inlineCursor == nil {
				continue
			}
			if err := g.recordSchemaCursor(inlineTypeName, entityName, propPath, inlineCursor, out, origins); err != nil {
				return err
			}
			inlineSchema := &parser.Schema{Properties: prop.Properties}
			if err := g.collectSchemaCursors(entityName, inlineTypeName, propPath, inlineSchema, inlineCursor, schemas, out, origins); err != nil {
				return err
			}

		case prop.Type == "array" && prop.Items != nil && isInlineObjectWithProperties(prop.Items):
			itemCursor := childFieldConfig(propCursor)
			if itemCursor == nil {
				continue
			}
			itemTypeName := g.inlineTypeName(entityName, schemaGoTypeName, prop.Name)
			if err := g.recordSchemaCursor(itemTypeName, entityName, propPath, itemCursor, out, origins); err != nil {
				return err
			}
			itemSchema := &parser.Schema{Properties: prop.Items.Properties}
			if err := g.collectSchemaCursors(entityName, itemTypeName, propPath, itemSchema, itemCursor, schemas, out, origins); err != nil {
				return err
			}

		case prop.Type == "array" && prop.Items != nil && prop.Items.RefName != "":
			// Array is transparent to the cursor: validations address the item's fields.
			refSchema := schemas[prop.Items.RefName]
			if refSchema == nil {
				continue
			}
			itemCursor := childFieldConfig(propCursor)
			if itemCursor == nil {
				continue
			}
			itemTypeName := fixInitialisms(prop.Items.RefName)
			if err := g.recordSchemaCursor(itemTypeName, entityName, propPath, itemCursor, out, origins); err != nil {
				return err
			}
			if err := g.collectSchemaCursors(entityName, itemTypeName, propPath, refSchema, itemCursor, schemas, out, origins); err != nil {
				return err
			}

		case len(prop.OneOf) > 0:
			// Property-level discriminated union: variants are accessed via their
			// discriminator-value JSON tag under the property's own JSON tag.
			for _, variant := range buildUnionVariants(prop, generatedUnionTypeName(prop, entityName)) {
				variantTag := jsonName(variant.discValue)
				variantCursor := propCursor.Sub(variantTag)
				variantPath := propPath + "." + variantTag
				variantCursor = childFieldConfig(variantCursor)
				if variantCursor == nil {
					continue
				}
				variantTypeName := variant.goTypeName
				variantSchema := (*parser.Schema)(nil)
				if variant.source != nil && variant.source.RefName == "" && isInlineObjectWithProperties(variant.source) {
					variantSchema = &parser.Schema{Name: variantTypeName, Properties: variant.source.Properties}
				} else {
					for _, oneOfVariant := range prop.OneOf {
						if oneOfVariant.RefName == "" || fixInitialisms(oneOfVariant.RefName) != variantTypeName {
							continue
						}
						variantSchema = schemas[oneOfVariant.RefName]
						break
					}
				}
				if variantSchema == nil {
					continue
				}
				if err := g.recordSchemaCursor(variantTypeName, entityName, variantPath, variantCursor, out, origins); err != nil {
					return err
				}
				if err := g.collectSchemaCursors(entityName, variantTypeName, variantPath, variantSchema, variantCursor, schemas, out, origins); err != nil {
					return err
				}
			}
		}
	}

	// Handle root-level oneOf (entity is a discriminated union with inline embedding).
	// Variant fields are flattened directly under apiSpec (no intermediate key).
	if len(schema.OneOf) > 0 {
		rootProp := buildRootUnionProperty(schema)
		for _, variant := range buildUnionVariants(rootProp, generatedUnionTypeName(rootProp, "")) {
			variantTag := jsonName(variant.discValue)
			variantCursor := cursor.Sub(variantTag)
			variantPath := path + "." + variantTag
			variantCursor = childFieldConfig(variantCursor)
			if variantCursor == nil {
				continue
			}
			variantTypeName := variant.goTypeName
			variantSchema := (*parser.Schema)(nil)
			if variant.source != nil && variant.source.RefName == "" && isInlineObjectWithProperties(variant.source) {
				variantSchema = &parser.Schema{Name: variantTypeName, Properties: variant.source.Properties}
			} else {
				for _, oneOfVariant := range schema.OneOf {
					if oneOfVariant.RefName == "" || fixInitialisms(oneOfVariant.RefName) != variantTypeName {
						continue
					}
					variantSchema = schemas[oneOfVariant.RefName]
					break
				}
			}
			if variantSchema == nil {
				continue
			}
			if err := g.recordSchemaCursor(variantTypeName, entityName, variantPath, variantCursor, out, origins); err != nil {
				return err
			}
			if err := g.collectSchemaCursors(entityName, variantTypeName, variantPath, variantSchema, variantCursor, schemas, out, origins); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateCursorAtLevel checks that every key in cursor.Fields corresponds to a
// valid JSON-tag field at the current schema level (properties or union variants).
// Returns an error naming the offending path segment so the user can fix config.yaml.
func (g *Generator) validateCursorAtLevel(path string, cursor *config.FieldConfig, schema *parser.Schema) error {
	if cursor == nil || len(cursor.Fields) == 0 {
		return nil
	}

	validTags := make(map[string]bool)
	for _, prop := range schema.Properties {
		if !skipProperty(prop) {
			validTags[jsonTagForProperty(prop)] = true
			// Property-level oneOf: variant tags are valid under the property's tag.
			// Validation at the variant level happens during the next recursion.
		}
	}
	// Root-level oneOf: variant tags are valid directly at this level.
	if len(schema.OneOf) > 0 {
		rootProp := buildRootUnionProperty(schema)
		for _, variant := range buildUnionVariants(rootProp, generatedUnionTypeName(rootProp, "")) {
			validTags[jsonName(variant.discValue)] = true
		}
	}

	for tag := range cursor.Fields {
		if !validTags[tag] {
			return fmt.Errorf("CEL config path %q: segment %q does not match any field", path, tag)
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

	opsFile, opsTestFile, err := g.generateEntityOpsFileForEntity(entityName, schema)
	if err != nil {
		return nil, err
	}
	if opsFile != nil {
		files = append(files, *opsFile)
	}
	if opsTestFile != nil {
		files = append(files, *opsTestFile)
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

// generateEntityOpsFile generates the per-entity Konnect ops file and matching
// controller ops tests. The cross-group dispatchers are emitted separately by
// the Runner after all group-versions finish.
func (g *Generator) generateEntityOpsFileForEntity(entityName string, schema *parser.Schema) (*GeneratedFile, *GeneratedFile, error) {
	if g.config.ReconcilerConfig == nil || g.config.OpsConfig == nil {
		return nil, nil, nil
	}
	if _, hasReconciler := g.config.ReconcilerConfig[entityName]; !hasReconciler {
		return nil, nil, nil
	}
	opsConfig, ok := g.config.OpsConfig[entityName]
	if !ok || opsConfig == nil {
		return nil, nil, nil
	}

	opsResult, err := g.generateEntityOpsFile(entityName, schema, opsConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate ops file for %s: %w", entityName, err)
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
	return opsResult.File, opsResult.TestFile, nil
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
// konnect entity persistence helpers, and schema_types.go.
func (g *Generator) generateSharedFiles(parsed *parser.ParsedSpec, referencedSchemas map[string]bool, schemaCursors map[string]*config.FieldConfig) ([]GeneratedFile, error) {
	var files []GeneratedFile

	gviGeneratedContent, err := g.generateGroupVersionInfoGenerated(parsed)
	if err != nil {
		return nil, fmt.Errorf("failed to generate zz_generated_groupversion_info file: %w", err)
	}
	files = append(files, GeneratedFile{
		Name:    "zz_generated_groupversion_info.go",
		Content: gviGeneratedContent,
	})

	if g.config.GenerateGroupVersionInfo {
		gviContent, err := g.generateGroupVersionInfo()
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

	commonContent, err := g.generateCommonTypes(schemaCursors)
	if err != nil {
		return nil, fmt.Errorf("failed to generate common types: %w", err)
	}
	files = append(files, GeneratedFile{
		Name:    "zz_generated_common_types.go",
		Content: commonContent,
	})

	reconcilerConditionsFile, err := g.generateReconcilerConditions(parsed)
	if err != nil {
		return nil, fmt.Errorf("failed to generate reconciler condition constants: %w", err)
	}
	if reconcilerConditionsFile != nil {
		files = append(files, *reconcilerConditionsFile)
	}

	konnectEntityPersistenceFile, err := g.generateKonnectEntityPersistenceFile(parsed)
	if err != nil {
		return nil, fmt.Errorf("failed to generate Konnect entity persistence file: %w", err)
	}
	if konnectEntityPersistenceFile != nil {
		files = append(files, *konnectEntityPersistenceFile)
	}

	if len(referencedSchemas) > 0 {
		files = append(files, GeneratedFile{
			Name:    "zz_generated_schema_types.go",
			Content: g.generateSchemaTypes(referencedSchemas, parsed, schemaCursors),
		})

		if testsContent := g.generateSchemaTypesTests(referencedSchemas, parsed); testsContent != "" {
			files = append(files, GeneratedFile{
				Name:    "zz_generated_schema_types_test.go",
				Content: testsContent,
			})
		}
	}

	return files, nil
}

func (g *Generator) generateKonnectEntityPersistenceFile(parsed *parser.ParsedSpec) (*GeneratedFile, error) {
	if g.config.ReconcilerConfig == nil {
		return nil, nil
	}

	entityNames := make([]string, 0, len(g.config.ReconcilerConfig))
	singletonNoID := make(map[string]bool, len(g.config.ReconcilerConfig))
	for requestBodyName, schema := range parsed.RequestBodies {
		entityName := parser.GetEntityNameFromType(requestBodyName)
		if _, ok := g.config.ReconcilerConfig[entityName]; !ok {
			continue
		}
		entityNames = append(entityNames, entityName)
		singletonNoID[entityName] = isSingletonNoID(schema)
	}
	if len(entityNames) == 0 {
		return nil, nil
	}
	sort.Strings(entityNames)

	var buf strings.Builder
	fmt.Fprintf(&buf, "%s\n\npackage %s\n\n", sharedGeneratedFilePreamble, g.config.APIVersion)
	for _, entityName := range entityNames {
		fmt.Fprintf(
			&buf,
			"// PersistsKonnectID reports whether %s persists a Konnect ID in status.\n", entityName,
		)
		fmt.Fprintf(&buf, "func (*%s) PersistsKonnectID() bool {\n", entityName)
		if singletonNoID[entityName] {
			buf.WriteString("\treturn false\n")
		} else {
			buf.WriteString("\treturn true\n")
		}
		buf.WriteString("}\n\n")
	}

	formatted, err := format.Source([]byte(buf.String()))
	if err != nil {
		return nil, fmt.Errorf("failed to format generated zz_generated_konnect_entity_persistence.go: %w", err)
	}

	return &GeneratedFile{
		Name:    "zz_generated_konnect_entity_persistence.go",
		Content: string(formatted),
	}, nil
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
	if parsed == nil || parsed.RequestBodies == nil && parsed.Schemas == nil {
		return unionMemberDiscriminators
	}

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
// schemaCursors maps each schema's Go type name to the *config.FieldConfig cursor
// at that schema's level, used to apply custom kubebuilder validation markers.
func (g *Generator) generateSchemaTypes(refs map[string]bool, parsed *parser.ParsedSpec, schemaCursors map[string]*config.FieldConfig) string {
	g.ensureInlineTypeNames(parsed)
	unionMemberDiscriminators := collectUnionMemberDiscriminators(parsed)

	// Sort keys to ensure deterministic output order
	refNames := make([]string, 0, len(refs))
	for refName := range refs {
		refNames = append(refNames, refName)
	}
	sort.Strings(refNames)

	var body strings.Builder

	// emittedNested tracks inline-object type names already emitted, so a field
	// shape reused across multiple parent schemas only produces one definition.
	emittedNested := make(map[string]bool)
	for _, refName := range refNames {
		emittedNested[fixInitialisms(refName)] = true
	}

	for _, refName := range refNames {
		if schema, ok := parsed.Schemas[refName]; ok {
			goName := fixInitialisms(refName)

			// Look up the cursor for this schema (may be nil if no CEL config).
			var schemaCursor *config.FieldConfig
			if schemaCursors != nil {
				schemaCursor = schemaCursors[goName]
			}

			// Format the description as a proper comment
			comment := formatSchemaComment(goName, schema.Description)

			// Generate based on schema type
			switch {
			case len(schema.Properties) > 0:
				// It's an object type - generate a struct
				body.WriteString(comment)
				fmt.Fprintf(&body, "type %s struct {\n", goName)
				for _, prop := range schema.Properties {
					if g.shouldSkipSchemaProperty(goName, prop) || shouldSkipUnionMemberDiscriminator(refName, prop, unionMemberDiscriminators) {
						continue
					}
					g.writeSchemaTypeField(&body, prop, goName, schemaCursor)
				}
				body.WriteString("}\n\n")
				g.writeNestedInlineTypes(&body, schema.Properties, emittedNested, "", goName, schemaCursor)
				// Emit union type definitions for any property-level oneOf.
				for _, prop := range schema.Properties {
					if g.shouldSkipSchemaProperty(goName, prop) || len(prop.OneOf) == 0 {
						continue
					}
					var propCursor *config.FieldConfig
					if schemaCursor != nil {
						propCursor = schemaCursor.Sub(jsonTagForProperty(prop))
					}
					g.writeUnionTypeDefinition(&body, prop, goName, emittedNested, "", propCursor)
				}
				if wrapper := emitUnionWrapperUnmarshalJSON(goName, buildUnionFieldSpecs(schema.Properties, goName)); wrapper != "" {
					body.WriteString(wrapper)
				}
			case schema.Type == "boolean":
				// Per K8s API convention (nobools), boolean schemas become string
				// types with Enabled/Disabled enum constants.
				body.WriteString(comment)
				body.WriteString("//\n// +kubebuilder:validation:Enum=Enabled;Disabled\n")
				fmt.Fprintf(&body, "type %s string\n\n", goName)
				fmt.Fprintf(&body, "const (\n")
				fmt.Fprintf(&body, "\t// %sEnabled sets %s as enabled.\n", goName, goName)
				fmt.Fprintf(&body, "\t%sEnabled  %s = \"Enabled\"\n", goName, goName)
				fmt.Fprintf(&body, "\t// %sDisabled sets %s as disabled.\n", goName, goName)
				fmt.Fprintf(&body, "\t%sDisabled %s = \"Disabled\"\n", goName, goName)
				fmt.Fprintf(&body, ")\n\n")

			case isScalarStringIntOneOf(schema.OneOf, parsed.Schemas):
				// Root-level oneOf of exactly {string, integer} — emit a Go type alias for
				// intstr.IntOrString. Alias (not named type) ensures controller-gen recognises
				// it as IntOrString and emits anyOf:[integer,string] in the CRD schema.
				body.WriteString(comment)
				body.WriteString("//\n// +kubebuilder:validation:XIntOrString\n")
				fmt.Fprintf(&body, "type %s = intstr.IntOrString\n\n", goName)

			case hasRefVariants(schema.OneOf) && schema.Discriminator != "":
				// Root-level oneOf + OAS discriminator: emit a flat discriminated union
				// wrapper struct with custom MarshalJSON/UnmarshalJSON so the wire JSON
				// matches the SDK's expected flat shape.
				body.WriteString(g.emitDiscriminatedUnionType(goName, schema))

			case hasRefVariants(schema.AnyOf):
				// Root-level anyOf without discriminator: emit a wrapper struct with one
				// optional pointer per variant, with MinProperties=1 / MaxProperties=1.
				body.WriteString(g.emitAnyOfUnionType(goName, schema))

			case schema.AdditionalProperties != nil:
				// Map type with value constraints: generate a dedicated value type
				// with native kubebuilder markers, then define the map using it.
				valueTypeName := refName + "Value"
				valueBaseType := propertyToGoBaseType(schema.AdditionalProperties)

				fmt.Fprintf(&body, "// %s is the value type for %s.\n", valueTypeName, refName)
				if markers := valueTypeMarkers(schema.AdditionalProperties); len(markers) > 0 {
					body.WriteString("//\n")
					for _, marker := range markers {
						fmt.Fprintf(&body, "// %s\n", marker)
					}
				}
				fmt.Fprintf(&body, "type %s %s\n\n", valueTypeName, valueBaseType)

				body.WriteString(comment)
				fmt.Fprintf(&body, "type %s map[string]%s\n\n", refName, valueTypeName)

			case schema.Type == "array" && schema.Items != nil && isInlineObjectWithProperties(schema.Items):
				// Referenced array schema whose items are an inline object (not a $ref).
				// Emit a named element struct + a slice type alias so controller-gen
				// produces compilable, faithful deepcopy. Without this the type degrades
				// to []any, whose generated deepcopy has an unused range variable (for
				// i := range *in {}) and fails to compile.
				elemTypeName := goName + "Item"
				if !emittedNested[elemTypeName] {
					emittedNested[elemTypeName] = true
					body.WriteString(formatSchemaComment(elemTypeName, schema.Items.Description))
					fmt.Fprintf(&body, "type %s struct {\n", elemTypeName)
					for _, nested := range schema.Items.Properties {
						if g.shouldSkipSchemaProperty(elemTypeName, nested) {
							continue
						}
						g.writeSchemaTypeField(&body, nested, elemTypeName, schemaCursor)
					}
					body.WriteString("}\n\n")
					g.writeNestedInlineTypes(&body, schema.Items.Properties, emittedNested, "", elemTypeName, schemaCursor)
				}
				body.WriteString(comment)
				fmt.Fprintf(&body, "type %s []%s\n\n", goName, elemTypeName)

			default:
				// Generate based on the schema's actual type
				body.WriteString(comment)
				goType := schemaToGoType(schema)
				fmt.Fprintf(&body, "type %s %s\n\n", goName, goType)
			}
		} else {
			panic("Schema not found for reference: " + refName)
		}
	}

	bodyString := strings.TrimRight(body.String(), "\n")
	needsAPIExtJSON, needsIntStr, objectRefImport := g.schemaTypesImports(refNames, parsed)
	// encoding/json and fmt are only needed when the emitted schema body contains
	// generated JSON helper methods.
	needsEncodingJSON := strings.Contains(bodyString, "json.") || strings.Contains(bodyString, "fmt.")
	if !needsAPIExtJSON && strings.Contains(bodyString, "apiextensionsv1.") {
		needsAPIExtJSON = true
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "%s\n\npackage %s\n\n", sharedGeneratedFilePreamble, g.config.APIVersion)
	if needsAPIExtJSON || needsIntStr || needsEncodingJSON || objectRefImport != nil {
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
	if bodyString != "" {
		buf.WriteString(bodyString)
		buf.WriteByte('\n')
	}

	return strings.TrimRight(buf.String(), "\n") + "\n"
}

func (g *Generator) schemaTypesImports(refNames []string, parsed *parser.ParsedSpec) (needsAPIExtJSON, needsIntStr bool, objectRefImport *config.ImportConfig) {
	for _, refName := range refNames {
		schema, ok := parsed.Schemas[refName]
		if !ok {
			continue
		}
		goName := fixInitialisms(refName)

		if !needsAPIExtJSON && schemaUsesJSON(g, schema, parsed) {
			needsAPIExtJSON = true
		}
		if !needsIntStr && isScalarStringIntOneOf(schema.OneOf, parsed.Schemas) {
			needsIntStr = true
		}
		if objectRefImport == nil {
			objectRefImport = g.schemaTypeObjectRefImportIfNeeded(goName, schema)
		}

		if needsAPIExtJSON && needsIntStr && objectRefImport != nil {
			break
		}
	}
	return
}

func (g *Generator) writeSchemaTypeField(buf *strings.Builder, prop *parser.Property, typeName string, fieldCursor *config.FieldConfig) {
	jsonFieldName := jsonName(prop.Name)
	isSensitive := g.isSchemaFieldSensitiveLeaf(typeName, jsonFieldName)

	buf.WriteString(formatComment(prop.Description))
	buf.WriteString("\n")
	buf.WriteString("\t//\n")
	if isSensitive {
		// Sensitive leaf: emit only required/optional; SensitiveDataSource carries
		// the shared validation for its inline value representation.
		if prop.Required && !prop.Nullable {
			fmt.Fprintf(buf, "\t// %s\n", markerRequired())
		} else {
			fmt.Fprintf(buf, "\t// %s\n", markerOptional())
		}
	} else {
		for _, tag := range KubebuilderTags(prop, fieldCursor) {
			fmt.Fprintf(buf, "\t// %s\n", tag)
		}
	}
	goType := g.goType(prop)
	switch {
	case isSensitive:
		goType = "SensitiveDataSource"
	case prop.RefName != "" && g.anyOfSchemaNames[prop.RefName]:
		goType = "*" + fixInitialisms(prop.RefName)
	case len(prop.OneOf) > 0:
		// For schema types, oneOf properties use entity-prefixed type names to avoid
		// package-scoped collisions (e.g. bare "Config" would clash across entities).
		goType = "*" + generatedUnionTypeName(prop, typeName)
	case prop.Type == "array" && prop.Items != nil && isInlineObjectWithProperties(prop.Items):
		goType = "[]" + g.inlineTypeName("", typeName, prop.Name)
	case isInlineObjectWithProperties(prop):
		goType = g.inlineTypeName("", typeName, prop.Name)
	}
	fmt.Fprintf(buf, "\t%s %s `json:\"%s\"`\n", goFieldName(prop.Name), goType, jsonTag(prop, goType))
}

// writeNestedInlineTypes emits Go type definitions for any property that is an
// inline object (Type=="object" with sub-Properties and no $ref). This covers
// schemas like BackendClusterTLS.client_identity, where the OpenAPI spec
// declares the nested object inline rather than via $ref. Without this,
// generateSchemaTypes would reference the type by name (e.g. ClientIdentity)
// without ever defining it, producing uncompilable Go.
//
// emitted is a shared set used to dedupe across all parent schemas: a type
// name is generated at most once even when referenced repeatedly, while
// arbitrarily deep inline shapes are handled.
func (g *Generator) writeNestedInlineTypes(buf *strings.Builder, props []*parser.Property, emitted map[string]bool, entityName, parentTypeName string, parentCursor *config.FieldConfig) {
	for _, prop := range props {
		if g.shouldSkipSchemaProperty(parentTypeName, prop) {
			continue
		}
		propCursor := parentCursor.Sub(jsonTagForProperty(prop))

		if prop.Type == "array" && prop.Items != nil && isInlineObjectWithProperties(prop.Items) {
			typeName := g.inlineTypeName(entityName, parentTypeName, prop.Name)
			if emitted[typeName] {
				g.writeNestedInlineTypes(buf, prop.Items.Properties, emitted, entityName, typeName, propCursor)
				continue
			}
			emitted[typeName] = true

			buf.WriteString(formatSchemaComment(typeName, prop.Description))
			fmt.Fprintf(buf, "type %s struct {\n", typeName)
			for _, nested := range prop.Items.Properties {
				if g.shouldSkipSchemaProperty(typeName, nested) {
					continue
				}
				g.writeSchemaTypeField(buf, nested, typeName, propCursor)
			}
			buf.WriteString("}\n\n")

			g.writeNestedInlineTypes(buf, prop.Items.Properties, emitted, entityName, typeName, propCursor)
			for _, nested := range prop.Items.Properties {
				if g.shouldSkipSchemaProperty(typeName, nested) || len(nested.OneOf) == 0 {
					continue
				}
				g.writeUnionTypeDefinition(buf, nested, typeName, emitted, entityName, propCursor.Sub(jsonTagForProperty(nested)))
			}
			if wrapper := emitUnionWrapperUnmarshalJSON(typeName, buildUnionFieldSpecs(prop.Items.Properties, typeName)); wrapper != "" {
				buf.WriteString(wrapper)
			}
			continue
		}

		if prop.Items != nil {
			g.writeNestedInlineTypes(buf, []*parser.Property{prop.Items}, emitted, entityName, parentTypeName, propCursor)
		}
		if !isInlineObjectWithProperties(prop) {
			continue
		}
		typeName := g.inlineTypeName(entityName, parentTypeName, prop.Name)
		// Advance cursor to the inline struct level.
		inlineCursor := propCursor
		if emitted[typeName] {
			g.writeNestedInlineTypes(buf, prop.Properties, emitted, entityName, typeName, inlineCursor)
			continue
		}
		emitted[typeName] = true

		buf.WriteString(formatSchemaComment(typeName, prop.Description))
		fmt.Fprintf(buf, "type %s struct {\n", typeName)
		for _, nested := range prop.Properties {
			if g.shouldSkipSchemaProperty(typeName, nested) {
				continue
			}
			g.writeSchemaTypeField(buf, nested, typeName, inlineCursor)
		}
		buf.WriteString("}\n\n")

		g.writeNestedInlineTypes(buf, prop.Properties, emitted, entityName, typeName, inlineCursor)
		for _, nested := range prop.Properties {
			if g.shouldSkipSchemaProperty(typeName, nested) || len(nested.OneOf) == 0 {
				continue
			}
			g.writeUnionTypeDefinition(buf, nested, typeName, emitted, entityName, inlineCursor.Sub(jsonTagForProperty(nested)))
		}
		if wrapper := emitUnionWrapperUnmarshalJSON(typeName, buildUnionFieldSpecs(prop.Properties, typeName)); wrapper != "" {
			buf.WriteString(wrapper)
		}
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
// Returns true only when there are exactly two variants whose effective types are
// exactly "string" and "integer" (in either order). Variants may be inline scalars
// or $ref pointers to scalar component schemas; schemas is used to resolve $refs.
// Variants with sub-properties, nested unions, or unresolvable refs are rejected.
func isScalarStringIntOneOf(variants []*parser.Property, schemas map[string]*parser.Schema) bool {
	if len(variants) != 2 {
		return false
	}
	types := make(map[string]bool, 2)
	for _, v := range variants {
		switch {
		case v.RefName != "":
			// Resolve a $ref variant to its referenced scalar schema. Reject
			// non-scalar targets (objects, nested unions, unresolvable refs).
			ref, ok := schemas[v.RefName]
			if !ok || len(ref.Properties) > 0 || len(ref.OneOf) > 0 || len(ref.AnyOf) > 0 {
				return false
			}
			types[ref.Type] = true
		case len(v.Properties) > 0:
			return false
		default:
			types[v.Type] = true
		}
	}
	return types["string"] && types["integer"]
}

func (g *Generator) generateCRDType(name string, schema *parser.Schema) (string, error) {
	entityName := parser.GetEntityNameFromType(name)

	// apiSpecCursor is the FieldConfig cursor pre-advanced to the spec.apiSpec level
	// for this entity. KubebuilderTags advances it further by each property's JSON tag.
	apiSpecCursor := g.getAPISpecCursor(entityName)

	// Create a closure that passes the apiSpec-level cursor to KubebuilderTags.
	// For sensitive leaf fields the OAS string markers are suppressed since the
	// SensitiveDataSource struct carries its own validation.
	kubebuilderTagsWithConfig := func(prop *parser.Property) []string {
		if g.isEntityAPISpecFieldSensitiveLeaf(entityName, jsonName(prop.Name)) {
			if prop.Required && !prop.Nullable {
				return []string{markerRequired()}
			}
			return []string{markerOptional()}
		}
		return KubebuilderTags(prop, apiSpecCursor)
	}

	// In the CRD APISpec, property-level oneOf types are rendered as a pointer
	// to a generated union type. The union type is emitted in the same package,
	// so its name must be prefixed with the entity name to avoid package-scoped
	// collisions (e.g. a bare "Config" type would clash across entities).
	// anyOf union refs also need pointer treatment so omitempty omits zero values.
	// Sensitive leaf fields are emitted as SensitiveDataSource regardless of their OAS type.
	goTypeInCRD := func(prop *parser.Property) string {
		if g.isEntityAPISpecFieldSensitiveLeaf(entityName, jsonName(prop.Name)) {
			return "SensitiveDataSource"
		}
		if len(prop.OneOf) > 0 {
			return "*" + entityName + goFieldName(prop.Name)
		}
		if prop.RefName != "" && g.anyOfSchemaNames[prop.RefName] {
			return "*" + fixInitialisms(prop.RefName)
		}
		if prop.Type == "array" && prop.Items != nil && isInlineObjectWithProperties(prop.Items) {
			return "[]" + g.inlineTypeName(entityName, entityName+"APISpec", prop.Name)
		}
		if isInlineObjectWithProperties(prop) {
			return g.inlineTypeName(entityName, entityName+"APISpec", prop.Name)
		}
		return g.goType(prop)
	}

	rc := g.config.ReconcilerConfig[entityName]
	rootParentDep := rootRefDependency(schema)

	var (
		parentRef                *config.ParentRefConfig
		parentRefGoFieldName     string
		parentRefJSONName        string
		setParentIDEntityName    string
		parentStatusEntityName   string
		emitParentRefStatusField bool
	)
	if rc != nil && rc.ParentRef != nil {
		parentRef = rc.ParentRef
		parentRefGoFieldName = goFieldName(rc.ParentRef.FieldName)
		parentRefJSONName = rc.ParentRef.FieldName
		setParentIDEntityName = rc.ParentEntityKind()
		parentStatusEntityName = parentRefStatusEntityName(rootParentDep, rc)
		emitParentRefStatusField = shouldEmitParentRefStatusField(rootParentDep, rc)
	}

	isParentRefReplacedField := func(propName string) bool {
		return parentRef != nil && propName == parentRef.ReplacesAPISpecField
	}

	funcMap := template.FuncMap{
		"goType":                   goTypeInCRD,
		"goFieldName":              goFieldName,
		"jsonTag":                  jsonTag,
		"jsonPropName":             func(p *parser.Property) string { return jsonName(p.Name) },
		"refJSONTag":               func(p *parser.Property) string { return jsonName(p.Name) + "Ref" },
		"kubebuilderTags":          kubebuilderTagsWithConfig,
		"isRefProperty":            isRefProperty,
		"isRefConfigField":         func(prop *parser.Property) bool { return g.referenceForField(entityName, prop.Name) != nil },
		"isParentRefReplacedField": func(propName string) bool { return isParentRefReplacedField(propName) },
		"refEntityName":            parser.GetRefEntityName,
		"skipProperty":             skipProperty,
		"lower":                    strings.ToLower,
		"lowerCamel":               lowerCamelCase,
		"statusIDJSONName":         func(entityName string) string { return toLowerCamel(entityName) + "ID" },
		"formatComment":            formatComment,
		"hasRootOneOf":             hasRootOneOf,
		"objectRefTypeName":        func() string { return g.objectRefTypeName() },
		"namespacedRefTypeName":    func() string { return g.namespacedRefTypeName() },
		"join":                     strings.Join,
	}

	tmpl := template.Must(template.New("crd").Funcs(funcMap).Parse(crdTypeTemplate))

	// Determine whether we need the ObjectRef import: either for dependencies/refs,
	// sensitive data source SecretRef type, configured inter-CR references, or
	// a parentRef override field.
	objectRefImport := g.objectRefImportIfNeeded(schema)
	if objectRefImport == nil && g.hasSecretRefs(entityName) && g.objectRefImported() {
		objectRefImport = g.config.CommonTypes.ObjectRef.Import
	}
	if objectRefImport == nil && g.entityHasReferences(entityName) && g.objectRefImported() {
		objectRefImport = g.config.CommonTypes.ObjectRef.Import
	}
	if objectRefImport == nil && parentRef != nil && g.objectRefImported() {
		objectRefImport = g.config.CommonTypes.ObjectRef.Import
	}

	hasRootReconciler := false
	if rc != nil {
		hasRootReconciler = rc.GetIsRoot()
	}

	categories := g.config.Categories

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

	// When ParentRef is configured, suppress the OpenAPI-derived spec ref field
	// (e.g. GatewayRef) so only the configured field (e.g. EventGatewayBackendClusterRef)
	// is emitted.
	immediateParentDep := rootParentDep
	if parentRef != nil {
		immediateParentDep = nil
	}

	var responseStatusFields []config.ResponseStatusFieldConfig
	if oc := g.config.OpsConfig[entityName]; oc != nil {
		responseStatusFields = oc.ResponseStatusFields
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
		HasRootReconciler         bool
		ImmediateParentDependency *parser.Dependency
		Categories                []string
		SingletonNoID             bool
		References                []TemplateReferenceConfig
		ParentRef                 *config.ParentRefConfig
		ParentRefGoFieldName      string
		ParentRefJSONFieldName    string
		SetParentIDEntityName     string
		ParentStatusEntityName    string
		EmitParentRefStatusField  bool
		ResponseStatusFields      []config.ResponseStatusFieldConfig
	}{
		EntityName:                entityName,
		Schema:                    schema,
		APIGroup:                  g.config.APIGroup,
		APIVersion:                g.config.APIVersion,
		NeedsJSONImport:           schemaUsesJSONInCRDTypeFile(g, schema),
		HasUnionTypes:             hasUnionTypes,
		ObjectRefImport:           objectRefImport,
		HasRootReconciler:         hasRootReconciler,
		ImmediateParentDependency: immediateParentDep,
		Categories:                categories,
		SingletonNoID:             isSingletonNoID(schema),
		References:                g.templateReferences(entityName),
		ParentRef:                 parentRef,
		ParentRefGoFieldName:      parentRefGoFieldName,
		ParentRefJSONFieldName:    parentRefJSONName,
		SetParentIDEntityName:     setParentIDEntityName,
		ParentStatusEntityName:    parentStatusEntityName,
		EmitParentRefStatusField:  emitParentRefStatusField,
		ResponseStatusFields:      responseStatusFields,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	emittedInlineAndUnionTypes := make(map[string]bool)

	var nestedInlineTypes strings.Builder
	g.writeNestedInlineTypes(&nestedInlineTypes, schema.Properties, emittedInlineAndUnionTypes, entityName, entityName+"APISpec", apiSpecCursor)
	if nestedInlineTypes.Len() > 0 {
		buf.WriteString("\n")
		buf.WriteString(nestedInlineTypes.String())
	}

	// Generate union types for any oneOf properties
	unionTypes := g.generateUnionTypes(schema, entityName, emittedInlineAndUnionTypes, apiSpecCursor)
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
	var wrapperSpecs []unionWrapperTestSpec
	if len(unionSpecs) > 0 {
		wrapperSpecs = []unionWrapperTestSpec{{
			StructTypeName: entityName + "APISpec",
			Fields:         unionSpecs,
		}}
	}
	return emitUnionTests(g.config.APIVersion, unionSpecs, wrapperSpecs, []string{entityName + "APISpec"})
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
	var marshalTestTypes []string

	for _, refName := range refNames {
		schema, ok := parsed.Schemas[refName]
		if !ok {
			continue
		}

		goName := fixInitialisms(refName)

		if len(schema.Properties) > 0 {
			marshalTestTypes = append(marshalTestTypes, goName)
		}

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

	return emitUnionTests(g.config.APIVersion, unionSpecs, wrapperSpecs, marshalTestTypes)
}

func (g *Generator) generateCRDFuncs(name string, schema *parser.Schema) (string, error) {
	entityName := parser.GetEntityNameFromType(name)
	rc := g.config.ReconcilerConfig[entityName]
	isReconcilerRoot := false
	if rc != nil {
		isReconcilerRoot = rc.GetIsRoot()
	}
	rootRefDependency := rootRefDependency(schema)

	var (
		funcsParentRef              *config.ParentRefConfig
		funcsParentRefGoFieldName   string
		funcsSetParentIDEntityName  string
		funcsParentStatusEntityName string
		emitParentRefStatusField    bool
	)
	if rc != nil && rc.ParentRef != nil {
		funcsParentRef = rc.ParentRef
		funcsParentRefGoFieldName = goFieldName(rc.ParentRef.FieldName)
		funcsSetParentIDEntityName = rc.ParentEntityKind()
		funcsParentStatusEntityName = parentRefStatusEntityName(rootRefDependency, rc)
		emitParentRefStatusField = shouldEmitParentRefStatusField(rootRefDependency, rc)
	}

	imports := make([]*config.ImportConfig, 0, 3)
	imports = appendUniqueImportConfig(imports, defaultKonnectStatusImport())
	imports = appendUniqueImportConfig(imports, &config.ImportConfig{
		Alias: "metav1",
		Path:  "k8s.io/apimachinery/pkg/apis/meta/v1",
	})
	if (rootRefDependency != nil || funcsParentRef != nil) && g.objectRefImported() {
		imports = appendUniqueImportConfig(imports, g.config.CommonTypes.ObjectRef.Import)
	}
	if rootRefDependency != nil {
		imports = appendUniqueImportConfig(imports, &config.ImportConfig{
			Path: "k8s.io/apimachinery/pkg/runtime/schema",
		})
	}
	if isReconcilerRoot {
		imports = appendUniqueImportConfig(imports, &config.ImportConfig{
			Alias: defaultKonnectStatusAlias,
			Path:  defaultKonnectStatusPackage,
		})
	}
	if g.entityHasReferences(entityName) && g.objectRefImported() {
		imports = appendUniqueImportConfig(imports, g.config.CommonTypes.ObjectRef.Import)
	}

	funcsFuncMap := template.FuncMap{
		"lowerCamel":  lowerCamelCase,
		"goFieldName": goFieldName,
	}
	tmpl := template.Must(template.New("crdFuncs").Funcs(funcsFuncMap).Parse(crdFuncsTemplate))

	// Compute ancestor-chain data for GetAncestorIDs / SetAncestorID.
	// AncestorEntityTypes maps each OpenAPI-derived dependency (in URL order)
	// to its GVK Kind string. For single-dep entities without a parentRef override,
	// this defaults to [ParentKind]. For entities with a parentRef override, the
	// caller must supply AncestorEntityTypes in the reconciler config.
	parentKindForFuncs := func() string {
		if rootRefDependency == nil {
			return ""
		}
		if rc != nil && rc.ParentEntityKind() != "" {
			return rc.ParentEntityKind()
		}
		return refConditionEntityName(rootRefDependency)
	}()
	parentGroupForFuncs := func() string {
		if rootRefDependency == nil {
			return ""
		}
		if rc == nil {
			return g.config.APIGroup
		}
		return rc.ParentEntityGroup(g.config.APIGroup)
	}()
	ancestorEntityTypes := func() []string {
		if len(schema.Dependencies) == 0 {
			return nil
		}
		if rc != nil && len(rc.AncestorEntityKinds()) > 0 {
			return rc.AncestorEntityKinds()
		}
		if len(schema.Dependencies) == 1 {
			return []string{parentKindForFuncs}
		}
		return nil
	}()
	var ancestorDependencies []*parser.Dependency
	if len(ancestorEntityTypes) > 0 && len(ancestorEntityTypes) <= len(schema.Dependencies) {
		ancestorDependencies = schema.Dependencies[:len(ancestorEntityTypes)]
	}

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
		RefConditionPrefix                 string
		IsReconcilerRoot                   bool
		KonnectAPIAuthConfigurationRefType string
		ParentKind                         string
		ParentGroup                        string
		References                         []TemplateReferenceConfig
		ObjectRefTypeName                  string
		ParentRef                          *config.ParentRefConfig
		ParentRefGoFieldName               string
		SetParentIDEntityName              string
		ParentStatusEntityName             string
		EmitParentRefStatusField           bool
		AncestorDependencies               []*parser.Dependency
		AncestorEntityTypes                []string
		SingletonNoID                      bool
	}{
		EntityName:                entityName,
		APIVersion:                g.config.APIVersion,
		Imports:                   imports,
		KonnectStatusType:         defaultKonnectStatusQualifiedTypeName(),
		KonnectLabelsField:        g.konnectLabelsField(schema),
		Dependencies:              schema.Dependencies,
		RootRefDependency:         rootRefDependency,
		RootRefAccessorEntityName: rootRefAccessorEntityName(rootRefDependency),
		RootRefTypeName:           g.objectRefTypeName(),
		RefConditionPrefix: func() string {
			if rootRefDependency == nil {
				return ""
			}
			if funcsParentRef != nil && rc != nil && rc.ParentEntityKind() != "" {
				return rc.ParentEntityKind()
			}
			return refConditionEntityName(rootRefDependency)
		}(),
		IsReconcilerRoot:                   isReconcilerRoot,
		KonnectAPIAuthConfigurationRefType: defaultKonnectStatusAlias + ".ControlPlaneKonnectAPIAuthConfigurationRef",
		ParentKind:                         parentKindForFuncs,
		ParentGroup:                        parentGroupForFuncs,
		References:                         g.templateReferences(entityName),
		ObjectRefTypeName:                  g.objectRefTypeName(),
		ParentRef:                          funcsParentRef,
		ParentRefGoFieldName:               funcsParentRefGoFieldName,
		SetParentIDEntityName:              funcsSetParentIDEntityName,
		ParentStatusEntityName:             funcsParentStatusEntityName,
		EmitParentRefStatusField:           emitParentRefStatusField,
		AncestorDependencies:               ancestorDependencies,
		AncestorEntityTypes:                ancestorEntityTypes,
		SingletonNoID:                      isSingletonNoID(schema),
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

func parentRefStatusEntityName(dep *parser.Dependency, rc *config.ReconcilerConfig) string {
	if rc == nil || rc.ParentRef == nil {
		return ""
	}
	if rc.ParentRef.ReplacesAPISpecField != "" && rc.ParentEntityKind() != "" {
		return rc.ParentEntityKind()
	}
	if dep != nil {
		return dep.EntityName
	}
	return rc.ParentEntityKind()
}

func shouldEmitParentRefStatusField(dep *parser.Dependency, rc *config.ReconcilerConfig) bool {
	fieldName := parentRefStatusEntityName(dep, rc)
	return fieldName != "" && (dep == nil || fieldName != dep.EntityName)
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
func (g *Generator) generateUnionTypes(schema *parser.Schema, entityName string, emitted map[string]bool, parentCursor *config.FieldConfig) string {
	var buf strings.Builder

	// Handle root-level oneOf (the schema itself is a union type)
	if len(schema.OneOf) > 0 {
		g.writeUnionTypeDefinition(&buf, buildRootUnionProperty(schema), "", emitted, entityName, parentCursor)
	}

	// Handle property-level oneOf
	for _, prop := range schema.Properties {
		if skipProperty(prop) {
			continue
		}
		if len(prop.OneOf) > 0 {
			g.writeUnionTypeDefinition(&buf, prop, entityName, emitted, entityName, parentCursor.Sub(jsonTagForProperty(prop)))
		}
	}

	return buf.String()
}

// unionVariant holds the generated names for one union variant.
type unionVariant struct {
	discValue  string           // OAS discriminator value, e.g. "sasl_plain" — used for JSON tag and enum const.
	fieldName  string           // Go field name used by the wrapper.
	goTypeName string           // Go type name used by the wrapper field.
	source     *parser.Property // Original property schema, used for anonymous inline members.
}

// buildUnionVariants builds the ordered list of variants for a property-level oneOf
// union. Uses the OAS discriminator mapping when present (for correct snake_case
// values); falls back to extractVariantNames when no discriminator is available.
func buildUnionVariants(prop *parser.Property, unionTypeName string) []unionVariant {
	if len(prop.DiscriminatorMapping) > 0 {
		values := make([]string, 0, len(prop.DiscriminatorMapping))
		for v := range prop.DiscriminatorMapping {
			values = append(values, v)
		}
		sort.Strings(values)
		rawRefNames := make([]string, 0, len(values))
		for _, v := range values {
			rawRefNames = append(rawRefNames, prop.DiscriminatorMapping[v])
		}
		fieldNames := uniqueUnionFieldNames(rawRefNames)
		variants := make([]unionVariant, 0, len(values))
		for i, v := range values {
			refName := prop.DiscriminatorMapping[v]
			variants = append(variants, unionVariant{
				discValue:  v,
				fieldName:  fixInitialisms(goFieldName(fieldNames[i])),
				goTypeName: fixInitialisms(refName),
			})
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
	discValues := uniqueUnionDiscriminatorValues(rawNames)
	fieldNames := uniqueUnionFieldNames(rawNames)
	variants := make([]unionVariant, 0, len(prop.OneOf))
	for i, v := range prop.OneOf {
		fieldName := fixInitialisms(goFieldName(fieldNames[i]))
		goTypeName := anonymousUnionVariantTypeName(unionTypeName, fieldName)
		if v.RefName != "" {
			goTypeName = fixInitialisms(v.RefName)
		}
		variants = append(variants, unionVariant{
			discValue:  discValues[i],
			fieldName:  fieldName,
			goTypeName: goTypeName,
			source:     v,
		})
	}
	return variants
}

// generateUnionType generates a single union type struct for a property-level oneOf.
// typeNamePrefix is prepended to the generated Go type name to avoid
// package-scoped collisions for common property names like "config".
func (g *Generator) generateUnionType(prop *parser.Property, typeNamePrefix string) string {
	typeName := generatedUnionTypeName(prop, typeNamePrefix)
	variants := buildUnionVariants(prop, typeName)
	return emitDiscriminatedUnionCode(typeName, prop.Name, unionDiscriminatorJSONName(prop), variants)
}

func (g *Generator) writeUnionTypeDefinition(buf *strings.Builder, prop *parser.Property, typeNamePrefix string, emitted map[string]bool, entityName string, parentCursor *config.FieldConfig) {
	if prop == nil || len(prop.OneOf) == 0 {
		return
	}

	unionTypeName := generatedUnionTypeName(prop, typeNamePrefix)
	g.writeAnonymousUnionVariantTypes(buf, prop, unionTypeName, emitted, entityName, parentCursor)

	if emitted[unionTypeName] {
		return
	}
	emitted[unionTypeName] = true
	buf.WriteString(g.generateUnionType(prop, typeNamePrefix))
}

func (g *Generator) writeAnonymousUnionVariantTypes(buf *strings.Builder, prop *parser.Property, unionTypeName string, emitted map[string]bool, entityName string, parentCursor *config.FieldConfig) {
	for _, variant := range buildUnionVariants(prop, unionTypeName) {
		if variant.source == nil || variant.source.RefName != "" {
			continue
		}
		g.writeAnonymousUnionVariantType(buf, variant, emitted, entityName, parentCursor.Sub(jsonName(variant.discValue)), prop.Discriminator)
	}
}

func (g *Generator) writeAnonymousUnionVariantType(buf *strings.Builder, variant unionVariant, emitted map[string]bool, entityName string, parentCursor *config.FieldConfig, parentDiscriminator string) {
	if variant.source == nil {
		return
	}
	if emitted[variant.goTypeName] {
		return
	}
	emitted[variant.goTypeName] = true

	if !isInlineObjectWithProperties(variant.source) {
		buf.WriteString(formatSchemaComment(variant.goTypeName, variant.source.Description))
		fmt.Fprintf(buf, "type %s %s\n\n", variant.goTypeName, anonymousUnionVariantGoType(variant.source))
		return
	}

	buf.WriteString(formatSchemaComment(variant.goTypeName, variant.source.Description))
	fmt.Fprintf(buf, "type %s struct {\n", variant.goTypeName)
	for _, nested := range variant.source.Properties {
		if g.shouldSkipSchemaProperty(variant.goTypeName, nested) || (parentDiscriminator != "" && nested.Name == parentDiscriminator) {
			continue
		}
		g.writeSchemaTypeField(buf, nested, variant.goTypeName, parentCursor)
	}
	buf.WriteString("}\n\n")

	g.writeNestedInlineTypes(buf, variant.source.Properties, emitted, entityName, variant.goTypeName, parentCursor)
	for _, nested := range variant.source.Properties {
		if g.shouldSkipSchemaProperty(variant.goTypeName, nested) || len(nested.OneOf) == 0 {
			continue
		}
		g.writeUnionTypeDefinition(buf, nested, variant.goTypeName, emitted, entityName, parentCursor.Sub(jsonTagForProperty(nested)))
	}
	if wrapper := emitUnionWrapperUnmarshalJSON(variant.goTypeName, buildUnionFieldSpecs(variant.source.Properties, variant.goTypeName)); wrapper != "" {
		buf.WriteString(wrapper)
	}
}

func anonymousUnionVariantGoType(prop *parser.Property) string {
	if prop == nil {
		return "any"
	}

	switch prop.Type {
	case "string":
		return "string"
	case "integer":
		switch prop.Format {
		case "int32":
			return "int32"
		case "int64":
			return "int64"
		default:
			return "int"
		}
	case "number":
		switch prop.Format {
		case "float":
			return "float32"
		case "double":
			return "float64"
		default:
			return "float64"
		}
	case "boolean":
		return "string"
	case "array":
		if prop.Items == nil {
			return "[]any"
		}
		return "[]" + anonymousUnionVariantGoType(prop.Items)
	case "object":
		if prop.AdditionalProperties != nil {
			return "map[string]" + anonymousUnionVariantGoType(prop.AdditionalProperties)
		}
		return "apiextensionsv1.JSON"
	default:
		return "any"
	}
}

type unionFieldVariant struct {
	DiscValue   string
	FieldName   string
	TypeConst   string
	TestPayload string
}

type unionFieldSpec struct {
	FieldName              string
	JSONName               string
	Inline                 bool
	TypeName               string
	DiscriminatorJSONName  string
	DiscriminatorFieldName string
	Variants               []unionFieldVariant
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
	result := make([]unionFieldVariant, 0, len(variants))
	for _, v := range variants {
		result = append(result, unionFieldVariant{
			DiscValue:   v.discValue,
			FieldName:   v.fieldName,
			TypeConst:   typeName + "Type" + v.fieldName,
			TestPayload: unionVariantTestPayload(v.source),
		})
	}

	return result
}

func unionVariantTestPayload(prop *parser.Property) string {
	if prop == nil || isInlineObjectWithProperties(prop) {
		return "{}"
	}

	switch prop.Type {
	case "array":
		return "[]"
	case "string", "boolean":
		return `""`
	case "integer", "number":
		return "0"
	case "object":
		return "{}"
	default:
		return "{}"
	}
}

func buildUnionFieldSpec(fieldName, typeName, jsonName string, prop *parser.Property) unionFieldSpec {
	discriminatorJSONName := unionDiscriminatorJSONName(prop)
	return unionFieldSpec{
		FieldName:              fieldName,
		JSONName:               jsonName,
		Inline:                 jsonName == "",
		TypeName:               typeName,
		DiscriminatorJSONName:  discriminatorJSONName,
		DiscriminatorFieldName: fixInitialisms(goFieldName(discriminatorJSONName)),
		Variants:               buildUnionFieldVariants(buildUnionVariants(prop, typeName), typeName),
	}
}

func unionDiscriminatorJSONName(prop *parser.Property) string {
	if prop != nil && prop.Discriminator != "" {
		return jsonName(prop.Discriminator)
	}
	return "type"
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
		fmt.Fprintf(&buf, "\tif aux.%s != nil && aux.%s.%s == \"\"", field.FieldName, field.FieldName, field.DiscriminatorFieldName)
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

func emitUnionTests(pkgName string, unionSpecs []unionFieldSpec, wrapperSpecs []unionWrapperTestSpec, marshalTestTypes []string) string {
	if len(unionSpecs) == 0 && len(wrapperSpecs) == 0 && len(marshalTestTypes) == 0 {
		return ""
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "%s\n\npackage %s\n\n", sharedGeneratedFilePreamble, pkgName)
	buf.WriteString("import (\n")
	if len(wrapperSpecs) > 0 || len(marshalTestTypes) > 0 {
		buf.WriteString("\t\"encoding/json\"\n")
	}
	buf.WriteString("\t\"testing\"\n")
	buf.WriteString(")\n\n")

	for _, typeName := range marshalTestTypes {
		fmt.Fprintf(&buf, "func Test%s_MarshalEmpty(t *testing.T) {\n", typeName)
		buf.WriteString("\tt.Parallel()\n\n")
		fmt.Fprintf(&buf, "\tvar spec %s\n", typeName)
		buf.WriteString("\tout, err := json.Marshal(spec)\n")
		buf.WriteString("\tif err != nil {\n")
		buf.WriteString("\t\tt.Fatalf(\"json.Marshal() error = %v\", err)\n")
		buf.WriteString("\t}\n")
		buf.WriteString("\tif got, want := string(out), \"{}\"; got != want {\n")
		buf.WriteString("\t\tt.Fatalf(\"empty spec must marshal to {}: got %q, want %q\", got, want)\n")
		buf.WriteString("\t}\n")
		buf.WriteString("}\n\n")
	}

	for _, unionSpec := range unionSpecs {
		fmt.Fprintf(&buf, "func Test%sUnmarshalJSON_NilReceiver(t *testing.T) {\n", unionSpec.TypeName)
		buf.WriteString("\tt.Parallel()\n\n")
		buf.WriteString("\ttests := []struct {\n")
		buf.WriteString("\t\tname    string\n")
		buf.WriteString("\t\tpayload []byte\n")
		buf.WriteString("\t}{\n")
		for _, variant := range unionSpec.Variants {
			camelDiscValue := jsonName(variant.DiscValue)
			payload := fmt.Sprintf(`{%q:%q,%q:%s}`, unionSpec.DiscriminatorJSONName, camelDiscValue, camelDiscValue, variant.TestPayload)
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
				camelDiscValue := jsonName(variant.DiscValue)
				variantPayload := fmt.Sprintf(`{%q:%q,%q:%s}`, field.DiscriminatorJSONName, camelDiscValue, camelDiscValue, variant.TestPayload)
				payload := variantPayload
				if !field.Inline {
					payload = fmt.Sprintf(`{%q:%s}`, jsonName(field.JSONName), variantPayload)
				}
				fmt.Fprintf(&buf, "\t\t{\n")
				fmt.Fprintf(&buf, "\t\t\tname: %q,\n", field.FieldName+"/"+variant.DiscValue)
				fmt.Fprintf(&buf, "\t\t\tpayload: []byte(%q),\n", payload)
				fmt.Fprintf(&buf, "\t\t\tassert: func(t *testing.T, target %s) {\n", wrapperSpec.StructTypeName)
				buf.WriteString("\t\t\t\tt.Helper()\n")
				fmt.Fprintf(&buf, "\t\t\t\tif target.%s == nil {\n", field.FieldName)
				fmt.Fprintf(&buf, "\t\t\t\t\tt.Fatalf(%q)\n", field.FieldName+" should be allocated")
				buf.WriteString("\t\t\t\t}\n")
				fmt.Fprintf(&buf, "\t\t\t\tif got, want := target.%s.%s, %s; got != want {\n", field.FieldName, field.DiscriminatorFieldName, variant.TypeConst)
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
//   - A struct with a discriminator field and one optional pointer per variant.
//   - A string type alias for the discriminator + constants.
//   - Custom MarshalJSON/UnmarshalJSON that produce/consume a nested JSON object
//     {"<discriminator>":"<disc>","<disc>":{...variant fields...}} matching the CRD schema and
//     K8s/etcd wire format.
func emitDiscriminatedUnionCode(typeName, propName, discriminatorJSONName string, variants []unionVariant) string {
	discriminatorFieldName := fixInitialisms(goFieldName(discriminatorJSONName))

	discValues := make([]string, 0, len(variants))
	for _, v := range variants {
		discValues = append(discValues, jsonName(v.discValue))
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "// %s represents a union type for %s.\n", typeName, propName)
	fmt.Fprintf(&buf, "// Only one of the fields should be set based on the %s.\n", discriminatorFieldName)
	buf.WriteString("//\n")
	if shouldEmitTypeMatchCEL(typeName) {
		for _, v := range variants {
			camelDiscValue := jsonName(v.discValue)
			fmt.Fprintf(&buf, "// +kubebuilder:validation:XValidation:rule=\"self.%s == '%s' ? has(self.%s) : !has(self.%s)\",message=\"%s must be set only when %s is %s\"\n",
				discriminatorJSONName, camelDiscValue, camelDiscValue, camelDiscValue, camelDiscValue, discriminatorJSONName, camelDiscValue)
		}
	}
	fmt.Fprintf(&buf, "type %s struct {\n", typeName)

	fmt.Fprintf(&buf, "\t// %s designates the type of configuration.\n", discriminatorFieldName)
	buf.WriteString("\t//\n")
	buf.WriteString("\t// +required\n")
	buf.WriteString("\t// +kubebuilder:validation:MinLength=1\n")
	fmt.Fprintf(&buf, "\t// +kubebuilder:validation:Enum=%s\n", strings.Join(discValues, ";"))
	fmt.Fprintf(&buf, "\t%s %sType `json:\"%s,omitempty\"`\n\n", discriminatorFieldName, typeName, discriminatorJSONName)

	for _, v := range variants {
		fmt.Fprintf(&buf, "\t// %s configuration.\n", v.fieldName)
		buf.WriteString("\t//\n")
		buf.WriteString("\t// +optional\n")
		fmt.Fprintf(&buf, "\t%s *%s `json:\"%s,omitempty\"`\n", v.fieldName, v.goTypeName, jsonName(v.discValue))
	}
	buf.WriteString("}\n\n")

	fmt.Fprintf(&buf, "// %sType represents the type of %s.\n", typeName, propName)
	fmt.Fprintf(&buf, "type %sType string\n\n", typeName)

	fmt.Fprintf(&buf, "// %sType values.\n", typeName)
	buf.WriteString("const (\n")
	for _, v := range variants {
		fmt.Fprintf(&buf, "\t%sType%s %sType = \"%s\"\n", typeName, v.fieldName, typeName, jsonName(v.discValue))
	}
	buf.WriteString(")\n\n")

	// MarshalJSON: produce the nested shape that matches the CRD schema and
	// K8s/etcd wire format: {"<discriminator>":"<disc>","<disc>":{...variant fields...}}.
	fmt.Fprintf(&buf, "// MarshalJSON implements json.Marshaler.\n")
	fmt.Fprintf(&buf, "func (u %s) MarshalJSON() ([]byte, error) {\n", typeName)
	buf.WriteString("\tm := map[string]json.RawMessage{}\n")
	fmt.Fprintf(&buf, "\ttypeBytes, err := json.Marshal(string(u.%s))\n", discriminatorFieldName)
	buf.WriteString("\tif err != nil {\n")
	fmt.Fprintf(&buf, "\t\treturn nil, fmt.Errorf(\"marshaling %s %s: %%w\", err)\n", typeName, discriminatorJSONName)
	buf.WriteString("\t}\n")
	fmt.Fprintf(&buf, "\tm[%q] = typeBytes\n", discriminatorJSONName)
	fmt.Fprintf(&buf, "\tswitch u.%s {\n", discriminatorFieldName)
	for _, v := range variants {
		camelDiscValue := jsonName(v.discValue)
		fmt.Fprintf(&buf, "\tcase %sType%s:\n", typeName, v.fieldName)
		fmt.Fprintf(&buf, "\t\tif u.%s != nil {\n", v.fieldName)
		fmt.Fprintf(&buf, "\t\t\traw, err := json.Marshal(u.%s)\n", v.fieldName)
		buf.WriteString("\t\t\tif err != nil {\n")
		fmt.Fprintf(&buf, "\t\t\t\treturn nil, fmt.Errorf(\"marshaling %s %s: %%w\", err)\n", typeName, v.discValue)
		buf.WriteString("\t\t\t}\n")
		fmt.Fprintf(&buf, "\t\t\tm[\"%s\"] = raw\n", camelDiscValue)
		buf.WriteString("\t\t}\n")
	}
	buf.WriteString("\t}\n")
	buf.WriteString("\treturn json.Marshal(m)\n")
	buf.WriteString("}\n\n")

	// UnmarshalJSON: read the discriminator, then decode the variant
	// payload from raw["<discValue>"] to match the nested K8s wire shape.
	fmt.Fprintf(&buf, "// UnmarshalJSON implements json.Unmarshaler.\n")
	fmt.Fprintf(&buf, "func (u *%s) UnmarshalJSON(data []byte) error {\n", typeName)
	fmt.Fprintf(&buf, "\tif u == nil {\n")
	fmt.Fprintf(&buf, "\t\treturn fmt.Errorf(\"unmarshaling %s: nil receiver\")\n", typeName)
	buf.WriteString("\t}\n")
	buf.WriteString("\tvar probe struct {\n")
	fmt.Fprintf(&buf, "\t\t%s string `json:\"%s\"`\n", discriminatorFieldName, discriminatorJSONName)
	buf.WriteString("\t}\n")
	buf.WriteString("\tif err := json.Unmarshal(data, &probe); err != nil {\n")
	buf.WriteString("\t\treturn err\n")
	buf.WriteString("\t}\n")
	buf.WriteString("\tvar raw map[string]json.RawMessage\n")
	buf.WriteString("\tif err := json.Unmarshal(data, &raw); err != nil {\n")
	buf.WriteString("\t\treturn err\n")
	buf.WriteString("\t}\n")
	fmt.Fprintf(&buf, "\tu.%s = %sType(probe.%s)\n", discriminatorFieldName, typeName, discriminatorFieldName)
	fmt.Fprintf(&buf, "\tswitch probe.%s {\n", discriminatorFieldName)
	for _, v := range variants {
		camelDiscValue := jsonName(v.discValue)
		fmt.Fprintf(&buf, "\tcase \"%s\":\n", camelDiscValue)
		fmt.Fprintf(&buf, "\t\tpayload, ok := raw[\"%s\"]\n", camelDiscValue)
		buf.WriteString("\t\tif !ok || len(payload) == 0 {\n")
		buf.WriteString("\t\t\treturn nil\n")
		buf.WriteString("\t\t}\n")
		fmt.Fprintf(&buf, "\t\tvar val %s\n", v.goTypeName)
		buf.WriteString("\t\tif err := json.Unmarshal(payload, &val); err != nil {\n")
		fmt.Fprintf(&buf, "\t\t\treturn fmt.Errorf(\"unmarshaling %s %s: %%w\", err)\n", typeName, v.discValue)
		buf.WriteString("\t\t}\n")
		fmt.Fprintf(&buf, "\t\tu.%s = &val\n", v.fieldName)
	}
	buf.WriteString("\t}\n")
	buf.WriteString("\treturn nil\n")
	buf.WriteString("}\n")

	return buf.String()
}

func shouldEmitTypeMatchCEL(typeName string) bool {
	switch typeName {
	case "EncryptionKey", "EventGatewayEncryptConfigEncryptionKey":
		return true
	default:
		return false
	}
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

func uniqueUnionDiscriminatorValues(names []string) []string {
	return uniquifyUnionMemberNames(extractVariantNames(names), "Variant", jsonName)
}

func uniqueUnionFieldNames(names []string) []string {
	return uniquifyUnionMemberNames(extractVariantNames(names), "Variant", func(name string) string {
		return fixInitialisms(goFieldName(name))
	})
}

func uniquifyUnionMemberNames(names []string, fallback string, key func(string) string) []string {
	if len(names) == 0 {
		return nil
	}

	result := make([]string, len(names))
	counts := make(map[string]int, len(names))
	for i, name := range names {
		normalized := normalizeUnionMemberName(name, fallback)
		result[i] = normalized
		counts[key(normalized)]++
	}

	seen := make(map[string]int, len(counts))
	for i, name := range result {
		normalizedKey := key(name)
		if counts[normalizedKey] == 1 {
			continue
		}
		seen[normalizedKey]++
		result[i] = fmt.Sprintf("%s%d", name, seen[normalizedKey])
	}

	return result
}

func normalizeUnionMemberName(name, fallback string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || !startsWithLetter(trimmed) {
		return fallback
	}
	return trimmed
}

func startsWithLetter(name string) bool {
	for _, r := range name {
		return unicode.IsLetter(r)
	}
	return false
}

func anonymousUnionVariantTypeName(unionTypeName, fieldName string) string {
	return unionTypeName + fieldName
}

// emitDiscriminatedUnionType emits Go source for a ROOT-LEVEL oneOf schema that has
// an OAS discriminator.  It delegates to emitDiscriminatedUnionCode after building
// the variant list from the schema's DiscriminatorMapping.
func (g *Generator) emitDiscriminatedUnionType(goName string, schema *parser.Schema) string {
	variants := buildUnionVariants(buildRootUnionProperty(schema), goName)
	return emitDiscriminatedUnionCode(goName, goName, unionDiscriminatorJSONName(buildRootUnionProperty(schema)), variants)
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
			fmt.Fprintf(&buf, "\t%s %s `json:\"%s,omitempty\"`\n", fieldGoName, fieldGoType, jsonName(p.Name))
		} else {
			// Multi-property variant: embed as a pointer.
			fieldGoName := fixInitialisms(cleanSingleVariantName(refName))
			refTypeName := fixInitialisms(refName)
			fmt.Fprintf(&buf, "\t// +optional\n")
			fmt.Fprintf(&buf, "\t%s *%s `json:\"%s,omitempty\"`\n", fieldGoName, refTypeName, lowerCamelCase(fieldGoName))
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

func (g *Generator) collectEntityNames(parsed *parser.ParsedSpec) []string {
	var entityNames []string
	for name := range parsed.RequestBodies {
		entityNames = append(entityNames, parser.GetEntityNameFromType(name))
	}
	sort.Strings(entityNames)
	return entityNames
}

func (g *Generator) generateGroupVersionInfo() (string, error) {
	tmpl := template.Must(template.New("groupVersionInfo").Parse(groupVersionInfoTemplate))

	var buf strings.Builder
	data := struct {
		APIGroup   string
		APIVersion string
	}{
		APIGroup:   g.config.APIGroup,
		APIVersion: g.config.APIVersion,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (g *Generator) generateGroupVersionInfoGenerated(parsed *parser.ParsedSpec) (string, error) {
	tmpl := template.Must(template.New("groupVersionInfoGenerated").Parse(groupVersionInfoGeneratedTemplate))

	var buf strings.Builder
	data := struct {
		APIVersion  string
		EntityNames []string
	}{
		APIVersion:  g.config.APIVersion,
		EntityNames: g.collectEntityNames(parsed),
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

func (g *Generator) generateCommonTypes(typeCursors map[string]*config.FieldConfig) (string, error) {
	tmpl := template.Must(template.New("commonTypes").Parse(commonTypesTemplate))

	var buf strings.Builder
	hasSecretRefs := g.hasAnySecretRefs()
	var objectRefImport *config.ImportConfig
	if g.objectRefImported() && g.config.CommonTypes != nil && g.config.CommonTypes.ObjectRef != nil {
		objectRefImport = g.config.CommonTypes.ObjectRef.Import
	}
	var sensitiveCursor *config.FieldConfig
	if typeCursors != nil {
		sensitiveCursor = typeCursors[sensitiveDataSourceTypeName]
	}
	fieldValidations := func(fc *config.FieldConfig, fieldName string) []string {
		if fc == nil {
			return nil
		}
		child := fc.Sub(fieldName)
		if child == nil || len(child.Validations) == 0 {
			return nil
		}
		return append([]string(nil), child.Validations...)
	}
	data := struct {
		APIVersion                              string
		KonnectStatusImport                     *config.ImportConfig
		KonnectStatusType                       string
		ObjectRefImported                       bool
		ObjectRefImport                         *config.ImportConfig
		Namespaced                              bool
		HasSecretRefEntities                    bool
		SensitiveDataSourceValueMaxLength       int
		SensitiveDataSourceTypeValidations      []string
		SensitiveDataSourceSecretRefValidations []string
	}{
		APIVersion:                              g.config.APIVersion,
		KonnectStatusImport:                     defaultKonnectStatusImport(),
		KonnectStatusType:                       defaultKonnectStatusQualifiedTypeName(),
		ObjectRefImported:                       g.objectRefImported(),
		ObjectRefImport:                         objectRefImport,
		Namespaced:                              g.objectRefNamespaced(),
		HasSecretRefEntities:                    hasSecretRefs,
		SensitiveDataSourceValueMaxLength:       sensitiveDataSourceValueMaxLength,
		SensitiveDataSourceTypeValidations:      fieldValidations(sensitiveCursor, "type"),
		SensitiveDataSourceSecretRefValidations: fieldValidations(sensitiveCursor, "secretRef"),
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

func (g *Generator) schemaTypeObjectRefImportIfNeeded(typeName string, schema *parser.Schema) *config.ImportConfig {
	if !g.objectRefImported() {
		return nil
	}
	if g.schemaTypeUsesObjectRef(typeName, schema) {
		return g.config.CommonTypes.ObjectRef.Import
	}
	return nil
}

// TemplateReferenceConfig extends ReferenceConfig with Go-code computed fields
// suitable for use in templates.
type TemplateReferenceConfig struct {
	config.ReferenceConfig

	// GoFieldName is the Go struct field name derived from the last path segment,
	// e.g. "spec.apiSpec.destination" → "Destination".
	GoFieldName string
	// JSONFieldName is the JSON key used in the SDK payload after renameKeysToSDK,
	// e.g. "destination". Derived from the last segment of Path.
	JSONFieldName string
}

// templateReferences returns the references for an entity with computed Go field names.
func (g *Generator) templateReferences(entityName string) []TemplateReferenceConfig {
	refs := g.config.References[entityName]
	if len(refs) == 0 {
		return nil
	}
	result := make([]TemplateReferenceConfig, len(refs))
	for i, ref := range refs {
		tail := ref.Path
		if idx := strings.LastIndex(tail, "."); idx >= 0 {
			tail = tail[idx+1:]
		}
		result[i] = TemplateReferenceConfig{
			ReferenceConfig: ref,
			GoFieldName:     goFieldName(tail),
			JSONFieldName:   tail,
		}
	}
	return result
}

// referenceForField returns the ReferenceConfig for the given entity+field if
// that field is configured as an inter-CR reference, or nil otherwise.
// propName is the JSON/OpenAPI property name (e.g. "destination").
func (g *Generator) referenceForField(entityName, propName string) *config.ReferenceConfig {
	for i, ref := range g.config.References[entityName] {
		tail := ref.Path
		if idx := strings.LastIndex(tail, "."); idx >= 0 {
			tail = tail[idx+1:]
		}
		if tail == propName {
			return &g.config.References[entityName][i]
		}
	}
	return nil
}

// entityHasReferences returns true if the entity has at least one configured reference.
func (g *Generator) entityHasReferences(entityName string) bool {
	return len(g.config.References[entityName]) > 0
}

// entityHasParentRefReplacement returns true when the entity is configured with
// a ParentRef that replaces an API spec field. In that case the SDK request body
// builder must inject the replaced field from the resolved parent status ID.
func (g *Generator) entityHasParentRefReplacement(entityName string) bool {
	if g.config.ReconcilerConfig == nil {
		return false
	}
	rc, ok := g.config.ReconcilerConfig[entityName]
	if !ok || rc == nil || rc.ParentRef == nil {
		return false
	}
	return rc.ParentRef.ReplacesAPISpecField != ""
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

func (g *Generator) schemaTypeUsesObjectRef(typeName string, schema *parser.Schema) bool {
	if len(schema.Dependencies) > 0 {
		return true
	}
	for _, prop := range schema.Properties {
		if g.shouldSkipSchemaProperty(typeName, prop) {
			continue
		}
		if prop.IsReference {
			return true
		}
	}
	return false
}

func (g *Generator) shouldSkipSchemaProperty(typeName string, prop *parser.Property) bool {
	if prop == nil {
		return true
	}
	if skipProperty(prop) {
		return true
	}
	if len(g.config.SchemaFieldOmissions) == 0 {
		return false
	}
	fields := g.config.SchemaFieldOmissions[typeName]
	if len(fields) == 0 {
		return false
	}
	return fields[jsonName(prop.Name)]
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
	ImportPath  string

	NestedUnionFields []sdkOpsNestedUnionField

	// IsOperationsWrapped is true when the method's SDK type is in the operations
	// package (a fully-wrapped request struct combining path params and the JSON
	// body). In that case the conversion must unmarshal the payload into the body
	// component type and wrap it in the operations request struct, because the body
	// lives under a named (non-embedded) field that JSON payload keys don't match.
	IsOperationsWrapped   bool
	ComponentsImportAlias string
	BodyTypeName          string // e.g. "CreateAIGatewayConsumerCredentialRequest"
	BodyFieldName         string // field name on the operations request struct
	BodyFieldPointer      bool
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

// sdkOpsConstField represents a const discriminator that was stripped from the
// CRD struct but is required by the SDK request type, to be re-injected at the
// given payload path (e.g. Path=[anthropic config auth], Key="type", Value="basic").
type sdkOpsConstField struct {
	Label string
	Path  []string
	Key   string
	Value string
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
	UpdateDirectUnion     bool

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
	constFields := g.collectSDKOpsConstFields(schema)

	if hasRootOneOf(schema) {
		return g.generateRootUnionSDKOps(entityName, schema, opsConfig, imports, methods, boolFields, constFields)
	}

	componentsPath := "github.com/Kong/sdk-konnect-go/models/components"
	componentsImportAlias := sdkImportAlias(componentsPath)
	needComponentsImport := false
	standardMethods := make([]sdkOpsMethod, 0, len(methods))
	for _, method := range methods {
		method.NestedUnionFields = g.buildSDKOpsNestedUnionFields(schema, method)
		// When the SDK request type lives in the operations package it is a
		// fully-wrapped request struct (path params + a named body field). The
		// conversion must unmarshal into the body component type and wrap it,
		// because JSON payload keys don't match the wrapper's Go field names.
		if strings.Contains(method.ImportAlias, "sdkkonnectoper") {
			bodyInfo, err := ParseSDKRequestBodyInfo(method.ImportPath, method.TypeName)
			if err != nil {
				return "", fmt.Errorf("failed to inspect SDK request body for %s %s: %w", entityName, method.TypeName, err)
			}
			method.IsOperationsWrapped = true
			method.ComponentsImportAlias = componentsImportAlias
			method.BodyTypeName = bodyInfo.TypeName
			method.BodyFieldName = bodyInfo.FieldName
			method.BodyFieldPointer = bodyInfo.Pointer
			needComponentsImport = true
		}
		standardMethods = append(standardMethods, method)
	}
	if needComponentsImport {
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

	tmpl := template.Must(template.New("sdkops").Parse(sdkOpsTemplate))
	var buf strings.Builder
	rc := g.config.ReconcilerConfig[entityName]
	hasParentRefReplacement := rc != nil && rc.ParentRef != nil && rc.ParentRef.ReplacesAPISpecField != ""
	var parentRefReplacesField, parentStatusEntityName string
	if hasParentRefReplacement {
		parentRefReplacesField = rc.ParentRef.ReplacesAPISpecField
		parentStatusEntityName = parentRefStatusEntityName(rootRefDependency(schema), rc)
	}

	data := struct {
		APIVersion              string
		EntityName              string
		Imports                 []*sdkOpsImport
		BoolFields              []sdkOpsBoolField
		ConstFields             []sdkOpsConstField
		Methods                 []sdkOpsMethod
		NeedsClient             bool
		SecretReferences        []SecretReferenceForTemplate
		HasReferences           bool
		References              []TemplateReferenceConfig
		HasParentRefReplacement bool
		ParentRefReplacesField  string
		ParentStatusEntityName  string
	}{
		APIVersion:              g.config.APIVersion,
		EntityName:              entityName,
		Imports:                 imports,
		BoolFields:              boolFields,
		ConstFields:             constFields,
		Methods:                 standardMethods,
		NeedsClient:             opsConfig.RequireClient,
		SecretReferences:        g.templateSecretReferences(entityName),
		HasReferences:           g.entityHasReferences(entityName),
		References:              g.templateReferences(entityName),
		HasParentRefReplacement: hasParentRefReplacement,
		ParentRefReplacesField:  parentRefReplacesField,
		ParentStatusEntityName:  parentStatusEntityName,
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
	constFields []sdkOpsConstField,
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
			sdkOpsMethod: method,
			IsCreate:     isCreate,
		}
		m.IsOperationsWrapped = isOperationsWrapped
		m.ComponentsImportAlias = componentsImportAlias
		if isOperationsWrapped {
			bodyInfo, err := ParseSDKRequestBodyInfo(method.ImportPath, method.TypeName)
			if err != nil {
				return "", fmt.Errorf("failed to inspect SDK request body for %s %s: %w", entityName, method.TypeName, err)
			}
			m.BodyTypeName = bodyInfo.TypeName
			m.BodyFieldName = bodyInfo.FieldName
			m.BodyFieldPointer = bodyInfo.Pointer
		}
		rootUnionMethods = append(rootUnionMethods, m)
	}

	wrappedCreateBodyTypeName := ""
	createMethodTypeName := ""
	updateMethodTypeName := ""
	updateMethodImportPath := ""
	for _, method := range rootUnionMethods {
		if method.IsCreate && method.IsOperationsWrapped {
			wrappedCreateBodyTypeName = method.BodyTypeName
			continue
		}
		if method.IsCreate && createMethodTypeName == "" {
			createMethodTypeName = method.TypeName
			continue
		}
		if !method.IsCreate && updateMethodTypeName == "" {
			updateMethodTypeName = method.TypeName
			updateMethodImportPath = method.ImportPath
		}
	}

	// Determine whether the SDK update request type is itself a discriminated
	// union (like the create request) rather than a struct wrapping a nested
	// payload field. Introspecting the SDK type is authoritative; guessing from
	// the OAS shape misclassifies variants whose only required $ref property is a
	// scalar (e.g. a named string), which the SDK collapses to a plain type.
	updateSDKTypeIsUnion := false
	if hasUpdateMethod && !isOperationsWrapped && updateMethodTypeName != "" {
		memberFields, err := ParseSDKUnionMemberFieldNames(updateMethodImportPath, updateMethodTypeName)
		if err != nil {
			return "", fmt.Errorf("failed to inspect SDK update type %s for %s: %w", updateMethodTypeName, entityName, err)
		}
		updateSDKTypeIsUnion = len(memberFields) > 0
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
		discValue := discValueForRef[variantRefName]
		if discValue == "" {
			discValue = strings.ToLower(variantNames[i])
		}
		updatePayloadJSONName := ""
		updateTargetFieldName := ""
		updateVariantTypeName := ""
		updateConstructorName := ""
		updateDirectUnion := false
		fieldName := fixInitialisms(variantNames[i])
		if hasUpdateMethod && !isOperationsWrapped && updateSDKTypeIsUnion {
			// The SDK update request is a discriminated union (same shape as
			// create); rebuild the selected variant directly via its update
			// constructor instead of targeting a nested payload field.
			updateVariantTypeName = fixInitialisms(variantRefName)
			updateConstructorName = "Create" + updateMethodTypeName + fieldName
			updateDirectUnion = true
		} else if hasUpdateMethod && !isOperationsWrapped {
			updatePayloadProp, err := findRootUnionUpdatePayloadProperty(variant.Properties)
			if err != nil {
				// Heuristic: when a root-union update variant exposes multiple required
				// $ref payload members, the SDK update request is typically the union
				// itself rather than a wrapper around a nested payload field. In that
				// shape there is no single payload property to target, so we rebuild the
				// selected variant directly via the update constructor.
				if updateMethodTypeName == "" {
					return "", fmt.Errorf("failed to infer update payload property for %s variant %q: %w", entityName, variantRefName, err)
				}
				updateVariantTypeName = fixInitialisms(variantRefName)
				updateConstructorName = "Create" + updateMethodTypeName + fieldName
				updateDirectUnion = true
			} else {
				if updatePayloadProp == nil {
					return "", fmt.Errorf("failed to infer update payload property for %s variant %q: no ref payload property found", entityName, variantRefName)
				}
				updatePayloadJSONName = updatePayloadProp.Name
				updateTargetFieldName = goFieldName(updatePayloadProp.Name)
				updateVariantTypeName = fixInitialisms(strings.Replace(updatePayloadProp.RefName, "Create", "Update", 1))
				updateConstructorName = "Create" + goFieldName(updatePayloadProp.Name) + updateVariantTypeName
			}
		}

		// For operations-wrapped: compute wrapped constructor names using disc value.
		wrappedCreateConstructorName := ""
		if wrappedCreateBodyTypeName != "" {
			discPascal := fixInitialisms(pascalFromKebab(discValue))
			wrappedCreateConstructorName = "Create" + wrappedCreateBodyTypeName + discPascal
		}
		createConstructorName := "Create" + fixInitialisms(variantRefName)
		if createMethodTypeName != "" {
			createConstructorName = "Create" + createMethodTypeName + fieldName
		}
		variants = append(variants, sdkOpsRootUnionVariant{
			FieldName:                    fieldName,
			JSONName:                     discValue,
			TypeConstName:                fmt.Sprintf("%sType%s", rootUnionTypeName, fieldName),
			CreateVariantTypeName:        fixInitialisms(variantRefName),
			CreateConstructorName:        createConstructorName,
			UpdatePayloadJSONName:        updatePayloadJSONName,
			UpdateTargetFieldName:        updateTargetFieldName,
			UpdateVariantTypeName:        updateVariantTypeName,
			UpdateConstructorName:        updateConstructorName,
			UpdateDirectUnion:            updateDirectUnion,
			WrappedCreateConstructorName: wrappedCreateConstructorName,
		})
	}

	tmpl := template.Must(template.New("sdkops-root-union").Parse(sdkOpsRootUnionTemplate))
	var buf strings.Builder
	data := struct {
		APIVersion       string
		EntityName       string
		UnionTypeName    string
		Imports          []*sdkOpsImport
		BoolFields       []sdkOpsBoolField
		ConstFields      []sdkOpsConstField
		Methods          []sdkOpsRootUnionMethod
		Variants         []sdkOpsRootUnionVariant
		NeedsClient      bool
		SecretReferences []SecretReferenceForTemplate
	}{
		APIVersion:       g.config.APIVersion,
		EntityName:       entityName,
		UnionTypeName:    rootUnionTypeName,
		Imports:          imports,
		BoolFields:       boolFields,
		ConstFields:      constFields,
		Methods:          rootUnionMethods,
		Variants:         variants,
		NeedsClient:      opsConfig.RequireClient,
		SecretReferences: g.templateSecretReferences(entityName),
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
		testFields := g.buildSDKOpsTestFields(entityName, schema.Properties, method)
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

func (g *Generator) buildSDKOpsTestFields(entityName string, props []*parser.Property, method sdkOpsMethod) []sdkOpsTestField {
	testFields := make([]sdkOpsTestField, 0, len(props))
	for _, prop := range props {
		if skipProperty(prop) || prop.IsReference || shouldSkipSDKOpsTestField(prop, method) {
			continue
		}
		if ok := g.entityAPISpecSensitiveLeaf(entityName, jsonName(prop.Name)); ok {
			// SensitiveDataSource field: emit an inline value; after flattenSensitiveData
			// the JSON payload contains just the plain string.
			testFields = append(testFields, sdkOpsTestField{
				FieldName: goFieldName(prop.Name),
				// JSONName is the SDK payload key used in the generated
				// payload["..."] check; prop.Name is already the OAS snake_case name.
				JSONName:      prop.Name,
				TestValue:     `SensitiveDataSource{Type: SensitiveDataSourceTypeInline, Value: new("test-value")}`,
				ExpectedValue: fmt.Sprintf("%q", "test-value"),
			})
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

func buildSDKOpsMethodNames(opsConfig *config.EntityOpsConfig) (map[string]string, error) {
	methodNames := make(map[string]string)
	if opsConfig == nil {
		return methodNames, nil
	}

	opPaths := make(map[string]string)
	pathCounts := make(map[string]int)
	opNames := make([]string, 0, len(opsConfig.Ops))
	for opName := range opsConfig.Ops {
		if opName == "delete" {
			continue
		}
		opNames = append(opNames, opName)
	}
	sort.Strings(opNames)

	for _, opName := range opNames {
		opCfg := opsConfig.Ops[opName]
		if opCfg == nil {
			continue
		}
		if _, _, err := ParseSDKTypePath(opCfg.Path); err != nil {
			return nil, fmt.Errorf("operation %q: %w", opName, err)
		}
		opPaths[opName] = opCfg.Path
		pathCounts[opCfg.Path]++
	}

	for _, opName := range opNames {
		path, ok := opPaths[opName]
		if !ok {
			continue
		}
		_, typeName, err := ParseSDKTypePath(path)
		if err != nil {
			return nil, fmt.Errorf("operation %q: %w", opName, err)
		}
		methodName := "To" + typeName
		if pathCounts[path] > 1 {
			methodName = "To" + pascalFromKebab(opName) + typeName
		}
		methodNames[opName] = methodName
	}

	return methodNames, nil
}

func sdkOpsMethodNameForOp(opsConfig *config.EntityOpsConfig, opName string) (string, error) {
	methodNames, err := buildSDKOpsMethodNames(opsConfig)
	if err != nil {
		return "", err
	}
	methodName, ok := methodNames[opName]
	if !ok {
		return "", fmt.Errorf("operation %q has no SDK conversion method", opName)
	}
	return methodName, nil
}

// buildSDKOpsMethods parses the ops config and returns sorted imports and methods.
func (g *Generator) buildSDKOpsMethods(opsConfig *config.EntityOpsConfig) ([]*sdkOpsImport, []sdkOpsMethod, error) {
	imports := make(map[string]*sdkOpsImport)
	var methods []sdkOpsMethod
	methodNames, err := buildSDKOpsMethodNames(opsConfig)
	if err != nil {
		return nil, nil, err
	}

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
		if opCfg == nil {
			continue
		}
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
			MethodName:  methodNames[opName],
			TypeName:    typeName,
			ImportAlias: alias,
			ImportPath:  importPath,
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
		discValueForRef := make(map[string]string, len(schema.DiscriminatorMapping))
		for discValue, refName := range schema.DiscriminatorMapping {
			discValueForRef[refName] = discValue
		}
		for i, variant := range schema.OneOf {
			variantRefName := variant.Name
			if variant.RefName != "" {
				variantRefName = variant.RefName
			}
			variantJSONName := discValueForRef[variantRefName]
			if variantJSONName == "" {
				variantJSONName = strings.ToLower(variantNames[i])
			}
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

// collectSDKOpsConstFields finds discriminator fields that were stripped from
// the generated CRD structs (because the referenced schema is also used as a
// discriminated-union member) but are still required by the SDK request types.
// For a plain $ref (non-union) to such a schema, the single-value-enum
// discriminator (e.g. auth type="basic") has no CRD source and must be
// re-injected into the SDK payload at marshal time. Paths use OAS property
// names, which match the snake_case payload produced by renameKeysToSDK.
func (g *Generator) collectSDKOpsConstFields(schema *parser.Schema) []sdkOpsConstField {
	if schema == nil {
		return nil
	}

	discs := collectUnionMemberDiscriminators(g.parsed)
	var fields []sdkOpsConstField
	seen := make(map[string]struct{})

	for _, prop := range schema.Properties {
		g.collectSDKOpsConstFieldsFromProperty(prop, []string{prop.Name}, discs, map[string]struct{}{}, seen, &fields)
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
		discValueForRef := make(map[string]string, len(schema.DiscriminatorMapping))
		for discValue, refName := range schema.DiscriminatorMapping {
			discValueForRef[refName] = discValue
		}
		for i, variant := range schema.OneOf {
			variantRefName := variant.Name
			if variant.RefName != "" {
				variantRefName = variant.RefName
			}
			variantJSONName := discValueForRef[variantRefName]
			if variantJSONName == "" {
				variantJSONName = strings.ToLower(variantNames[i])
			}
			for _, nestedProp := range variant.Properties {
				g.collectSDKOpsConstFieldsFromProperty(nestedProp, []string{variantJSONName, nestedProp.Name}, discs, map[string]struct{}{}, seen, &fields)
			}
		}
	}

	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Label < fields[j].Label
	})

	return fields
}

func (g *Generator) collectSDKOpsConstFieldsFromProperty(
	prop *parser.Property,
	path []string,
	discs map[string]map[string]struct{},
	visited, seen map[string]struct{},
	fields *[]sdkOpsConstField,
) {
	if g == nil || g.parsed == nil || prop == nil || skipProperty(prop) {
		return
	}

	// A plain $ref (not a union at this position) to a schema whose discriminator
	// was stripped: re-inject the stripped single-value-enum discriminator here.
	if prop.RefName != "" && len(prop.OneOf) == 0 && len(prop.AnyOf) == 0 {
		if refSchema := g.parsed.Schemas[prop.RefName]; refSchema != nil {
			if stripped, ok := discs[prop.RefName]; ok {
				for _, refProp := range refSchema.Properties {
					if _, isStripped := stripped[refProp.Name]; !isStripped {
						continue
					}
					if len(refProp.Enum) != 1 {
						continue
					}
					label := strings.Join(path, ".") + "." + refProp.Name
					if _, dup := seen[label]; dup {
						continue
					}
					seen[label] = struct{}{}
					*fields = append(*fields, sdkOpsConstField{
						Label: label,
						Path:  append([]string(nil), path...),
						Key:   refProp.Name,
						Value: fmt.Sprintf("%v", refProp.Enum[0]),
					})
				}
			}
			// Recurse into the ref schema's own properties for deeper nested refs.
			if _, done := visited[prop.RefName]; !done {
				visited[prop.RefName] = struct{}{}
				for _, refProp := range refSchema.Properties {
					g.collectSDKOpsConstFieldsFromProperty(refProp, append(append([]string(nil), path...), refProp.Name), discs, visited, seen, fields)
				}
			}
		}
	}

	if prop.Items != nil {
		g.collectSDKOpsConstFieldsFromProperty(prop.Items, append(append([]string(nil), path...), "[]"), discs, visited, seen, fields)
	}
	for _, nestedProp := range prop.Properties {
		g.collectSDKOpsConstFieldsFromProperty(nestedProp, append(append([]string(nil), path...), nestedProp.Name), discs, visited, seen, fields)
	}
	if prop.AdditionalProperties != nil {
		g.collectSDKOpsConstFieldsFromProperty(prop.AdditionalProperties, append(append([]string(nil), path...), "{}"), discs, visited, seen, fields)
	}
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

	// Enum-typed string fields must use a valid enum value: some SDK enum types
	// reject unknown values during unmarshalling (unless x-speakeasy-unknown-values
	// is set), so a generic placeholder would fail the round-trip conversion.
	if len(prop.Enum) > 0 {
		enumVal := fmt.Sprintf("%v", prop.Enum[0])
		quoted := fmt.Sprintf("%q", enumVal)
		switch {
		case goType == "string":
			return quoted, quoted
		case goType == "*string":
			return fmt.Sprintf("new(%s)", quoted), quoted
		case prop.RefName != "" && prop.Type == "string":
			if elementType, ok := strings.CutPrefix(goType, "*"); ok {
				return fmt.Sprintf("new(%s(%s))", elementType, quoted), quoted
			}
			return fmt.Sprintf("%s(%s)", goType, quoted), quoted
		}
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
	case "[]string":
		return `[]string{"test-value"}`, `[]any{"test-value"}`
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
	return fixGoFieldInitialisms(strings.Join(parts, ""))
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

// fixGoFieldInitialisms is narrower than fixInitialisms: it also normalizes AI
// for generated Kubernetes field names, but intentionally does not add AI to
// isCommonAcronym because that helper is also used for SDK type/constructor
// naming where the upstream SDK still uses "Ai" in some identifiers.
func fixGoFieldInitialisms(name string) string {
	words := splitPascalCase(name)
	for i, word := range words {
		upper := strings.ToUpper(word)
		if upper == "AI" || isCommonAcronym(upper) {
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

// jsonName converts an OAS snake_case property name to camelCase for K8s JSON wire format.
// Rule: first segment always lowercase; subsequent segments use the acronym table
// (ID, URL, API, DNS, TLS, etc.) or PascalCase.
// Examples: "gateway_ref" → "gatewayRef", "default_api_visibility" → "defaultAPIVisibility",
// "organization_id" → "organizationID", "dns_label" → "dnsLabel".
func jsonName(s string) string {
	if s == "" {
		return s
	}
	// No underscores: already camelCase or single word — only lowercase first char.
	if !strings.Contains(s, "_") {
		return strings.ToLower(s[:1]) + s[1:]
	}
	parts := strings.Split(s, "_")
	result := make([]string, len(parts))
	// First segment always lowercase (no acronym caps for first position).
	result[0] = strings.ToLower(parts[0])
	// Subsequent segments: acronym caps if in table, else PascalCase first letter.
	for i, part := range parts[1:] {
		if len(part) == 0 {
			continue
		}
		upper := strings.ToUpper(part)
		if isCommonAcronym(upper) {
			result[i+1] = upper
		} else {
			result[i+1] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(result, "")
}

// lowerCamelCase converts a PascalCase or ALL_CAPS string to lowerCamelCase.
// ALL-caps strings (acronyms like "OIDC", "SAML") become fully lowercase.
// Mixed-case strings get only their first letter lowercased.
func lowerCamelCase(s string) string {
	if s == "" {
		return s
	}
	if strings.ToUpper(s) == s {
		return strings.ToLower(s)
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// tagOmitSuffix returns the JSON tag omit suffix appropriate for a resolved Go type.
// Pointer, slice, and map types use omitempty (zero value is nil/empty, omitempty works).
// All other types (named structs, primitives, type aliases) use omitzero — omitempty
// has no effect on struct types, producing {"namespace":{}} for zero-valued struct fields.
func tagOmitSuffix(resolvedGoType string) string {
	if strings.HasPrefix(resolvedGoType, "*") ||
		strings.HasPrefix(resolvedGoType, "[]") ||
		strings.HasPrefix(resolvedGoType, "map[") {
		return ",omitempty"
	}
	return ",omitzero"
}

// jsonTag generates the json struct tag using camelCase for K8s wire format.
// resolvedGoType is the Go type string as it appears in the emitted field declaration
// (e.g. "string", "*KonnectEntityRef", "[]Foo", "VirtualClusterNamespace").
func jsonTag(prop *parser.Property, resolvedGoType string) string {
	return jsonName(prop.Name) + tagOmitSuffix(resolvedGoType)
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

// schemaUsesJSON checks if any property in the schema, including nested $refs,
// will be generated as apiextensionsv1.JSON.
func schemaUsesJSON(g *Generator, schema *parser.Schema, parsed *parser.ParsedSpec) bool {
	return schemaUsesJSONRecursive(g, schema, parsed, make(map[string]bool))
}

func schemaUsesJSONInCRDTypeFile(g *Generator, schema *parser.Schema) bool {
	if schema == nil {
		return false
	}
	for _, prop := range schema.Properties {
		if propertyUsesJSONDirect(g, prop) {
			return true
		}
	}
	return false
}

func schemaUsesJSONRecursive(g *Generator, schema *parser.Schema, parsed *parser.ParsedSpec, seenRefs map[string]bool) bool {
	for _, prop := range schema.Properties {
		if propertyUsesJSON(g, prop, parsed, seenRefs) {
			return true
		}
	}
	for _, variant := range schema.OneOf {
		if propertyUsesJSON(g, variant, parsed, seenRefs) {
			return true
		}
	}
	for _, variant := range schema.AnyOf {
		if propertyUsesJSON(g, variant, parsed, seenRefs) {
			return true
		}
	}
	return false
}

func propertyUsesJSONDirect(g *Generator, prop *parser.Property) bool {
	if prop == nil || skipProperty(prop) {
		return false
	}
	if strings.Contains(g.goType(prop), "apiextensionsv1.JSON") {
		return true
	}
	if prop.RefName != "" {
		return false
	}
	for _, variant := range prop.OneOf {
		if propertyUsesJSONDirect(g, variant) {
			return true
		}
	}
	for _, variant := range prop.AnyOf {
		if propertyUsesJSONDirect(g, variant) {
			return true
		}
	}
	if propertyUsesJSONDirect(g, prop.Items) {
		return true
	}
	if propertyUsesJSONDirect(g, prop.AdditionalProperties) {
		return true
	}
	for _, nested := range prop.Properties {
		if propertyUsesJSONDirect(g, nested) {
			return true
		}
	}
	return false
}

func propertyUsesJSON(g *Generator, prop *parser.Property, parsed *parser.ParsedSpec, seenRefs map[string]bool) bool {
	if prop == nil || skipProperty(prop) {
		return false
	}
	if prop.RefName != "" && parsed != nil && !seenRefs[prop.RefName] {
		if refSchema, ok := parsed.Schemas[prop.RefName]; ok {
			seenRefs[prop.RefName] = true
			if schemaUsesJSONRecursive(g, refSchema, parsed, seenRefs) {
				return true
			}
		}
	}
	if strings.Contains(g.goType(prop), "apiextensionsv1.JSON") {
		return true
	}
	for _, variant := range prop.OneOf {
		if propertyUsesJSON(g, variant, parsed, seenRefs) {
			return true
		}
	}
	for _, variant := range prop.AnyOf {
		if propertyUsesJSON(g, variant, parsed, seenRefs) {
			return true
		}
	}
	if propertyUsesJSON(g, prop.Items, parsed, seenRefs) {
		return true
	}
	if propertyUsesJSON(g, prop.AdditionalProperties, parsed, seenRefs) {
		return true
	}
	for _, nested := range prop.Properties {
		if propertyUsesJSON(g, nested, parsed, seenRefs) {
			return true
		}
	}
	return false
}
