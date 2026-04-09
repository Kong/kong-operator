package generator

import (
	"fmt"
	"slices"
	"strings"
	"text/template"
	"unicode"
)

// reconcilerFuncsTemplate generates interface methods for each entity.
// These are needed by the generic KonnectEntityReconciler.
const reconcilerFuncsTemplate = sharedGeneratedFilePreamble + `

package {{.APIVersion}}

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)
{{range .Entities}}
// GetKonnectStatus returns the Konnect status contained in the {{.Name}} status.
func (obj *{{.Name}}) GetKonnectStatus() *konnectv1alpha2.KonnectEntityStatus {
	return &obj.Status.KonnectEntityStatus
}

// GetKonnectID returns the Konnect ID in the {{.Name}} status.
func (obj *{{.Name}}) GetKonnectID() string {
	return obj.Status.ID
}

// SetKonnectID sets the Konnect ID in the {{.Name}} status.
func (obj *{{.Name}}) SetKonnectID(id string) {
	obj.Status.ID = id
}

// GetTypeName returns the {{.Name}} Kind name.
func (obj {{.Name}}) GetTypeName() string {
	return "{{.Name}}"
}

// GetConditions returns the Status Conditions.
func (obj *{{.Name}}) GetConditions() []metav1.Condition {
	return obj.Status.Conditions
}

// SetConditions sets the Status Conditions.
func (obj *{{.Name}}) SetConditions(conditions []metav1.Condition) {
	obj.Status.Conditions = conditions
}

{{- if .IsRoot }}
// GetKonnectAPIAuthConfigurationRef returns the Konnect API Auth Configuration Ref.
func (obj *{{.Name}}) GetKonnectAPIAuthConfigurationRef() konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef {
	return konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
		Name: obj.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name,
	}
}
{{- end }}
{{end}}`

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

// generateReconcilerFiles generates all reconciler wiring files for the given entities.
func (g *Generator) generateReconcilerFiles(entityNames []string) ([]GeneratedFile, error) {
	var files []GeneratedFile

	// Generate zz_generated_reconciler_funcs.go (one file for all entities, in the API dir)
	funcsContent, err := g.generateReconcilerFuncs(entityNames)
	if err != nil {
		return nil, fmt.Errorf("failed to generate reconciler funcs: %w", err)
	}
	files = append(files, GeneratedFile{
		Name:    "zz_generated_reconciler_funcs.go",
		Content: funcsContent,
	})

	// Generate per-entity watch and index files
	for _, entityName := range entityNames {
		rc := g.config.ReconcilerConfig[entityName]
		if !rc.IsRoot {
			// Non-root entities have different watch/index patterns (Phase 2)
			continue
		}

		// Watch file → controller/konnect/
		watchContent, err := g.generateWatch(entityName)
		if err != nil {
			return nil, fmt.Errorf("failed to generate watch for %s: %w", entityName, err)
		}
		files = append(files, GeneratedFile{
			Name:        "zz_generated_watch_" + strings.ToLower(entityName) + ".go",
			Content:     watchContent,
			RelativeDir: "controller/konnect",
		})

		// Index file → internal/utils/index/
		indexContent, err := g.generateIndex(entityName)
		if err != nil {
			return nil, fmt.Errorf("failed to generate index for %s: %w", entityName, err)
		}
		files = append(files, GeneratedFile{
			Name:        "zz_generated_" + strings.ToLower(entityName) + ".go",
			Content:     indexContent,
			RelativeDir: "internal/utils/index",
		})
	}

	return files, nil
}

func (g *Generator) generateReconcilerFuncs(entityNames []string) (string, error) {
	tmpl := template.Must(template.New("reconcilerFuncs").Parse(reconcilerFuncsTemplate))

	var buf strings.Builder
	type reconcilerEntity struct {
		Name   string
		IsRoot bool
	}

	entities := make([]reconcilerEntity, 0, len(entityNames))
	for _, entityName := range entityNames {
		entities = append(entities, reconcilerEntity{
			Name:   entityName,
			IsRoot: g.config.ReconcilerConfig[entityName] != nil && g.config.ReconcilerConfig[entityName].IsRoot,
		})
	}
	slices.SortFunc(entities, func(a, b reconcilerEntity) int {
		return strings.Compare(a.Name, b.Name)
	})

	data := struct {
		APIVersion string
		Entities   []reconcilerEntity
	}{
		APIVersion: g.config.APIVersion,
		Entities:   entities,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (g *Generator) generateWatch(entityName string) (string, error) {
	tmpl := template.Must(template.New("watch").Parse(watchTemplate))

	const konnectAPIAuthPackagePath = "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"

	apiAuthPackageAlias := g.config.APIGroupPackageAlias
	needsSeparateAPIAuthImport := g.config.APIGroupPackagePath != konnectAPIAuthPackagePath
	if needsSeparateAPIAuthImport {
		apiAuthPackageAlias = "konnectapiauthv1alpha1"
	}

	var buf strings.Builder
	data := struct {
		EntityName                 string
		APIAuthPackageAlias        string
		NeedsSeparateAPIAuthImport bool
		APIGroupPackagePath        string
		APIGroupPackageAlias       string
	}{
		EntityName:                 entityName,
		APIAuthPackageAlias:        apiAuthPackageAlias,
		NeedsSeparateAPIAuthImport: needsSeparateAPIAuthImport,
		APIGroupPackagePath:        g.config.APIGroupPackagePath,
		APIGroupPackageAlias:       g.config.APIGroupPackageAlias,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (g *Generator) generateIndex(entityName string) (string, error) {
	tmpl := template.Must(template.New("index").Parse(indexTemplate))

	var buf strings.Builder
	data := struct {
		EntityName           string
		EntityNameLowerCamel string
		APIGroupPackagePath  string
		APIGroupPackageAlias string
	}{
		EntityName:           entityName,
		EntityNameLowerCamel: toLowerCamel(entityName),
		APIGroupPackagePath:  g.config.APIGroupPackagePath,
		APIGroupPackageAlias: g.config.APIGroupPackageAlias,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// toLowerCamel converts a PascalCase name to lowerCamelCase.
// e.g. "Portal" → "portal", "KonnectEventControlPlane" → "konnectEventControlPlane".
func toLowerCamel(s string) string {
	if s == "" {
		return s
	}
	// Find the boundary: lowercase the leading uppercase run.
	// For "KonnectEventControlPlane" → "konnectEventControlPlane"
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
