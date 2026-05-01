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
		if err := cl.List(ctx, &l,
			// TODO: change this when cross namespace refs are allowed.
			client.InNamespace(auth.GetNamespace()),
			client.MatchingFields{
				index.IndexField{{.EntityName}}OnAPIAuthConfiguration: auth.Namespace + "/" + auth.Name,
			},
		); err != nil {
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
				&{{.APIGroupPackageAlias}}.{{.ParentEntityName}}{},
				handler.EnqueueRequestsFromMapFunc(
					enqueue{{.EntityName}}For{{.ParentEntityName}}(cl),
				),
			)
		},
	}
}

func enqueue{{.EntityName}}For{{.ParentEntityName}}(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		parent, ok := obj.(*{{.APIGroupPackageAlias}}.{{.ParentEntityName}})
		if !ok {
			return nil
		}
		var l {{.APIGroupPackageAlias}}.{{.EntityName}}List
		if err := cl.List(ctx, &l,
			// TODO: change this when cross namespace refs are allowed.
			client.InNamespace(parent.GetNamespace()),
			client.MatchingFields{
				index.IndexField{{.EntityName}}On{{.ParentEntityName}}Ref: parent.Name,
			},
		); err != nil {
			return nil
		}
		return objectListToReconcileRequests(l.Items)
	}
}
`

const childIndexTemplate = sharedGeneratedFilePreamble + `

package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	{{.APIGroupPackageAlias}} "{{.APIGroupPackagePath}}"
)

const (
	// IndexField{{.EntityName}}On{{.ParentEntityName}}Ref is the index field for {{.EntityName}} -> {{.ParentEntityName}}.
	IndexField{{.EntityName}}On{{.ParentEntityName}}Ref = "{{.EntityNameLowerCamel}}On{{.ParentEntityName}}Ref"
)

// OptionsFor{{.EntityName}} returns required Index options for {{.EntityName}} reconciler.
func OptionsFor{{.EntityName}}() []Option {
	return []Option{
		{
			Object:         &{{.APIGroupPackageAlias}}.{{.EntityName}}{},
			Field:          IndexField{{.EntityName}}On{{.ParentEntityName}}Ref,
			ExtractValueFn: {{.EntityNameLowerCamel}}On{{.ParentEntityName}}Ref,
		},
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

	return []string{ent.Spec.{{.ParentRefFieldName}}.NamespacedRef.Name}
}
`

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

type reconcilerEntityMetadata struct {
	EntityName           string
	EntityNameLowerCamel string
	ParentEntityName     string
	ParentRefFieldName   string
	APIGroupPackagePath  string
	APIGroupPackageAlias string
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
			IsRoot:         rc.IsRoot,
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
		if !ok || rc.IsRoot {
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
	if !rc.IsRoot {
		tmpl = template.Must(template.New("childWatch").Parse(childWatchTemplate))
	}

	const konnectAPIAuthPackagePath = "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"

	apiAuthPackageAlias := g.config.APIGroupPackageAlias
	needsSeparateAPIAuthImport := g.config.APIGroupPackagePath != konnectAPIAuthPackagePath
	if needsSeparateAPIAuthImport {
		apiAuthPackageAlias = "konnectapiauthv1alpha1"
	}

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
	}{
		EntityName:                 metadata.EntityName,
		EntityNameLowerCamel:       metadata.EntityNameLowerCamel,
		ParentEntityName:           metadata.ParentEntityName,
		ParentRefFieldName:         metadata.ParentRefFieldName,
		APIAuthPackageAlias:        apiAuthPackageAlias,
		NeedsSeparateAPIAuthImport: needsSeparateAPIAuthImport,
		APIGroupPackagePath:        metadata.APIGroupPackagePath,
		APIGroupPackageAlias:       metadata.APIGroupPackageAlias,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (g *Generator) generateIndex(metadata reconcilerEntityMetadata, rc *config.ReconcilerConfig) (string, error) {
	tmpl := template.Must(template.New("index").Parse(indexTemplate))
	if !rc.IsRoot {
		tmpl = template.Must(template.New("childIndex").Parse(childIndexTemplate))
	}

	var buf strings.Builder
	data := struct {
		EntityName           string
		EntityNameLowerCamel string
		ParentEntityName     string
		ParentRefFieldName   string
		APIGroupPackagePath  string
		APIGroupPackageAlias string
	}{
		EntityName:           metadata.EntityName,
		EntityNameLowerCamel: metadata.EntityNameLowerCamel,
		ParentEntityName:     metadata.ParentEntityName,
		ParentRefFieldName:   metadata.ParentRefFieldName,
		APIGroupPackagePath:  metadata.APIGroupPackagePath,
		APIGroupPackageAlias: metadata.APIGroupPackageAlias,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (g *Generator) reconcilerEntityMetadata(
	entityName string,
	schema *parser.Schema,
	rc *config.ReconcilerConfig,
) (reconcilerEntityMetadata, error) {
	metadata := reconcilerEntityMetadata{
		EntityName:           entityName,
		EntityNameLowerCamel: toLowerCamel(entityName),
		APIGroupPackagePath:  g.config.APIGroupPackagePath,
		APIGroupPackageAlias: g.config.APIGroupPackageAlias,
	}

	if rc.IsRoot {
		return metadata, nil
	}
	if len(schema.Dependencies) == 0 {
		return reconcilerEntityMetadata{}, fmt.Errorf("non-root entity %s has no parent dependency", entityName)
	}

	parentDep := schema.Dependencies[len(schema.Dependencies)-1]
	metadata.ParentRefFieldName = parentDep.FieldName
	metadata.ParentEntityName = parentDep.EntityName
	if rc.ParentEntityType != "" {
		metadata.ParentEntityName = rc.ParentEntityType
	}

	return metadata, nil
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
