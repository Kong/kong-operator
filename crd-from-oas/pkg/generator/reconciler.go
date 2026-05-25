package generator

import (
	"fmt"
	"sort"
	"strings"
	"text/template"
	"unicode"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// watchTemplate generates watch options for a single entity.
const watchTemplate = sharedGeneratedFilePreamble + `

package konnect

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

{{- if .NeedsSeparateAPIAuthImport}}
	{{.APIAuthPackageAlias}} "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	{{.APIGroupPackageAlias}} "{{.APIGroupPackagePath}}"
{{- else}}
	{{.APIGroupPackageAlias}} "{{.APIGroupPackagePath}}"
{{- end}}
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// {{.EntityName}}ReconciliationWatchOptions returns the watch options for
// the {{.EntityName}}.
func {{.EntityName}}ReconciliationWatchOptions(
	cl client.Client,
) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&{{.APIGroupPackageAlias}}.{{.EntityName}}{})
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&{{.APIAuthPackageAlias}}.KonnectAPIAuthConfiguration{},
				handler.EnqueueRequestsFromMapFunc(
					enqueue{{.EntityName}}ForKonnectAPIAuthConfiguration(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongReferenceGrant{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectsForKongReferenceGrant[{{.APIGroupPackageAlias}}.{{.EntityName}}List](cl),
				),
			)
		},
	}
}

func enqueue{{.EntityName}}ForKonnectAPIAuthConfiguration(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		auth, ok := obj.(*{{.APIAuthPackageAlias}}.KonnectAPIAuthConfiguration)
		if !ok {
			return nil
		}
		var l {{.APIGroupPackageAlias}}.{{.EntityName}}List
		if err := cl.List(ctx, &l, client.MatchingFields{
			index.IndexField{{.EntityName}}OnAPIAuthConfiguration: auth.Namespace + "/" + auth.Name,
		}); err != nil {
			return nil
		}
		return objectListToReconcileRequests(l.Items)
	}
}
`

// indexTemplate generates cache index for auth ref queries.
const indexTemplate = sharedGeneratedFilePreamble + `

package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	{{.APIGroupPackageAlias}} "{{.APIGroupPackagePath}}"
)

const (
	// IndexField{{.EntityName}}OnAPIAuthConfiguration is the index field for {{.EntityName}} -> APIAuthConfiguration.
	IndexField{{.EntityName}}OnAPIAuthConfiguration = "{{.EntityNameLowerCamel}}APIAuthConfigurationRef"
)

// OptionsFor{{.EntityName}} returns required Index options for {{.EntityName}} reconciler.
func OptionsFor{{.EntityName}}() []Option {
	return []Option{
		{
			Object:         &{{.APIGroupPackageAlias}}.{{.EntityName}}{},
			Field:          IndexField{{.EntityName}}OnAPIAuthConfiguration,
			ExtractValueFn: {{.EntityNameLowerCamel}}APIAuthConfigurationRef,
		},
	}
}

func {{.EntityNameLowerCamel}}APIAuthConfigurationRef(object client.Object) []string {
	ent, ok := object.(*{{.APIGroupPackageAlias}}.{{.EntityName}})
	if !ok {
		return nil
	}
	if ent.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name == "" {
		return nil
	}

	return []string{ent.GetNamespace() + "/" + ent.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name}
}
`

// rbacEntity holds the data needed to generate RBAC markers for a single entity.
type rbacEntity struct {
	APIGroup     string
	ResourceName string
}

// generateRBAC generates a Go file containing +kubebuilder:rbac marker comments
// for the given entities. The file is placed in the controller/konnect package
// so that controller-gen picks up the markers when generating RBAC manifests.
func (g *Generator) generateRBAC(entityNames []string) (string, error) {
	entities := make([]rbacEntity, 0, len(entityNames))
	for _, entityName := range entityNames {
		entities = append(entities, rbacEntity{
			APIGroup:     g.config.APIGroup,
			ResourceName: g.resourceNameForKind(entityName),
		})
	}

	tmpl := template.Must(template.New("rbac").Parse(rbacTemplate))

	var buf strings.Builder
	data := struct {
		Entities []rbacEntity
	}{
		Entities: entities,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (g *Generator) resourceNameForKind(kind string) string {
	// UnsafeGuessKindToResource is good enough for generated RBAC markers, but
	// it incorrectly pluralizes Gateway kinds as gatewaies.
	gvr, _ := meta.UnsafeGuessKindToResource(schema.GroupVersionKind{
		Group:   g.config.APIGroup,
		Version: g.config.APIVersion,
		Kind:    kind,
	})

	resourceName := gvr.Resource
	if strings.HasSuffix(kind, "Gateway") && strings.HasSuffix(resourceName, "gatewaies") {
		return strings.TrimSuffix(resourceName, "gatewaies") + "gateways"
	}

	return resourceName
}

const childWatchTemplate = sharedGeneratedFilePreamble + `

package konnect

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	{{.APIGroupPackageAlias}} "{{.APIGroupPackagePath}}"
	{{- if .NeedsSeparateParentImport}}
	{{.ParentAPIGroupPackageAlias}} "{{.ParentAPIGroupPackagePath}}"
	{{- end}}
	{{- if ne .APIGroupPackagePath "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"}}
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	{{- end}}
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// {{.EntityName}}ReconciliationWatchOptions returns the watch options for
// the {{.EntityName}}.
func {{.EntityName}}ReconciliationWatchOptions(
	cl client.Client,
) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&{{.APIGroupPackageAlias}}.{{.EntityName}}{})
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&{{.ParentAPIGroupPackageAlias}}.{{.ParentEntityName}}{},
				handler.EnqueueRequestsFromMapFunc(
					enqueue{{.EntityName}}For{{.ParentEntityName}}(cl),
				),
			)
		},
		{{- range .CrossRefs}}
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&{{$.APIGroupPackageAlias}}.{{.RefKind}}{},
				handler.EnqueueRequestsFromMapFunc(
					enqueue{{$.EntityName}}For{{.RefKind}}(cl),
				),
			)
		},
		{{- end}}
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongReferenceGrant{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectsForKongReferenceGrant[{{.APIGroupPackageAlias}}.{{.EntityName}}List](cl),
				),
			)
		},
	}
}

func enqueue{{.EntityName}}For{{.ParentEntityName}}(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		parent, ok := obj.(*{{.ParentAPIGroupPackageAlias}}.{{.ParentEntityName}})
		if !ok {
			return nil
		}
		var l {{.APIGroupPackageAlias}}.{{.EntityName}}List
		if err := cl.List(ctx, &l, client.MatchingFields{
			index.IndexField{{.EntityName}}On{{.ParentEntityName}}Ref: client.ObjectKeyFromObject(parent).String(),
		}); err != nil {
			return nil
		}
		return objectListToReconcileRequests(l.Items)
	}
}
{{range .CrossRefs}}
func enqueue{{$.EntityName}}For{{.RefKind}}(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		ref, ok := obj.(*{{$.APIGroupPackageAlias}}.{{.RefKind}})
		if !ok {
			return nil
		}
		var l {{$.APIGroupPackageAlias}}.{{$.EntityName}}List
		if err := cl.List(ctx, &l, client.MatchingFields{
			index.IndexField{{$.EntityName}}On{{.RefKind}}Ref: client.ObjectKeyFromObject(ref).String(),
		}); err != nil {
			return nil
		}
		return objectListToReconcileRequests(l.Items)
	}
}
{{end}}`

const childIndexTemplate = sharedGeneratedFilePreamble + `

package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	{{.APIGroupPackageAlias}} "{{.APIGroupPackagePath}}"
)

const (
	// IndexField{{.EntityName}}On{{.ParentEntityName}}Ref is the index field for {{.EntityName}} -> {{.ParentEntityName}}.
	IndexField{{.EntityName}}On{{.ParentEntityName}}Ref = "{{.EntityNameLowerCamel}}On{{.ParentEntityName}}Ref"
	{{- range .CrossRefs}}
	// IndexField{{$.EntityName}}On{{.RefKind}}Ref is the index field for {{$.EntityName}} -> {{.RefKind}}.
	IndexField{{$.EntityName}}On{{.RefKind}}Ref = "{{$.EntityNameLowerCamel}}On{{.RefKind}}Ref"
	{{- end}}
)

// OptionsFor{{.EntityName}} returns required Index options for {{.EntityName}} reconciler.
func OptionsFor{{.EntityName}}() []Option {
	return []Option{
		{
			Object:         &{{.APIGroupPackageAlias}}.{{.EntityName}}{},
			Field:          IndexField{{.EntityName}}On{{.ParentEntityName}}Ref,
			ExtractValueFn: {{.EntityNameLowerCamel}}On{{.ParentEntityName}}Ref,
		},
		{{- range .CrossRefs}}
		{
			Object:         &{{$.APIGroupPackageAlias}}.{{$.EntityName}}{},
			Field:          IndexField{{$.EntityName}}On{{.RefKind}}Ref,
			ExtractValueFn: {{$.EntityNameLowerCamel}}On{{.RefKind}}Ref,
		},
		{{- end}}
	}
}

func {{.EntityNameLowerCamel}}On{{.ParentEntityName}}Ref(object client.Object) []string {
	ent, ok := object.(*{{.APIGroupPackageAlias}}.{{.EntityName}})
	if !ok {
		return nil
	}
	if ent.Spec.{{.ParentRefFieldName}}.NamespacedRef == nil {
		return nil
	}

	refNamespace := ent.GetNamespace()
	if ent.Spec.{{.ParentRefFieldName}}.NamespacedRef.Namespace != nil && *ent.Spec.{{.ParentRefFieldName}}.NamespacedRef.Namespace != "" {
		refNamespace = *ent.Spec.{{.ParentRefFieldName}}.NamespacedRef.Namespace
	}

	return []string{refNamespace + "/" + ent.Spec.{{.ParentRefFieldName}}.NamespacedRef.Name}
}
{{range .CrossRefs}}
func {{$.EntityNameLowerCamel}}On{{.RefKind}}Ref(object client.Object) []string {
	ent, ok := object.(*{{$.APIGroupPackageAlias}}.{{$.EntityName}})
	if !ok {
		return nil
	}
	if ent.{{.GoFieldPath}} == nil || ent.{{.GoFieldPath}}.NamespacedRef == nil {
		return nil
	}
	refNamespace := ent.GetNamespace()
	if ent.{{.GoFieldPath}}.NamespacedRef.Namespace != nil && *ent.{{.GoFieldPath}}.NamespacedRef.Namespace != "" {
		refNamespace = *ent.{{.GoFieldPath}}.NamespacedRef.Namespace
	}
	return []string{refNamespace + "/" + ent.{{.GoFieldPath}}.NamespacedRef.Name}
}
{{end}}`

const reconcilerConditionsTemplate = sharedGeneratedFilePreamble + `

package {{.APIVersion}}

const (
{{ range $i, $group := .ConditionGroups }}{{ if $i }}
{{ end }}	// {{$group.Prefix}}RefValidConditionType is the type of the condition that indicates
	// whether the {{$group.Prefix}} reference is valid and points to an existing
	// {{$group.ReferencedEntityName}}.
	{{$group.Prefix}}RefValidConditionType = "{{$group.Prefix}}RefValid"

	// {{$group.Prefix}}RefReasonValid is the reason used with the {{$group.Prefix}}RefValid
	// condition type indicating that the {{$group.Prefix}} reference is valid.
	{{$group.Prefix}}RefReasonValid = "Valid"
	// {{$group.Prefix}}RefReasonInvalid is the reason used with the {{$group.Prefix}}RefValid
	// condition type indicating that the {{$group.Prefix}} reference is invalid.
	{{$group.Prefix}}RefReasonInvalid = "Invalid"
	// {{$group.Prefix}}RefReasonNotProgrammed is the reason used with the {{$group.Prefix}}RefValid
	// condition type indicating that the referenced {{$group.ReferencedEntityName}} exists but is not
	// yet programmed in Konnect.
	{{$group.Prefix}}RefReasonNotProgrammed = "NotProgrammed"
{{ end }})
`

// crossRefWatchData holds per-cross-reference metadata for watch/index templates.
type crossRefWatchData struct {
	// RefKind is the referenced entity kind, e.g. "EventGatewayBackendCluster".
	RefKind string
	// GoFieldPath is the Go struct field accessor from the entity root,
	// e.g. "Spec.APISpec.Destination".
	GoFieldPath string
}

type reconcilerEntityMetadata struct {
	EntityName                 string
	EntityNameLowerCamel       string
	ParentEntityName           string
	ParentRefFieldName         string
	APIGroupPackagePath        string
	APIGroupPackageAlias       string
	ParentAPIGroupPackagePath  string
	ParentAPIGroupPackageAlias string
}

type reconcilerConditionGroup struct {
	Prefix               string
	ReferencedEntityName string
}

// generateReconcilerFiles generates all reconciler wiring files for the given entities.
func (g *Generator) generateReconcilerFiles(entityNames []string, entitySchemas map[string]*parser.Schema) ([]GeneratedFile, error) {
	var files []GeneratedFile

	// Generate RBAC markers file for all reconciler entities.
	rbacContent, err := g.generateRBAC(entityNames)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RBAC for reconciler entities: %w", err)
	}
	files = append(files, GeneratedFile{
		Name:        "zz_generated_reconciler_generic_rbac_" + g.config.APIGroupPackageAlias + ".go",
		Content:     rbacContent,
		RelativeDir: "controller/konnect",
	})

	// Generate per-entity watch and index files
	for _, entityName := range entityNames {
		rc := g.config.ReconcilerConfig[entityName]
		schema, ok := entitySchemas[entityName]
		if !ok {
			return nil, fmt.Errorf("missing schema for reconciler entity %s", entityName)
		}

		metadata, err := g.reconcilerEntityMetadata(entityName, schema, rc)
		if err != nil {
			return nil, fmt.Errorf("failed to build reconciler metadata for %s: %w", entityName, err)
		}

		// Watch file → controller/konnect/
		watchContent, err := g.generateWatch(metadata, rc)
		if err != nil {
			return nil, fmt.Errorf("failed to generate watch for %s: %w", entityName, err)
		}
		files = append(files, GeneratedFile{
			Name:        "zz_generated_watch_" + EntityFilePrefix(entityName) + ".go",
			Content:     watchContent,
			RelativeDir: "controller/konnect",
		})
		g.watchInfos = append(g.watchInfos, &WatchFileInfo{
			Entity:         entityName,
			APIAlias:       g.config.APIGroupPackageAlias,
			APIPackagePath: g.config.APIGroupPackagePath,
			IsRoot:         rc.GetIsRoot(),
		})

		// Index file → internal/utils/index/
		indexContent, err := g.generateIndex(metadata, rc)
		if err != nil {
			return nil, fmt.Errorf("failed to generate index for %s: %w", entityName, err)
		}
		files = append(files, GeneratedFile{
			Name:        "zz_generated_" + EntityFilePrefix(entityName) + ".go",
			Content:     indexContent,
			RelativeDir: "internal/utils/index",
		})
	}

	return files, nil
}

// generateReconcilerConditions emits shared ref condition constants for
// non-root reconciler entities, deduplicated by generated ref prefix.
func (g *Generator) generateReconcilerConditions(parsed *parser.ParsedSpec) (*GeneratedFile, error) {
	if len(g.config.ReconcilerConfig) == 0 {
		return nil, nil
	}

	groupMap := make(map[string]reconcilerConditionGroup)
	requestBodyNames := make([]string, 0, len(parsed.RequestBodies))
	for name := range parsed.RequestBodies {
		requestBodyNames = append(requestBodyNames, name)
	}
	sort.Strings(requestBodyNames)

	for _, name := range requestBodyNames {
		schema := parsed.RequestBodies[name]
		entityName := parser.GetEntityNameFromType(name)

		rc, ok := g.config.ReconcilerConfig[entityName]
		if !ok || (rc.IsRoot != nil && *rc.IsRoot) {
			continue
		}

		metadata, err := g.reconcilerEntityMetadata(entityName, schema, rc)
		if err != nil {
			return nil, fmt.Errorf("failed to build reconciler metadata for %s: %w", entityName, err)
		}

		parentDep := rootRefDependency(schema)
		if parentDep == nil {
			return nil, fmt.Errorf("non-root entity %s has no parent dependency", entityName)
		}
		prefix := refConditionEntityName(parentDep)
		// When parentRef overrides the immediate parent, use the configured
		// parent entity kind as the condition prefix so that condition constants
		// reflect the actual referenced type (e.g. EventGatewayBackendCluster)
		// rather than the OpenAPI-derived ancestor (e.g. EventGateway).
		if rc.ParentRef != nil && rc.ParentEntityKind() != "" {
			prefix = rc.ParentEntityKind()
		}
		if prefix == "" {
			return nil, fmt.Errorf("failed to derive condition prefix for %s", entityName)
		}

		group := reconcilerConditionGroup{
			Prefix:               prefix,
			ReferencedEntityName: metadata.ParentEntityName,
		}
		if existing, ok := groupMap[prefix]; ok {
			if existing.ReferencedEntityName != group.ReferencedEntityName {
				return nil, fmt.Errorf(
					"condition prefix %q maps to both %q and %q",
					prefix,
					existing.ReferencedEntityName,
					group.ReferencedEntityName,
				)
			}
			continue
		}
		groupMap[prefix] = group
	}

	// Also add condition groups for cross-reference kinds (references: config).
	// Each unique referenced kind gets its own condition prefix.
	for entityName, refs := range g.config.References {
		_ = entityName
		for _, ref := range refs {
			if existing, ok := groupMap[ref.Kind]; ok {
				if existing.ReferencedEntityName != ref.Kind {
					return nil, fmt.Errorf(
						"cross-reference condition prefix %q maps to both %q and %q",
						ref.Kind,
						existing.ReferencedEntityName,
						ref.Kind,
					)
				}
				continue
			}
			groupMap[ref.Kind] = reconcilerConditionGroup{
				Prefix:               ref.Kind,
				ReferencedEntityName: ref.Kind,
			}
		}
	}

	if len(groupMap) == 0 {
		return nil, nil
	}

	// Sort prefixes to keep generated output deterministic while preserving the
	// map-based deduplication above.
	prefixes := make([]string, 0, len(groupMap))
	for prefix := range groupMap {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)

	conditionGroups := make([]reconcilerConditionGroup, 0, len(prefixes))
	for _, prefix := range prefixes {
		conditionGroups = append(conditionGroups, groupMap[prefix])
	}

	tmpl := template.Must(template.New("reconcilerConditions").Parse(reconcilerConditionsTemplate))
	var buf strings.Builder
	data := struct {
		APIVersion      string
		ConditionGroups []reconcilerConditionGroup
	}{
		APIVersion:      g.config.APIVersion,
		ConditionGroups: conditionGroups,
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}

	return &GeneratedFile{
		Name:    "zz_generated_reconciler_conditions.go",
		Content: buf.String(),
	}, nil
}

func (g *Generator) generateWatch(metadata reconcilerEntityMetadata, rc *config.ReconcilerConfig) (string, error) {
	tmpl := template.Must(template.New("watch").Parse(watchTemplate))
	if !rc.GetIsRoot() {
		tmpl = template.Must(template.New("childWatch").Parse(childWatchTemplate))
	}

	const konnectAPIAuthPackagePath = "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"

	apiAuthPackageAlias := g.config.APIGroupPackageAlias
	needsSeparateAPIAuthImport := g.config.APIGroupPackagePath != konnectAPIAuthPackagePath
	if needsSeparateAPIAuthImport {
		apiAuthPackageAlias = "konnectapiauthv1alpha1"
	}

	crossRefs := g.buildCrossRefWatchData(metadata.EntityName)

	var buf strings.Builder
	data := struct {
		EntityName                 string
		EntityNameLowerCamel       string
		ParentEntityName           string
		ParentRefFieldName         string
		APIAuthPackageAlias        string
		NeedsSeparateAPIAuthImport bool
		APIGroupPackagePath        string
		APIGroupPackageAlias       string
		NeedsSeparateParentImport  bool
		ParentAPIGroupPackagePath  string
		ParentAPIGroupPackageAlias string
		CrossRefs                  []crossRefWatchData
	}{
		EntityName:                 metadata.EntityName,
		EntityNameLowerCamel:       metadata.EntityNameLowerCamel,
		ParentEntityName:           metadata.ParentEntityName,
		ParentRefFieldName:         metadata.ParentRefFieldName,
		APIAuthPackageAlias:        apiAuthPackageAlias,
		NeedsSeparateAPIAuthImport: needsSeparateAPIAuthImport,
		APIGroupPackagePath:        metadata.APIGroupPackagePath,
		APIGroupPackageAlias:       metadata.APIGroupPackageAlias,
		NeedsSeparateParentImport:  metadata.ParentAPIGroupPackagePath != metadata.APIGroupPackagePath,
		ParentAPIGroupPackagePath:  metadata.ParentAPIGroupPackagePath,
		ParentAPIGroupPackageAlias: metadata.ParentAPIGroupPackageAlias,
		CrossRefs:                  crossRefs,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (g *Generator) generateIndex(metadata reconcilerEntityMetadata, rc *config.ReconcilerConfig) (string, error) {
	tmpl := template.Must(template.New("index").Parse(indexTemplate))
	if !rc.GetIsRoot() {
		tmpl = template.Must(template.New("childIndex").Parse(childIndexTemplate))
	}

	crossRefs := g.buildCrossRefWatchData(metadata.EntityName)

	var buf strings.Builder
	data := struct {
		EntityName           string
		EntityNameLowerCamel string
		ParentEntityName     string
		ParentRefFieldName   string
		APIGroupPackagePath  string
		APIGroupPackageAlias string
		CrossRefs            []crossRefWatchData
	}{
		EntityName:           metadata.EntityName,
		EntityNameLowerCamel: metadata.EntityNameLowerCamel,
		ParentEntityName:     metadata.ParentEntityName,
		ParentRefFieldName:   metadata.ParentRefFieldName,
		APIGroupPackagePath:  metadata.APIGroupPackagePath,
		APIGroupPackageAlias: metadata.APIGroupPackageAlias,
		CrossRefs:            crossRefs,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// buildCrossRefWatchData builds crossRefWatchData entries for an entity's references.
func (g *Generator) buildCrossRefWatchData(entityName string) []crossRefWatchData {
	refs := g.templateReferences(entityName)
	if len(refs) == 0 {
		return nil
	}
	result := make([]crossRefWatchData, len(refs))
	for i, ref := range refs {
		// Convert path like "spec.apiSpec.destination" to Go field accessor "Spec.APISpec.Destination".
		// goFieldName handles "_" separators; fixInitialisms corrects initialisms like "Api" → "API".
		segments := strings.Split(ref.Path, ".")
		goSegments := make([]string, len(segments))
		for j, seg := range segments {
			goSegments[j] = fixInitialisms(goFieldName(seg))
		}
		result[i] = crossRefWatchData{
			RefKind:     ref.Kind,
			GoFieldPath: strings.Join(goSegments, "."),
		}
	}
	return result
}

func (g *Generator) reconcilerEntityMetadata(
	entityName string,
	schema *parser.Schema,
	rc *config.ReconcilerConfig,
) (reconcilerEntityMetadata, error) {
	metadata := reconcilerEntityMetadata{
		EntityName:                 entityName,
		EntityNameLowerCamel:       toLowerCamel(entityName),
		APIGroupPackagePath:        g.config.APIGroupPackagePath,
		APIGroupPackageAlias:       g.config.APIGroupPackageAlias,
		ParentAPIGroupPackagePath:  g.config.APIGroupPackagePath,
		ParentAPIGroupPackageAlias: g.config.APIGroupPackageAlias,
	}

	if rc.GetIsRoot() {
		return metadata, nil
	}
	if len(schema.Dependencies) == 0 {
		return reconcilerEntityMetadata{}, fmt.Errorf("non-root entity %s has no parent dependency", entityName)
	}

	parentDep := schema.Dependencies[len(schema.Dependencies)-1]
	metadata.ParentRefFieldName = parentDep.FieldName
	metadata.ParentEntityName = parentDep.EntityName
	if rc.ParentEntityKind() != "" {
		metadata.ParentEntityName = rc.ParentEntityKind()
	}
	parentGroup := rc.ParentEntityGroup(g.config.APIGroup)
	if parentGroup != g.config.APIGroup {
		metadata.ParentAPIGroupPackagePath, metadata.ParentAPIGroupPackageAlias = apiGroupPackagePathAndAlias(parentGroup, g.config.APIVersion)
	}
	if rc.ParentRef != nil {
		metadata.ParentRefFieldName = goFieldName(rc.ParentRef.FieldName)
	}

	return metadata, nil
}

func apiGroupPackagePathAndAlias(apiGroup, apiVersion string) (string, string) {
	groupPrefix := strings.Split(apiGroup, ".")[0]
	return fmt.Sprintf("github.com/kong/kong-operator/v2/api/%s/%s", groupPrefix, apiVersion),
		strings.ReplaceAll(groupPrefix, "-", "") + apiVersion
}

// toLowerCamel converts a PascalCase name to lowerCamelCase.
// e.g. "Portal" → "portal", "KonnectEventGateway" → "konnectEventControlPlane".
func toLowerCamel(s string) string {
	if s == "" {
		return s
	}
	// Find the boundary: lowercase the leading uppercase run.
	// For "KonnectEventGateway" → "konnectEventControlPlane"
	runes := []rune(s)
	i := 0
	for i < len(runes) && unicode.IsUpper(runes[i]) {
		i++
	}
	if i == 0 {
		return s
	}
	// If all chars are uppercase, lowercase all
	if i == len(runes) {
		return strings.ToLower(s)
	}
	// If more than one leading uppercase, lowercase all but the last
	// (the last starts the next word)
	if i > 1 {
		result := strings.ToLower(string(runes[:i-1])) + string(runes[i-1:])
		return result
	}
	// Single leading uppercase
	return strings.ToLower(string(runes[0])) + string(runes[1:])
}
