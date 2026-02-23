package generator

const crdTypeTemplate = `package {{.APIVersion}}

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
{{- if .NeedsJSONImport}}
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
{{- end}}
)

// {{.EntityName}} is the Schema for the {{.EntityName | lower}}s API.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="ID",description="Konnect ID",type="string",JSONPath=".status.id"
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=` + "`" + `.status.conditions[?(@.type=='Programmed')].status` + "`" + `
// +kubebuilder:printcolumn:name="OrgID",description="Konnect Organization ID this resource belongs to.",type=string,JSONPath=` + "`" + `.status.organizationID` + "`" + `
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:storageversion
// +apireference:kgo:include
// +kong:channels=kong-operator
type {{.EntityName}} struct {
	metav1.TypeMeta   ` + "`" + `json:",inline"` + "`" + `
	metav1.ObjectMeta ` + "`" + `json:"metadata,omitzero"` + "`" + `

	// +optional
	Spec {{.EntityName}}Spec ` + "`" + `json:"spec,omitzero"` + "`" + `

	// +optional
	Status {{.EntityName}}Status ` + "`" + `json:"status,omitzero"` + "`" + `
}

// {{.EntityName}}List contains a list of {{.EntityName}}.
//
// +kubebuilder:object:root=true
type {{.EntityName}}List struct {
	metav1.TypeMeta ` + "`" + `json:",inline"` + "`" + `
	metav1.ListMeta ` + "`" + `json:"metadata,omitzero"` + "`" + `
	Items           []{{.EntityName}} ` + "`" + `json:"items"` + "`" + `
}

// {{.EntityName}}Spec defines the desired state of {{.EntityName}}.
type {{.EntityName}}Spec struct {
{{- range .Schema.Dependencies}}
	// {{.FieldName}} is the reference to the parent {{.EntityName}} object.
	//
	// +required
	{{.FieldName}} ObjectRef ` + "`" + `json:"{{.JSONName}},omitzero"` + "`" + `
{{end}}
	// APISpec defines the desired state of the resource's API spec fields.
	//
	// +optional
	APISpec {{.EntityName}}APISpec ` + "`" + `json:"apiSpec,omitzero"` + "`" + `
}

// {{.EntityName}}APISpec defines the API spec fields for {{.EntityName}}.
type {{.EntityName}}APISpec struct {
{{- if hasRootOneOf .Schema}}
	// {{.EntityName}}Config embeds the union type configuration.
	//
	// +optional
	*{{.EntityName}}Config ` + "`" + `json:",inline"` + "`" + `
{{- else}}
{{- range $i, $prop := .Schema.Properties}}
{{- if not (skipProperty $prop)}}
{{formatComment $prop.Description}}
	//
{{- range kubebuilderTags $prop}}
	// {{.}}
{{- end}}
{{- if isRefProperty $prop}}
	{{goFieldName $prop.Name}}Ref {{goType $prop}} ` + "`" + `json:"{{$prop.Name}}_ref,omitempty"` + "`" + `
{{- else}}
	{{goFieldName $prop.Name}} {{goType $prop}} ` + "`" + `json:"{{jsonTag $prop}}"` + "`" + `
{{- end}}
{{end}}
{{- end}}
{{- end}}
}

// {{.EntityName}}Status defines the observed state of {{.EntityName}}.
type {{.EntityName}}Status struct {
	// Conditions represent the current state of the resource.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition ` + "`" + `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"` + "`" + `

	// Konnect contains the Konnect entity status.
	//
	// +optional
	KonnectEntityStatus ` + "`" + `json:",inline"` + "`" + `
{{range .Schema.Dependencies}}
	// {{.EntityName}}ID is the Konnect ID of the parent {{.EntityName}}.
	//
	// +optional
	{{.EntityName}}ID *KonnectEntityRef ` + "`" + `json:"{{.EntityName | lower}}ID,omitempty"` + "`" + `
{{end}}
	// ObservedGeneration is the most recent generation observed
	//
	// +optional
	ObservedGeneration int64 ` + "`" + `json:"observedGeneration,omitempty"` + "`" + `
}

func init() {
	SchemeBuilder.Register(&{{.EntityName}}{}, &{{.EntityName}}List{})
}
`

const sdkOpsTemplate = `package {{.APIVersion}}

import (
	"encoding/json"
	"fmt"
{{range .Imports}}
	{{.Alias}} "{{.Path}}"
{{- end}}
)
{{range .Methods}}
// {{.MethodName}} converts the {{$.EntityName}}APISpec to the SDK type
// {{.ImportAlias}}.{{.TypeName}} using JSON marshal/unmarshal.
// Fields that exist in the CRD spec but not in the SDK type (e.g., Kubernetes
// object references) are naturally excluded because they have different JSON names.
func (s *{{$.EntityName}}APISpec) {{.MethodName}}() (*{{.ImportAlias}}.{{.TypeName}}, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal {{$.EntityName}}APISpec: %w", err)
	}
	var target {{.ImportAlias}}.{{.TypeName}}
	if err := json.Unmarshal(data, &target); err != nil {
		return nil, fmt.Errorf("failed to unmarshal into {{.TypeName}}: %w", err)
	}
	return &target, nil
}
{{end}}`

const sdkOpsTestTemplate = `package {{.APIVersion}}

import (
	"testing"

	"github.com/stretchr/testify/require"
)
{{range .Methods}}
func Test{{$.EntityName}}APISpec_{{.MethodName}}(t *testing.T) {
	spec := &{{$.EntityName}}APISpec{
{{- range $.TestFields}}
		{{.FieldName}}: {{.TestValue}},
{{- end}}
	}
	result, err := spec.{{.MethodName}}()
	require.NoError(t, err)
	require.NotNil(t, result)
}
{{end}}`

const registerTemplate = `package {{.APIVersion}}

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "{{.APIGroup}}", Version: "{{.APIVersion}}"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme
	AddToScheme = SchemeBuilder.AddToScheme
)
`
