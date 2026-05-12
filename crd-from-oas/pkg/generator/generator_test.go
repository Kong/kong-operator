package generator

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

func ptr[T any](v T) *T { return &v }

func TestObjectRefTypeName(t *testing.T) {
	tests := []struct {
		name        string
		commonTypes *config.CommonTypesConfig
		want        string
	}{
		{
			name:        "nil commonTypes returns ObjectRef",
			commonTypes: nil,
			want:        "ObjectRef",
		},
		{
			name: "generate true returns ObjectRef",
			commonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Generate: new(true),
				},
			},
			want: "ObjectRef",
		},
		{
			name: "import with alias returns qualified name",
			commonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{
						Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
						Alias: "commonv1alpha1",
					},
				},
			},
			want: "commonv1alpha1.ObjectRef",
		},
		{
			name: "import without alias uses last path segment",
			commonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{
						Path: "github.com/kong/kong-operator/v2/api/common/v1alpha1",
					},
				},
			},
			want: "v1alpha1.ObjectRef",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGenerator(Config{
				CommonTypes: tc.commonTypes,
			})
			assert.Equal(t, tc.want, g.objectRefTypeName())
		})
	}
}

func TestGoType_ObjectRef(t *testing.T) {
	t.Run("without import uses ObjectRef", func(t *testing.T) {
		g := NewGenerator(Config{})
		prop := &parser.Property{IsReference: true}
		assert.Equal(t, "*ObjectRef", g.goType(prop))
	})

	t.Run("with import uses qualified ObjectRef", func(t *testing.T) {
		g := NewGenerator(Config{
			CommonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{
						Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
						Alias: "commonv1alpha1",
					},
				},
			},
		})
		prop := &parser.Property{IsReference: true}
		assert.Equal(t, "*commonv1alpha1.ObjectRef", g.goType(prop))
	})
}

func TestGenerateWatch_UsesStableAPIAuthImportAndNamespacedLookup(t *testing.T) {
	t.Run("reuses generated package import for konnect v1alpha1 entities", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
			APIGroupPackageAlias: "konnectv1alpha1",
		})

		content, err := g.generateWatch(reconcilerEntityMetadata{
			EntityName:           "Portal",
			EntityNameLowerCamel: "portal",
			APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
			APIGroupPackageAlias: "konnectv1alpha1",
		}, &config.ReconcilerConfig{IsRoot: ptr(true)})
		require.NoError(t, err)

		assert.Contains(t, content, `konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"`)
		assert.NotContains(t, content, `konnectapiauthv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"`)
		assert.Contains(t, content, `&konnectv1alpha1.KonnectAPIAuthConfiguration{}`)
	})

	t.Run("uses separate auth import and namespaced lookup for other api groups", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1",
			APIGroupPackageAlias: "xkonnectv1alpha1",
		})

		content, err := g.generateWatch(reconcilerEntityMetadata{
			EntityName:           "Portal",
			EntityNameLowerCamel: "portal",
			APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1",
			APIGroupPackageAlias: "xkonnectv1alpha1",
		}, &config.ReconcilerConfig{IsRoot: ptr(true)})
		require.NoError(t, err)

		assert.Contains(t, content, `konnectapiauthv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"`)
		assert.Contains(t, content, `&konnectapiauthv1alpha1.KonnectAPIAuthConfiguration{}`)
		assert.Contains(t, content, `index.IndexFieldPortalOnAPIAuthConfiguration: auth.Namespace + "/" + auth.Name,`)
	})
}

func TestGenerateIndex_UsesNamespacedAPIAuthKey(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1",
		APIGroupPackageAlias: "xkonnectv1alpha1",
	})

	content, err := g.generateIndex(reconcilerEntityMetadata{
		EntityName:           "Portal",
		EntityNameLowerCamel: "portal",
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1",
		APIGroupPackageAlias: "xkonnectv1alpha1",
	}, &config.ReconcilerConfig{IsRoot: ptr(true)})
	require.NoError(t, err)

	assert.Contains(t, content, `if ent.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name == "" {`)
	assert.Contains(t, content, `return []string{ent.GetNamespace() + "/" + ent.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name}`)
}

func TestGenerateWatchAndIndex_ForChildEntity(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
	})

	metadata := reconcilerEntityMetadata{
		EntityName:           "KonnectEventDataPlaneCertificate",
		EntityNameLowerCamel: "konnectEventDataPlaneCertificate",
		ParentEntityName:     "KonnectEventGateway",
		ParentRefFieldName:   "GatewayRef",
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
	}

	t.Run("watches parent entity", func(t *testing.T) {
		content, err := g.generateWatch(metadata, &config.ReconcilerConfig{
			IsRoot:           ptr(false),
			ParentEntityType: "KonnectEventGateway",
		})
		require.NoError(t, err)

		assert.Contains(t, content, `&konnectv1alpha1.KonnectEventGateway{}`)
		assert.Contains(t, content, `enqueueKonnectEventDataPlaneCertificateForKonnectEventGateway(cl)`)
		assert.Contains(t, content, `index.IndexFieldKonnectEventDataPlaneCertificateOnKonnectEventGatewayRef: parent.Name,`)
	})

	t.Run("indexes by dependency namespaced ref", func(t *testing.T) {
		content, err := g.generateIndex(metadata, &config.ReconcilerConfig{
			IsRoot:           ptr(false),
			ParentEntityType: "KonnectEventGateway",
		})
		require.NoError(t, err)

		assert.Contains(t, content, `IndexFieldKonnectEventDataPlaneCertificateOnKonnectEventGatewayRef`)
		assert.Contains(t, content, `if ent.Spec.GatewayRef.NamespacedRef == nil {`)
		assert.Contains(t, content, `return []string{ent.Spec.GatewayRef.NamespacedRef.Name}`)
	})
}

func TestGenerateReconcilerConditions(t *testing.T) {
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"Portal":                           {IsRoot: new(true)},
			"PortalTeam":                       {IsRoot: new(false)},
			"KonnectEventGateway":              {IsRoot: new(true)},
			"KonnectEventDataPlaneCertificate": {IsRoot: new(false), ParentEntityType: "KonnectEventGateway"},
			"EventGatewayListenerPolicy":       {IsRoot: new(false), ParentEntityType: "EventGatewayListener"},
		},
	})

	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"CreatePortal": {
				Name: "CreatePortal",
			},
			"CreatePortalTeam": {
				Name: "CreatePortalTeam",
				Dependencies: []*parser.Dependency{{
					EntityName:         "Portal",
					AccessorEntityName: "Portal",
					FieldName:          "PortalRef",
					JSONName:           "portal_ref",
				}},
			},
			"CreateKonnectEventGateway": {
				Name: "CreateKonnectEventGateway",
			},
			"CreateKonnectEventDataPlaneCertificate": {
				Name: "CreateKonnectEventDataPlaneCertificate",
				Dependencies: []*parser.Dependency{{
					EntityName:         "Gateway",
					AccessorEntityName: "EventGateway",
					FieldName:          "GatewayRef",
					JSONName:           "gateway_ref",
				}},
			},
			"CreateEventGatewayListenerPolicy": {
				Name: "CreateEventGatewayListenerPolicy",
				Dependencies: []*parser.Dependency{{
					EntityName:         "EventGatewayListener",
					AccessorEntityName: "Listener",
					FieldName:          "EventGatewayListenerRef",
					JSONName:           "event_gateway_listener_ref",
				}},
			},
		},
	}

	file, err := g.generateReconcilerConditions(parsed)
	require.NoError(t, err)
	require.NotNil(t, file)
	assert.Equal(t, "zz_generated_reconciler_conditions.go", file.Name)
	assert.Contains(t, file.Content, `package v1alpha1`)
	assert.Contains(t, file.Content, `EventGatewayRefValidConditionType = "EventGatewayRefValid"`)
	assert.Contains(t, file.Content, `EventGatewayRefReasonNotProgrammed = "NotProgrammed"`)
	assert.Contains(t, file.Content, `EventGatewayListenerRefValidConditionType = "EventGatewayListenerRefValid"`)
	assert.Contains(t, file.Content, `PortalRefValidConditionType = "PortalRefValid"`)
	assert.NotContains(t, file.Content, `KonnectEventGatewayRefValidConditionType`)
	assert.NotContains(t, file.Content, "\n\tListenerRefValidConditionType = \"ListenerRefValid\"")

	_, err = format.Source([]byte(file.Content))
	require.NoError(t, err, "generated file must be valid gofmt'd Go source")
}

func TestGenerate_EmitsReconcilerConditionsFile(t *testing.T) {
	g := NewGenerator(Config{
		APIGroup:   "konnect.konghq.com",
		APIVersion: "v1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"Portal":     {IsRoot: new(true)},
			"PortalTeam": {IsRoot: new(false)},
		},
	})

	files, err := g.Generate(&parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"CreatePortal": {
				Name: "CreatePortal",
			},
			"CreatePortalTeam": {
				Name: "CreatePortalTeam",
				Dependencies: []*parser.Dependency{{
					EntityName:         "Portal",
					AccessorEntityName: "Portal",
					FieldName:          "PortalRef",
					JSONName:           "portal_ref",
				}},
			},
		},
		Schemas: map[string]*parser.Schema{},
	})
	require.NoError(t, err)

	var fileNames []string
	for _, file := range files {
		fileNames = append(fileNames, file.Name)
	}

	assert.Contains(t, fileNames, "zz_generated_reconciler_conditions.go")
}

func TestGenerateCommonTypes(t *testing.T) {
	t.Run("without import includes union ObjectRef types", func(t *testing.T) {
		g := NewGenerator(Config{APIVersion: "v1alpha1"})
		content, err := g.generateCommonTypes()
		require.NoError(t, err)
		assert.Contains(t, content, "type ObjectRefType string")
		assert.Contains(t, content, "type ObjectRef struct")
		assert.Contains(t, content, "Type ObjectRefType")
		assert.NotContains(t, content, "KonnectID")
		assert.Contains(t, content, "NamespacedRef *NamespacedRef")
		assert.Contains(t, content, "type NamespacedRef struct")
		assert.NotContains(t, content, "Namespace *string")
		assert.Contains(t, content, `konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"`)
		assert.Contains(t, content, "type KonnectEntityStatus = konnectv1alpha2.KonnectEntityStatus")
		assert.Contains(t, content, "Code generated by CRD generation pipeline. DO NOT EDIT.")
	})

	t.Run("without import with namespaced includes Namespace field", func(t *testing.T) {
		g := NewGenerator(Config{
			APIVersion: "v1alpha1",
			CommonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Namespaced: true,
				},
			},
		})
		content, err := g.generateCommonTypes()
		require.NoError(t, err)
		assert.Contains(t, content, "type NamespacedRef struct")
		assert.Contains(t, content, "Namespace *string")
	})

	t.Run("with import excludes ObjectRef types", func(t *testing.T) {
		g := NewGenerator(Config{
			APIVersion: "v1alpha1",
			CommonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{
						Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
						Alias: "commonv1alpha1",
					},
				},
			},
		})
		content, err := g.generateCommonTypes()
		require.NoError(t, err)
		assert.NotContains(t, content, "type ObjectRef struct")
		assert.NotContains(t, content, "type ObjectRefType string")
		assert.NotContains(t, content, "type NamespacedRef struct")
		// Other common types should still be present
		assert.Contains(t, content, "type SecretKeyRef struct")
		assert.Contains(t, content, "type KonnectEntityStatus = konnectv1alpha2.KonnectEntityStatus")
		assert.Contains(t, content, "type KonnectEntityRef struct")
		assert.Contains(t, content, "Code generated by CRD generation pipeline. DO NOT EDIT.")
	})

}

func TestGenerateCRDType_ObjectRefImport(t *testing.T) {
	schema := &parser.Schema{
		Name: "CreatePortal",
		Dependencies: []*parser.Dependency{
			{
				ParamName:  "portalId",
				EntityName: "Portal",
				FieldName:  "PortalRef",
				JSONName:   "portal_ref",
			},
		},
	}

	t.Run("without import uses unqualified ObjectRef", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "konnect.konghq.com",
			APIVersion: "v1alpha1",
		})
		content, err := g.generateCRDType("CreatePortal", schema)
		require.NoError(t, err)
		assert.Contains(t, content, "PortalRef ObjectRef")
		assert.NotContains(t, content, "commonv1alpha1")
	})

	t.Run("with import uses qualified ObjectRef and adds import", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "konnect.konghq.com",
			APIVersion: "v1alpha1",
			CommonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{
						Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
						Alias: "commonv1alpha1",
					},
				},
			},
		})
		content, err := g.generateCRDType("CreatePortal", schema)
		require.NoError(t, err)
		assert.Contains(t, content, "PortalRef commonv1alpha1.ObjectRef")
		assert.Contains(t, content, `commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"`)
	})

	t.Run("with import qualifies ref property types", func(t *testing.T) {
		schemaWithRef := &parser.Schema{
			Name: "CreateTeam",
			Properties: []*parser.Property{
				{
					Name:        "portal_id",
					Type:        "string",
					Format:      "uuid",
					IsReference: true,
				},
			},
		}
		g := NewGenerator(Config{
			APIGroup:   "konnect.konghq.com",
			APIVersion: "v1alpha1",
			CommonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{
						Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
						Alias: "commonv1alpha1",
					},
				},
			},
		})
		content, err := g.generateCRDType("CreateTeam", schemaWithRef)
		require.NoError(t, err)
		assert.Contains(t, content, "*commonv1alpha1.ObjectRef")
	})
}

func TestGenerateCRDType_NoObjectRefImportWhenUnneeded(t *testing.T) {
	schema := &parser.Schema{
		Name: "CreatePortal",
		Properties: []*parser.Property{
			{
				Name: "name",
				Type: "string",
			},
		},
	}

	g := NewGenerator(Config{
		APIGroup:   "konnect.konghq.com",
		APIVersion: "v1alpha1",
		CommonTypes: &config.CommonTypesConfig{
			ObjectRef: &config.ObjectRefConfig{
				Import: &config.ImportConfig{
					Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
					Alias: "commonv1alpha1",
				},
			},
		},
	})
	content, err := g.generateCRDType("CreatePortal", schema)
	require.NoError(t, err)
	// When there are no dependencies or ref properties, the import should
	// not be included to avoid unused import errors.
	assert.NotContains(t, content, "commonv1alpha1")
}

func TestGenerateCRDType_DoesNotGenerateHelperMethods(t *testing.T) {
	schema := &parser.Schema{
		Name: "CreatePortal",
		Properties: []*parser.Property{
			{
				Name: "labels",
				Type: "object",
				AdditionalProperties: &parser.Property{
					Name: "value",
					Type: "string",
				},
			},
		},
	}

	g := NewGenerator(Config{
		APIGroup:   "x-konnect.konghq.com",
		APIVersion: "v1alpha1",
	})

	content, err := g.generateCRDType("CreatePortal", schema)
	require.NoError(t, err)
	assert.NotContains(t, content, "GetKonnectLabels")
	assert.NotContains(t, content, "SetKonnectLabels")
	assert.NotContains(t, content, "GetKonnectStatus")
	assert.NotContains(t, content, "SetKonnectID")
	assert.NotContains(t, content, "konnectv1alpha2")
}

func TestGenerateCRDFuncs_GeneratesKonnectLabelAccessors(t *testing.T) {
	t.Run("referenced labels map type", func(t *testing.T) {
		schema := &parser.Schema{
			Name: "CreatePortal",
			Properties: []*parser.Property{
				{
					Name:    "labels",
					Type:    "object",
					RefName: "LabelsUpdate",
					AdditionalProperties: &parser.Property{
						Name: "value",
						Type: "string",
					},
				},
			},
		}

		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
		})

		content, err := g.generateCRDFuncs("CreatePortal", schema)
		require.NoError(t, err)
		assert.Contains(t, content, "func (obj *Portal) GetKonnectLabels() map[string]string {")
		assert.Contains(t, content, "if obj.Spec.APISpec.Labels == nil {")
		assert.Contains(t, content, "labels[key] = string(value)")
		assert.Contains(t, content, "converted := make(LabelsUpdate, len(labels))")
		assert.Contains(t, content, "converted[key] = LabelsUpdateValue(value)")
		assert.Contains(t, content, "obj.Spec.APISpec.Labels = converted")
	})

	t.Run("inline labels map type", func(t *testing.T) {
		schema := &parser.Schema{
			Name: "CreatePortal",
			Properties: []*parser.Property{
				{
					Name: "labels",
					Type: "object",
					AdditionalProperties: &parser.Property{
						Name: "value",
						Type: "string",
					},
				},
			},
		}

		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
		})

		content, err := g.generateCRDFuncs("CreatePortal", schema)
		require.NoError(t, err)
		assert.Contains(t, content, "converted := make(map[string]string, len(labels))")
		assert.Contains(t, content, "converted[key] = string(value)")
	})

	t.Run("without labels field", func(t *testing.T) {
		schema := &parser.Schema{
			Name: "CreatePortal",
			Properties: []*parser.Property{
				{
					Name: "name",
					Type: "string",
				},
			},
		}

		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
		})

		content, err := g.generateCRDFuncs("CreatePortal", schema)
		require.NoError(t, err)
		assert.NotContains(t, content, "GetKonnectLabels")
		assert.NotContains(t, content, "SetKonnectLabels")
	})
}

func TestGenerateCRDFuncs_GeneratesKonnectFuncs(t *testing.T) {
	schema := &parser.Schema{
		Name: "CreatePortal",
		Properties: []*parser.Property{{
			Name: "name",
			Type: "string",
		}},
	}

	t.Run("default Konnect status return type", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
		})

		content, err := g.generateCRDFuncs("CreatePortal", schema)
		require.NoError(t, err)
		assert.Contains(t, content, `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"`)
		assert.Contains(t, content, `konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"`)
		assert.Contains(t, content, "func (obj *Portal) GetKonnectStatus() *konnectv1alpha2.KonnectEntityStatus {")
		assert.Contains(t, content, "return &obj.Status.KonnectEntityStatus")
		assert.Contains(t, content, "func (obj *Portal) SetKonnectID(id string) {")
		assert.Contains(t, content, "obj.Status.ID = id")
		assert.Contains(t, content, `func (obj *Portal) GetKonnectID() string {`)
		assert.Contains(t, content, `func (obj Portal) GetTypeName() string {`)
		assert.Contains(t, content, `func (obj *Portal) GetConditions() []metav1.Condition {`)
		assert.Contains(t, content, `func (obj *Portal) SetConditions(conditions []metav1.Condition) {`)
		assert.NotContains(t, content, `SetParentID(id string)`)
	})

	t.Run("reconciler entities include lifecycle helpers in the same file", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
			ReconcilerConfig: map[string]*config.ReconcilerConfig{
				"Portal": {
					IsRoot: ptr(true),
				},
			},
		})

		content, err := g.generateCRDFuncs("CreatePortal", schema)
		require.NoError(t, err)
		assert.Contains(t, content, `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"`)
		assert.Contains(t, content, `func (obj *Portal) GetKonnectID() string {`)
		assert.Contains(t, content, `func (obj Portal) GetTypeName() string {`)
		assert.Contains(t, content, `func (obj *Portal) GetConditions() []metav1.Condition {`)
		assert.Contains(t, content, `func (obj *Portal) SetConditions(conditions []metav1.Condition) {`)
		assert.Contains(t, content, `func (obj *Portal) GetKonnectAPIAuthConfigurationRef() konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef {`)
	})

	t.Run("non-root reconciler entities omit auth accessors", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
			ReconcilerConfig: map[string]*config.ReconcilerConfig{
				"PortalTeam": {},
			},
		})

		content, err := g.generateCRDFuncs("CreatePortalTeam", schema)
		require.NoError(t, err)
		assert.Contains(t, content, `func (obj *PortalTeam) GetKonnectID() string {`)
		assert.Contains(t, content, `func (obj PortalTeam) GetTypeName() string {`)
		assert.Contains(t, content, `func (obj *PortalTeam) GetConditions() []metav1.Condition {`)
		assert.Contains(t, content, `func (obj *PortalTeam) SetConditions(conditions []metav1.Condition) {`)
		assert.NotContains(t, content, `GetKonnectAPIAuthConfigurationRef`)
	})

	t.Run("dependency-backed child entities get parent ID accessors", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
			ReconcilerConfig: map[string]*config.ReconcilerConfig{
				"PortalTeam": {},
			},
		})

		schemaWithDependency := &parser.Schema{
			Name: "CreatePortalTeam",
			Dependencies: []*parser.Dependency{{
				EntityName: "Portal",
				FieldName:  "PortalRef",
				JSONName:   "portalRef",
			}},
		}

		content, err := g.generateCRDFuncs("CreatePortalTeam", schemaWithDependency)
		require.NoError(t, err)
		assert.Contains(t, content, `func (obj *PortalTeam) GetPortalID() string {`)
		assert.Contains(t, content, `func (obj *PortalTeam) SetPortalID(id string) {`)
		assert.Contains(t, content, `func (obj *PortalTeam) SetParentID(id string) {`)
		assert.Contains(t, content, `obj.SetPortalID(id)`)
	})

	t.Run("dependency-backed child entities get root ref accessor", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "konnect.konghq.com",
			APIVersion: "v1alpha1",
			CommonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{
						Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
						Alias: "commonv1alpha1",
					},
				},
			},
		})

		schemaWithDependency := &parser.Schema{
			Name: "CreatePortalTeam",
			Dependencies: []*parser.Dependency{{
				EntityName: "Portal",
				FieldName:  "PortalRef",
				JSONName:   "portal_ref",
			}},
		}

		content, err := g.generateCRDFuncs("CreatePortalTeam", schemaWithDependency)
		require.NoError(t, err)
		assert.Contains(t, content, `commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"`)
		assert.Contains(t, content, `func (obj *PortalTeam) GetPortalRef() commonv1alpha1.ObjectRef {`)
		assert.Contains(t, content, `return obj.Spec.PortalRef`)
		assert.Contains(t, content, `func (obj *PortalTeam) GetParentRef() commonv1alpha1.ObjectRef {`)
		assert.Contains(t, content, `return obj.GetPortalRef()`)
		assert.Contains(t, content, `func (obj *PortalTeam) SetParentID(id string) {`)
		assert.Contains(t, content, `obj.SetPortalID(id)`)
		assert.Contains(t, content, `func (obj *PortalTeam) GetStatusConditionTypeParentRefValid() string {`)
		assert.Contains(t, content, `return PortalRefValidConditionType`)
		assert.Contains(t, content, `func (obj *PortalTeam) GetStatusConditionReasonParentRefValid() string {`)
		assert.Contains(t, content, `return PortalRefReasonValid`)
		assert.Contains(t, content, `func (obj *PortalTeam) GetStatusConditionReasonParentRefInvalid() string {`)
		assert.Contains(t, content, `return PortalRefReasonInvalid`)
		assert.Contains(t, content, `func (obj *PortalTeam) GetStatusConditionReasonParentRefNotProgrammed() string {`)
		assert.Contains(t, content, `return PortalRefReasonNotProgrammed`)
	})

	t.Run("event gateway child entities get event gateway ref accessor alias", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "konnect.konghq.com",
			APIVersion: "v1alpha1",
			CommonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{
						Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
						Alias: "commonv1alpha1",
					},
				},
			},
		})

		schemaWithDependency := &parser.Schema{
			Name: "CreateEventGatewayDataPlaneCertificate",
			Dependencies: []*parser.Dependency{{
				EntityName:         "Gateway",
				AccessorEntityName: "EventGateway",
				FieldName:          "GatewayRef",
				JSONName:           "gateway_ref",
			}},
		}

		content, err := g.generateCRDFuncs("CreateEventGatewayDataPlaneCertificate", schemaWithDependency)
		require.NoError(t, err)
		assert.Contains(t, content, `func (obj *EventGatewayDataPlaneCertificate) GetGatewayRef() commonv1alpha1.ObjectRef {`)
		assert.Contains(t, content, `func (obj *EventGatewayDataPlaneCertificate) GetEventGatewayRef() commonv1alpha1.ObjectRef {`)
		assert.Contains(t, content, `return obj.Spec.GatewayRef`)
		assert.Contains(t, content, `func (obj *EventGatewayDataPlaneCertificate) GetParentRef() commonv1alpha1.ObjectRef {`)
		assert.Contains(t, content, `return obj.GetEventGatewayRef()`)
		assert.Contains(t, content, `func (obj *EventGatewayDataPlaneCertificate) SetParentID(id string) {`)
		assert.Contains(t, content, `obj.SetGatewayID(id)`)
		assert.Contains(t, content, `func (obj *EventGatewayDataPlaneCertificate) GetStatusConditionTypeParentRefValid() string {`)
		assert.Contains(t, content, `return EventGatewayRefValidConditionType`)
		assert.Contains(t, content, `func (obj *EventGatewayDataPlaneCertificate) GetStatusConditionReasonParentRefValid() string {`)
		assert.Contains(t, content, `return EventGatewayRefReasonValid`)
		assert.Contains(t, content, `func (obj *EventGatewayDataPlaneCertificate) GetStatusConditionReasonParentRefInvalid() string {`)
		assert.Contains(t, content, `return EventGatewayRefReasonInvalid`)
		assert.Contains(t, content, `func (obj *EventGatewayDataPlaneCertificate) GetStatusConditionReasonParentRefNotProgrammed() string {`)
		assert.Contains(t, content, `return EventGatewayRefReasonNotProgrammed`)
	})

	t.Run("root ref accessor uses last (immediate) dependency", func(t *testing.T) {
		// For multi-parent entities, only the immediate (last) parent is exposed
		// in Spec as a Ref field. Transitive parents (earlier in URL order) have
		// their IDs cached in Status and are NOT exposed as Ref accessors.
		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
		})

		schemaWithDependencies := &parser.Schema{
			Name: "CreatePortalTeamDeveloper",
			Dependencies: []*parser.Dependency{
				{
					EntityName: "Portal",
					FieldName:  "PortalRef",
					JSONName:   "portal_ref",
				},
				{
					EntityName: "Team",
					FieldName:  "TeamRef",
					JSONName:   "team_ref",
				},
			},
		}

		content, err := g.generateCRDFuncs("CreatePortalTeamDeveloper", schemaWithDependencies)
		require.NoError(t, err)
		// Team is the immediate (last) parent → its Ref accessor is generated.
		assert.Contains(t, content, `func (obj *PortalTeamDeveloper) GetTeamRef() ObjectRef {`)
		// Portal is a transitive parent → no GetPortalRef accessor.
		assert.NotContains(t, content, `func (obj *PortalTeamDeveloper) GetPortalRef() ObjectRef {`)
		assert.Contains(t, content, `func (obj *PortalTeamDeveloper) GetParentRef() ObjectRef {`)
		assert.Contains(t, content, `return obj.GetTeamRef()`)
		// SetParentID delegates to the immediate (last) parent only.
		assert.Contains(t, content, `func (obj *PortalTeamDeveloper) SetParentID(id string) {`)
		assert.Contains(t, content, `obj.SetTeamID(id)`)
		assert.NotContains(t, content, `obj.SetPortalID(id)`)
		// Condition methods use the immediate parent prefix (Team, not Portal).
		assert.Contains(t, content, `return TeamRefValidConditionType`)
		assert.Contains(t, content, `return TeamRefReasonValid`)
		assert.Contains(t, content, `return TeamRefReasonInvalid`)
		assert.Contains(t, content, `return TeamRefReasonNotProgrammed`)
		assert.NotContains(t, content, `return PortalRefValidConditionType`)
	})

	t.Run("root entity without parent gets no status condition methods", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
		})

		rootSchema := &parser.Schema{
			Name:         "CreatePortal",
			Dependencies: []*parser.Dependency{},
		}

		content, err := g.generateCRDFuncs("CreatePortal", rootSchema)
		require.NoError(t, err)
		assert.NotContains(t, content, `GetStatusConditionTypeParentRefValid`)
		assert.NotContains(t, content, `GetStatusConditionReasonParentRefValid`)
		assert.NotContains(t, content, `GetStatusConditionReasonParentRefInvalid`)
		assert.NotContains(t, content, `GetStatusConditionReasonParentRefNotProgrammed`)
	})
}

func TestGenerate_GeneratesFuncsFile(t *testing.T) {
	g := NewGenerator(Config{
		APIGroup:   "x-konnect.konghq.com",
		APIVersion: "v1alpha1",
	})

	files, err := g.Generate(&parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"CreatePortal": {
				Name: "CreatePortal",
				Properties: []*parser.Property{{
					Name: "name",
					Type: "string",
				}},
			},
		},
	})
	require.NoError(t, err)

	var fileNames []string
	for _, file := range files {
		fileNames = append(fileNames, file.Name)
	}

	assert.Contains(t, fileNames, "zz_generated_portal_types.go")
	assert.Contains(t, fileNames, "zz_generated_portal_funcs.go")
}

func TestGenerateCRDType_WithRootUnionGeneratesSafeUnmarshal(t *testing.T) {
	g := NewGenerator(Config{
		APIGroup:   "konnect.konghq.com",
		APIVersion: "v1alpha1",
	})

	schema := &parser.Schema{
		Name:          "CreateEventGatewayListenerPolicy",
		Discriminator: "type",
		OneOf: []*parser.Property{
			{RefName: "EventGatewayTLSListenerPolicy"},
			{RefName: "ForwardToVirtualClusterPolicy"},
		},
		DiscriminatorMapping: map[string]string{
			"EventGatewayTLSListen": "EventGatewayTLSListenerPolicy",
			"ForwardToVirtualClust": "ForwardToVirtualClusterPolicy",
		},
	}

	content, err := g.generateCRDType("CreateEventGatewayListenerPolicy", schema)
	require.NoError(t, err)

	assert.Contains(t, content, "func (u *EventGatewayListenerPolicyConfig) UnmarshalJSON(data []byte) error {")
	assert.Contains(t, content, `return fmt.Errorf("unmarshaling EventGatewayListenerPolicyConfig: nil receiver")`)
	assert.Contains(t, content, "func (s *EventGatewayListenerPolicyAPISpec) MarshalJSON() ([]byte, error) {")
	assert.Contains(t, content, `return []byte("{}"), nil`)
	assert.Contains(t, content, `return nil, fmt.Errorf("marshaling EventGatewayListenerPolicyAPISpec: %w", err)`)
	assert.Contains(t, content, "func (s *EventGatewayListenerPolicyAPISpec) UnmarshalJSON(data []byte) error {")
	assert.Contains(t, content, "aux.EventGatewayListenerPolicyConfig = &EventGatewayListenerPolicyConfig{}")
	assert.Contains(t, content, "aux.EventGatewayListenerPolicyConfig = nil")
	assert.Contains(t, content, `return fmt.Errorf("unmarshaling EventGatewayListenerPolicyAPISpec: %w", err)`)

	_, err = format.Source([]byte(content))
	require.NoError(t, err)
}

func TestGenerateEntityFiles_GeneratesUnionTypeTests(t *testing.T) {
	g := NewGenerator(Config{
		APIGroup:   "konnect.konghq.com",
		APIVersion: "v1alpha1",
	})

	schema := &parser.Schema{
		Name:          "CreateEventGatewayListenerPolicy",
		Discriminator: "type",
		OneOf: []*parser.Property{
			{RefName: "EventGatewayTLSListenerPolicy"},
			{RefName: "ForwardToVirtualClusterPolicy"},
		},
		DiscriminatorMapping: map[string]string{
			"EventGatewayTLSListen": "EventGatewayTLSListenerPolicy",
			"ForwardToVirtualClust": "ForwardToVirtualClusterPolicy",
		},
	}

	files, err := g.generateEntityFiles("CreateEventGatewayListenerPolicy", "EventGatewayListenerPolicy", schema)
	require.NoError(t, err)

	var testFile GeneratedFile
	for _, file := range files {
		if file.Name == "zz_generated_eventgatewaylistenerpolicy_types_test.go" {
			testFile = file
			break
		}
	}
	require.NotEmpty(t, testFile.Name)
	assert.Contains(t, testFile.Content, "func TestEventGatewayListenerPolicyConfigUnmarshalJSON_NilReceiver")
	assert.Contains(t, testFile.Content, "func TestEventGatewayListenerPolicyAPISpecMarshalJSON_NilInlineUnion")
	assert.Contains(t, testFile.Content, "func TestEventGatewayListenerPolicyAPISpecUnmarshalJSON_DecodesUnionFields")
	assert.Contains(t, testFile.Content, "json.Unmarshal(tt.payload, &target)")
	assert.Contains(t, testFile.Content, "payload, err := json.Marshal(target)")

	_, err = format.Source([]byte(testFile.Content))
	require.NoError(t, err)
}

func TestGenerateSharedFiles_GeneratesSchemaUnionTests(t *testing.T) {
	g := NewGenerator(Config{
		APIGroup:   "konnect.konghq.com",
		APIVersion: "v1alpha1",
	})

	parsed := &parser.ParsedSpec{
		Schemas: map[string]*parser.Schema{
			"ForwardToVirtualClusterPolicy": {
				Name: "ForwardToVirtualClusterPolicy",
				Properties: []*parser.Property{
					{
						Name: "config",
						OneOf: []*parser.Property{
							{RefName: "ForwardToClusterByPortMappingConfig"},
							{RefName: "ForwardToClusterBySNIConfig"},
						},
						Discriminator: "type",
						DiscriminatorMapping: map[string]string{
							"port_mapping": "ForwardToClusterByPortMappingConfig",
							"sni":          "ForwardToClusterBySNIConfig",
						},
					},
				},
			},
		},
	}

	files, err := g.generateSharedFiles(parsed, map[string]bool{"ForwardToVirtualClusterPolicy": true}, nil)
	require.NoError(t, err)

	var schemaFile GeneratedFile
	var schemaTestFile GeneratedFile
	for _, file := range files {
		switch file.Name {
		case "schema_types.go":
			schemaFile = file
		case "schema_types_test.go":
			schemaTestFile = file
		}
	}

	require.NotEmpty(t, schemaFile.Name)
	require.NotEmpty(t, schemaTestFile.Name)
	assert.Contains(t, schemaFile.Content, "func (s *ForwardToVirtualClusterPolicy) UnmarshalJSON(data []byte) error {")
	assert.Contains(t, schemaFile.Content, "aux.Config = &ForwardToVirtualClusterPolicyConfig{}")
	assert.Contains(t, schemaTestFile.Content, "func TestForwardToVirtualClusterPolicyConfigUnmarshalJSON_NilReceiver")
	assert.Contains(t, schemaTestFile.Content, "func TestForwardToVirtualClusterPolicyUnmarshalJSON_DecodesUnionFields")

	_, err = format.Source([]byte(schemaFile.Content))
	require.NoError(t, err)

	_, err = format.Source([]byte(schemaTestFile.Content))
	require.NoError(t, err)
}

func TestGenerateSchemaTypes_OmitsDiscriminatorFieldOnUnionMembersOnly(t *testing.T) {
	g := NewGenerator(Config{
		APIGroup:   "konnect.konghq.com",
		APIVersion: "v1alpha1",
	})

	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"CreateRootUnion": {
				Name:          "CreateRootUnion",
				Discriminator: "type",
				OneOf: []*parser.Property{
					{RefName: "RootVariantAlpha"},
					{RefName: "RootVariantBeta"},
				},
				DiscriminatorMapping: map[string]string{
					"alpha": "RootVariantAlpha",
					"beta":  "RootVariantBeta",
				},
			},
			"CreatePropertyUnion": {
				Name: "CreatePropertyUnion",
				Properties: []*parser.Property{
					{
						Name:          "config",
						Discriminator: "type",
						OneOf: []*parser.Property{
							{RefName: "PropertyVariantAlpha"},
							{RefName: "PropertyVariantBeta"},
						},
						DiscriminatorMapping: map[string]string{
							"alpha": "PropertyVariantAlpha",
							"beta":  "PropertyVariantBeta",
						},
					},
				},
			},
		},
		Schemas: map[string]*parser.Schema{
			"RootVariantAlpha": {
				Name: "RootVariantAlpha",
				Properties: []*parser.Property{
					{Name: "name", Type: "string"},
					{Name: "type", Type: "string"},
				},
			},
			"RootVariantBeta": {
				Name: "RootVariantBeta",
				Properties: []*parser.Property{
					{Name: "count", Type: "integer"},
					{Name: "type", Type: "string"},
				},
			},
			"PropertyVariantAlpha": {
				Name: "PropertyVariantAlpha",
				Properties: []*parser.Property{
					{Name: "enabled", Type: "boolean"},
					{Name: "type", Type: "string"},
				},
			},
			"PropertyVariantBeta": {
				Name: "PropertyVariantBeta",
				Properties: []*parser.Property{
					{Name: "labels", Type: "string"},
					{Name: "type", Type: "string"},
				},
			},
			"StandaloneTypeCarrier": {
				Name: "StandaloneTypeCarrier",
				Properties: []*parser.Property{
					{Name: "label", Type: "string"},
					{Name: "type", Type: "string"},
				},
			},
		},
	}

	content := g.generateSchemaTypes(map[string]bool{
		"RootVariantAlpha":      true,
		"RootVariantBeta":       true,
		"PropertyVariantAlpha":  true,
		"PropertyVariantBeta":   true,
		"StandaloneTypeCarrier": true,
	}, parsed, nil)

	assert.NotContains(t, generatedStructBlock(t, content, "RootVariantAlpha"), "Type string")
	assert.NotContains(t, generatedStructBlock(t, content, "RootVariantBeta"), "Type string")
	assert.NotContains(t, generatedStructBlock(t, content, "PropertyVariantAlpha"), "Type string")
	assert.NotContains(t, generatedStructBlock(t, content, "PropertyVariantBeta"), "Type string")
	assert.Contains(t, generatedStructBlock(t, content, "StandaloneTypeCarrier"), "Type string `json:\"type,omitzero\"`")

	_, err := format.Source([]byte(content))
	require.NoError(t, err)
}

func generatedStructBlock(t *testing.T, content, typeName string) string {
	t.Helper()

	start := strings.Index(content, "type "+typeName+" struct {")
	require.NotEqual(t, -1, start, "type %s struct not found", typeName)

	end := strings.Index(content[start:], "\n}\n")
	require.NotEqual(t, -1, end, "type %s struct end not found", typeName)

	return content[start : start+end+3]
}

func TestTagOmitSuffix(t *testing.T) {
	tests := []struct {
		goType string
		want   string
	}{
		{"string", ",omitzero"},
		{"int64", ",omitzero"},
		{"float64", ",omitzero"},
		{"VirtualClusterNamespace", ",omitzero"},
		{"VirtualClusterName", ",omitzero"},
		{"apiextensionsv1.JSON", ",omitzero"},
		{"*string", ",omitempty"},
		{"*KonnectEntityRef", ",omitempty"},
		{"[]VirtualClusterTopicAlias", ",omitempty"},
		{"[]metav1.Condition", ",omitempty"},
		{"map[string]Labels", ",omitempty"},
		{"map[string]string", ",omitempty"},
	}
	for _, tc := range tests {
		t.Run(tc.goType, func(t *testing.T) {
			assert.Equal(t, tc.want, tagOmitSuffix(tc.goType))
		})
	}
}

func TestGenerateSchemaTypes_EmitsOmitzeroForStructFields(t *testing.T) {
	g := NewGenerator(Config{APIVersion: "v1alpha1"})
	parsed := &parser.ParsedSpec{
		Schemas: map[string]*parser.Schema{
			"VirtualClusterNamespace": {
				Name: "VirtualClusterNamespace",
				Properties: []*parser.Property{
					{Name: "prefix", Type: "string"},
					{Name: "mode", Type: "string"},
				},
			},
			"ParentSchema": {
				Name: "ParentSchema",
				Properties: []*parser.Property{
					{Name: "namespace", RefName: "VirtualClusterNamespace"},
				},
			},
		},
	}

	content := g.generateSchemaTypes(map[string]bool{
		"VirtualClusterNamespace": true,
		"ParentSchema":            true,
	}, parsed, nil)

	assert.Contains(t, content, "Namespace VirtualClusterNamespace `json:\"namespace,omitzero\"`")
	assert.NotContains(t, content, "Namespace VirtualClusterNamespace `json:\"namespace,omitempty\"`")
	_, err := format.Source([]byte(content))
	require.NoError(t, err)
}

func TestEntityFilePrefix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "non-Konnect entity",
			input:    "Portal",
			expected: "portal",
		},
		{
			name:     "Konnect-prefixed entity",
			input:    "KonnectEventGateway",
			expected: "konnect_eventgateway",
		},
		{
			name:     "Konnect alone stays unchanged",
			input:    "Konnect",
			expected: "konnect",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, EntityFilePrefix(tc.input))
		})
	}
}

func TestGenerate_GroupVersionInfo(t *testing.T) {
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"CreatePortal": {
				Name: "CreatePortal",
				Properties: []*parser.Property{{
					Name: "name",
					Type: "string",
				}},
			},
		},
	}

	t.Run("enabled generates groupversion_info.go and zz_generated_groupversion_info.go", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:                 "x-konnect.konghq.com",
			APIVersion:               "v1alpha1",
			GenerateGroupVersionInfo: true,
		})

		files, err := g.Generate(parsed)
		require.NoError(t, err)

		var fileNames []string
		var gviContent, gviGeneratedContent string
		for _, file := range files {
			fileNames = append(fileNames, file.Name)
			if file.Name == "groupversion_info.go" {
				gviContent = file.Content
			}
			if file.Name == "zz_generated_groupversion_info.go" {
				gviGeneratedContent = file.Content
			}
		}

		assert.Contains(t, fileNames, "groupversion_info.go")
		assert.Contains(t, fileNames, "zz_generated_groupversion_info.go")
		assert.NotContains(t, fileNames, "register.go")

		// Full file: group/version vars, scheme wiring, delegation to generated func.
		assert.Contains(t, gviContent, `GroupVersion = schema.GroupVersion{Group: GroupName, Version: "v1alpha1"}`)
		assert.Contains(t, gviContent, `GroupName = "x-konnect.konghq.com"`)
		assert.Contains(t, gviContent, "SchemeGroupVersion = GroupVersion")
		assert.Contains(t, gviContent, "SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)")
		assert.Contains(t, gviContent, "func Resource(resource string) schema.GroupResource {")
		assert.Contains(t, gviContent, "func addKnownTypes(scheme *runtime.Scheme) error {")
		assert.Contains(t, gviContent, "return addKnownTypesGenerated(scheme)")
		assert.NotContains(t, gviContent, "&Portal{}")
		assert.Contains(t, gviContent, "Code generated by CRD generation pipeline. DO NOT EDIT.")

		// Additive file: entity list in addKnownTypesGenerated only.
		assert.Contains(t, gviGeneratedContent, "func addKnownTypesGenerated(scheme *runtime.Scheme) error {")
		assert.Contains(t, gviGeneratedContent, "&Portal{}")
		assert.Contains(t, gviGeneratedContent, "&PortalList{}")
		assert.Contains(t, gviGeneratedContent, "Code generated by CRD generation pipeline. DO NOT EDIT.")
	})

	t.Run("disabled skips groupversion_info.go but still emits zz_generated_groupversion_info.go", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:                 "konnect.konghq.com",
			APIVersion:               "v1alpha1",
			GenerateGroupVersionInfo: false,
		})

		files, err := g.Generate(parsed)
		require.NoError(t, err)

		var fileNames []string
		var gviGeneratedContent string
		for _, file := range files {
			fileNames = append(fileNames, file.Name)
			if file.Name == "zz_generated_groupversion_info.go" {
				gviGeneratedContent = file.Content
			}
		}

		assert.NotContains(t, fileNames, "groupversion_info.go")
		assert.NotContains(t, fileNames, "register.go")
		assert.Contains(t, fileNames, "zz_generated_groupversion_info.go")
		assert.Contains(t, gviGeneratedContent, "func addKnownTypesGenerated(scheme *runtime.Scheme) error {")
		assert.Contains(t, gviGeneratedContent, "&Portal{}")
		assert.Contains(t, gviGeneratedContent, "&PortalList{}")
	})
}

func TestGenerate_OmitsUnusedArrayRefAliases(t *testing.T) {
	// EntityScopes is a $ref whose schema is type:array. goType inlines it as
	// []string so the alias is never referenced — it must not be emitted.
	// EntityConfig is a $ref whose schema is type:object — it IS referenced by
	// name and must be emitted, along with any named refs it transitively uses.
	g := NewGenerator(Config{APIVersion: "v1alpha1"})

	files, err := g.Generate(&parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"CreateEntity": {
				Name: "CreateEntity",
				Properties: []*parser.Property{
					{
						Name:    "config",
						RefName: "EntityConfig",
						Type:    "object",
					},
					{
						Name:    "scopes",
						RefName: "EntityScopes",
						Type:    "array",
						Items:   &parser.Property{Type: "string"},
					},
				},
			},
		},
		Schemas: map[string]*parser.Schema{
			"EntityConfig": {
				Name: "EntityConfig",
				Properties: []*parser.Property{
					{Name: "name", Type: "string"},
				},
			},
			"EntityScopes": {
				Name:  "EntityScopes",
				Type:  "array",
				Items: &parser.Property{Type: "string"},
			},
		},
	})
	require.NoError(t, err)

	var schemaContent string
	for _, f := range files {
		if f.Name == "schema_types.go" {
			schemaContent = f.Content
			break
		}
	}
	require.NotEmpty(t, schemaContent, "schema_types.go should be generated")
	assert.Contains(t, schemaContent, "type EntityConfig struct")
	assert.NotContains(t, schemaContent, "EntityScopes", "array-typed $ref alias must not be emitted")
}

func TestGenerateSchemaTypes_AddsKubebuilderTags(t *testing.T) {
	g := NewGenerator(Config{APIVersion: "v1alpha1"})
	parsed := &parser.ParsedSpec{
		Schemas: map[string]*parser.Schema{
			"CreateDcrProviderRequestOkta": {
				Name: "CreateDcrProviderRequestOkta",
				Properties: []*parser.Property{
					{
						Name: "provider_type",
						Type: "string",
					},
				},
			},
		},
	}

	content := g.generateSchemaTypes(map[string]bool{"CreateDcrProviderRequestOkta": true}, parsed, nil)

	assert.Contains(t, content, "// +optional")
	assert.Contains(t, content, fmt.Sprintf("// +kubebuilder:validation:MaxLength=%d", defaultMaxLength))
	assert.Contains(t, content, "ProviderType string `json:\"providerType,omitzero\"`")
}

func TestBuildSchemaTypeFieldConfig_NestedInlineObject(t *testing.T) {
	certProp := &parser.Property{Name: "certificate", Type: "string", Required: true}
	ciProp := &parser.Property{
		Name: "client_identity", Type: "object",
		Properties: []*parser.Property{certProp},
	}
	tlsProp := &parser.Property{Name: "tls", Type: "object", RefName: "BackendClusterTLS"}
	entitySchema := &parser.Schema{
		Name:       "CreateEventGatewayBackendCluster",
		Properties: []*parser.Property{tlsProp},
	}
	bctSchema := &parser.Schema{
		Name:       "BackendClusterTLS",
		Properties: []*parser.Property{ciProp},
	}

	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"CreateEventGatewayBackendCluster": entitySchema,
		},
		Schemas: map[string]*parser.Schema{
			"BackendClusterTLS": bctSchema,
		},
	}

	fieldCfg := &config.Config{
		Entities: map[string]*config.EntityConfig{
			"EventGatewayBackendCluster": {
				Fields: map[string]*config.FieldConfig{
					"tls": {
						Fields: map[string]*config.FieldConfig{
							"client_identity": {
								Fields: map[string]*config.FieldConfig{
									"certificate": {
										Validations: []string{"+kubebuilder:validation:MaxLength=1024"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	gen := NewGenerator(Config{APIVersion: "v1alpha1", FieldConfig: fieldCfg})
	stfc := gen.buildSchemaTypeFieldConfig(parsed)
	require.NotNil(t, stfc)
	vals := stfc.GetFieldValidations("ClientIdentity", "certificate")
	assert.Equal(t, []string{"+kubebuilder:validation:MaxLength=1024"}, vals)
}

func TestBuildSchemaTypeFieldConfig_RootOneOfVariant(t *testing.T) {
	// Entity schema is a discriminated oneOf (no Properties). Config keys address
	// variant ref names. Validations on a prop inside the variant must propagate.
	// Note: entity name must not contain prefixes stripped by GetEntityNameFromType
	// (Add/Create/Update/Delete/Get/List), e.g. "ListenerPolicy" contains "List".
	certProp := &parser.Property{Name: "certificate", Type: "string", Required: true}
	variantSchema := &parser.Schema{
		Name:       "TLSChannelPolicy",
		Properties: []*parser.Property{certProp},
	}
	entitySchema := &parser.Schema{
		Name: "CreateChannelPolicy",
		OneOf: []*parser.Property{
			{Name: "TLSChannelPolicy", RefName: "TLSChannelPolicy"},
		},
	}

	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"CreateChannelPolicy": entitySchema},
		Schemas:       map[string]*parser.Schema{"TLSChannelPolicy": variantSchema},
	}

	fieldCfg := &config.Config{
		Entities: map[string]*config.EntityConfig{
			"ChannelPolicy": {
				Fields: map[string]*config.FieldConfig{
					"TLSChannelPolicy": {
						Fields: map[string]*config.FieldConfig{
							"certificate": {
								Validations: []string{"+kubebuilder:validation:MaxLength=4096"},
							},
						},
					},
				},
			},
		},
	}

	gen := NewGenerator(Config{APIVersion: "v1alpha1", FieldConfig: fieldCfg})
	stfc := gen.buildSchemaTypeFieldConfig(parsed)
	require.NotNil(t, stfc)
	vals := stfc.GetFieldValidations("TLSChannelPolicy", "certificate")
	assert.Equal(t, []string{"+kubebuilder:validation:MaxLength=4096"}, vals)
}

func TestBuildSchemaTypeFieldConfig_ArrayOfRefItems(t *testing.T) {
	// Config descends through an array-of-$ref property into the item schema.
	certProp := &parser.Property{Name: "certificate", Type: "string", Required: true}
	itemSchema := &parser.Schema{
		Name:       "TLSCertificate",
		Properties: []*parser.Property{certProp},
	}
	certsProp := &parser.Property{
		Name: "certificates", Type: "array",
		Items: &parser.Property{RefName: "TLSCertificate"},
	}
	entitySchema := &parser.Schema{
		Name:       "CreateTLSEntity",
		Properties: []*parser.Property{certsProp},
	}

	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"CreateTLSEntity": entitySchema},
		Schemas:       map[string]*parser.Schema{"TLSCertificate": itemSchema},
	}

	fieldCfg := &config.Config{
		Entities: map[string]*config.EntityConfig{
			"TLSEntity": {
				Fields: map[string]*config.FieldConfig{
					"certificates": {
						Fields: map[string]*config.FieldConfig{
							"certificate": {
								Validations: []string{"+kubebuilder:validation:MaxLength=4096"},
							},
						},
					},
				},
			},
		},
	}

	gen := NewGenerator(Config{APIVersion: "v1alpha1", FieldConfig: fieldCfg})
	stfc := gen.buildSchemaTypeFieldConfig(parsed)
	require.NotNil(t, stfc)
	vals := stfc.GetFieldValidations("TLSCertificate", "certificate")
	assert.Equal(t, []string{"+kubebuilder:validation:MaxLength=4096"}, vals)
}

func TestGenerateSchemaTypes_NestedInlineOverride(t *testing.T) {
	certProp := &parser.Property{Name: "certificate", Type: "string", Required: true}
	ciProp := &parser.Property{
		Name: "client_identity", Type: "object",
		Properties: []*parser.Property{certProp},
	}
	bctSchema := &parser.Schema{
		Name:       "BackendClusterTLS",
		Properties: []*parser.Property{ciProp},
	}

	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{},
		Schemas:       map[string]*parser.Schema{"BackendClusterTLS": bctSchema},
	}

	schemaTypeFieldConfig := &config.Config{
		Entities: map[string]*config.EntityConfig{
			"ClientIdentity": {
				Fields: map[string]*config.FieldConfig{
					"certificate": {
						Validations: []string{"+kubebuilder:validation:MaxLength=1024"},
					},
				},
			},
		},
	}

	gen := NewGenerator(Config{APIVersion: "v1alpha1"})
	content := gen.generateSchemaTypes(
		map[string]bool{"BackendClusterTLS": true},
		parsed,
		schemaTypeFieldConfig,
	)
	assert.Contains(t, content, "// +kubebuilder:validation:MaxLength=1024",
		"user-provided MaxLength should appear in generated type")
	assert.NotContains(t, content, "// +kubebuilder:validation:MaxLength=253",
		"default MaxLength should be replaced by user-provided override")
}

func TestObjectRefNamespaced(t *testing.T) {
	tests := []struct {
		name        string
		commonTypes *config.CommonTypesConfig
		want        bool
	}{
		{
			name: "nil commonTypes",
			want: false,
		},
		{
			name:        "nil objectRef",
			commonTypes: &config.CommonTypesConfig{},
			want:        false,
		},
		{
			name: "namespaced false",
			commonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{Namespaced: false},
			},
			want: false,
		},
		{
			name: "namespaced true",
			commonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{Namespaced: true},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGenerator(Config{CommonTypes: tc.commonTypes})
			assert.Equal(t, tc.want, g.objectRefNamespaced())
		})
	}
}

func TestObjectRefImported(t *testing.T) {
	tests := []struct {
		name        string
		commonTypes *config.CommonTypesConfig
		want        bool
	}{
		{
			name: "nil commonTypes",
			want: false,
		},
		{
			name:        "nil objectRef",
			commonTypes: &config.CommonTypesConfig{},
			want:        false,
		},
		{
			name: "generate true, no import",
			commonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{Generate: new(true)},
			},
			want: false,
		},
		{
			name: "import set",
			commonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{Path: "some/path"},
				},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGenerator(Config{CommonTypes: tc.commonTypes})
			assert.Equal(t, tc.want, g.objectRefImported())
		})
	}
}

func TestExtractVariantNames(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty input",
			input:    []string{},
			expected: nil,
		},
		{
			name:     "single variant - removes common prefixes/suffixes",
			input:    []string{"CreateDcrProviderRequestAuth0"},
			expected: []string{"DcrProviderRequestAuth0"}, // Request not removed since it's not at the end
		},
		{
			name:     "identity provider variants - OIDC and SAML",
			input:    []string{"ConfigureOIDCIdentityProviderConfig", "SAMLIdentityProviderConfig"},
			expected: []string{"OIDC", "SAML"},
		},
		{
			name:     "dcr provider variants - multiple providers",
			input:    []string{"CreateDcrProviderRequestAuth0", "CreateDcrProviderRequestAzureAd", "CreateDcrProviderRequestCurity", "CreateDcrProviderRequestOkta", "CreateDcrProviderRequestHttp"},
			expected: []string{"Auth0", "AzureAd", "Curity", "Okta", "Http"},
		},
		{
			name:     "common prefix only",
			input:    []string{"ConfigTypeA", "ConfigTypeB"},
			expected: []string{"A", "B"},
		},
		{
			name:     "common suffix only",
			input:    []string{"AConfig", "BConfig"},
			expected: []string{"A", "B"},
		},
		{
			name:     "no common prefix or suffix - common suffix 'a' is too short",
			input:    []string{"Alpha", "Beta"},
			expected: []string{"Alph", "Bet"}, // common suffix is "a" so it gets trimmed
		},
		{
			name:     "variants with Configure prefix",
			input:    []string{"ConfigureAuth", "ConfigureSAML"},
			expected: []string{"Auth", "SAML"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extractVariantNames(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCommonPrefix(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected string
	}{
		{
			name:     "identical strings",
			a:        "hello",
			b:        "hello",
			expected: "hello",
		},
		{
			name:     "common prefix",
			a:        "CreateDcrProviderRequestAuth0",
			b:        "CreateDcrProviderRequestAzureAd",
			expected: "CreateDcrProviderRequestA",
		},
		{
			name:     "no common prefix",
			a:        "alpha",
			b:        "beta",
			expected: "",
		},
		{
			name:     "empty strings",
			a:        "",
			b:        "",
			expected: "",
		},
		{
			name:     "one empty string",
			a:        "hello",
			b:        "",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := commonPrefix(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCommonSuffix(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected string
	}{
		{
			name:     "identical strings",
			a:        "hello",
			b:        "hello",
			expected: "hello",
		},
		{
			name:     "common suffix",
			a:        "ConfigureOIDCIdentityProviderConfig",
			b:        "SAMLIdentityProviderConfig",
			expected: "IdentityProviderConfig",
		},
		{
			name:     "no common suffix",
			a:        "alpha",
			b:        "beta",
			expected: "a",
		},
		{
			name:     "empty strings",
			a:        "",
			b:        "",
			expected: "",
		},
		{
			name:     "one empty string",
			a:        "hello",
			b:        "",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := commonSuffix(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCleanSingleVariantName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "removes Config suffix",
			input:    "SomethingConfig",
			expected: "Something",
		},
		{
			name:     "removes Configuration suffix",
			input:    "SomethingConfiguration",
			expected: "Something",
		},
		{
			name:     "removes Provider suffix",
			input:    "SomethingProvider",
			expected: "Something",
		},
		{
			name:     "removes Request suffix",
			input:    "SomethingRequest",
			expected: "Something",
		},
		{
			name:     "removes Configure prefix",
			input:    "ConfigureSomething",
			expected: "Something",
		},
		{
			name:     "removes Create prefix",
			input:    "CreateSomething",
			expected: "Something",
		},
		{
			name:     "removes Update prefix",
			input:    "UpdateSomething",
			expected: "Something",
		},
		{
			name:     "removes multiple prefixes/suffixes",
			input:    "CreateSomethingRequest",
			expected: "Something",
		},
		{
			name:     "no prefixes or suffixes to remove",
			input:    "Something",
			expected: "Something",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := cleanSingleVariantName(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFixInitialisms(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Http becomes HTTP",
			input:    "CreateDcrConfigHttpInRequest",
			expected: "CreateDcrConfigHTTPInRequest",
		},
		{
			name:     "trailing Http becomes HTTP",
			input:    "CreateDcrProviderRequestHttp",
			expected: "CreateDcrProviderRequestHTTP",
		},
		{
			name:     "Url becomes URL",
			input:    "DcrBaseUrl",
			expected: "DcrBaseURL",
		},
		{
			name:     "Api becomes API",
			input:    "DcrConfigPropertyApiKey",
			expected: "DcrConfigPropertyAPIKey",
		},
		{
			name:     "trailing Id becomes ID",
			input:    "DcrConfigPropertyInitialClientId",
			expected: "DcrConfigPropertyInitialClientID",
		},
		{
			name:     "already correct initialisms unchanged",
			input:    "DcrBaseURL",
			expected: "DcrBaseURL",
		},
		{
			name:     "multiple initialisms fixed",
			input:    "HttpApiUrl",
			expected: "HTTPAPIURL",
		},
		{
			name:     "no initialisms unchanged",
			input:    "CreatePortal",
			expected: "CreatePortal",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := fixInitialisms(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSplitPascalCase(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple PascalCase",
			input:    "CreatePortal",
			expected: []string{"Create", "Portal"},
		},
		{
			name:     "multiple words",
			input:    "CreateDcrConfigHttpInRequest",
			expected: []string{"Create", "Dcr", "Config", "Http", "In", "Request"},
		},
		{
			name:     "single word",
			input:    "Portal",
			expected: []string{"Portal"},
		},
		{
			name:     "existing acronym sequence",
			input:    "DcrBaseURL",
			expected: []string{"Dcr", "Base", "U", "R", "L"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := splitPascalCase(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFormatSchemaComment(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		desc     string
		expected string
	}{
		{
			name:     "empty description",
			typeName: "Labels",
			desc:     "",
			expected: "// Labels is a type alias.\n",
		},
		{
			name:     "description does not start with type name",
			typeName: "Labels",
			desc:     "Store metadata of an entity.",
			expected: "// Labels Store metadata of an entity.\n",
		},
		{
			name:     "description starts with type name - no stutter",
			typeName: "Labels",
			desc:     "Labels store metadata of an entity.",
			expected: "// Labels store metadata of an entity.\n",
		},
		{
			name:     "description starts with type name followed by dot",
			typeName: "Labels",
			desc:     "Labels.",
			expected: "// Labels.\n",
		},
		{
			name:     "description equals type name exactly",
			typeName: "Labels",
			desc:     "Labels",
			expected: "// Labels\n",
		},
		{
			name:     "trailing empty lines are stripped",
			typeName: "Labels",
			desc:     "Labels store metadata.\n\n",
			expected: "// Labels store metadata.\n",
		},
		{
			name:     "multiline with trailing empty lines stripped",
			typeName: "Labels",
			desc:     "Labels store metadata.\n\nKeys must be 1-63 chars.\n\n",
			expected: "// Labels store metadata.\n//\n// Keys must be 1-63 chars.\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := formatSchemaComment(tc.typeName, tc.desc)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCollectSDKOpsBoolFields(t *testing.T) {
	g := NewGenerator(Config{})
	schema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "enabled",
				Type: "boolean",
			},
			{
				Name: "settings",
				Type: "object",
				Properties: []*parser.Property{
					{
						Name: "nested_enabled",
						Type: "boolean",
					},
				},
			},
			{
				Name: "items",
				Type: "array",
				Items: &parser.Property{
					Type: "object",
					Properties: []*parser.Property{
						{
							Name: "item_enabled",
							Type: "boolean",
						},
					},
				},
			},
			{
				Name: "flags",
				Type: "object",
				AdditionalProperties: &parser.Property{
					Type: "boolean",
				},
			},
		},
	}

	assert.Equal(t, []sdkOpsBoolField{
		{Label: "enabled", Path: []string{"enabled"}},
		{Label: "flags.{}", Path: []string{"flags", "{}"}},
		{Label: "items.[].item_enabled", Path: []string{"items", "[]", "item_enabled"}},
		{Label: "settings.nested_enabled", Path: []string{"settings", "nested_enabled"}},
	}, g.collectSDKOpsBoolFields(schema))
}

func TestGenerateSDKOps_NormalizesBooleanFields(t *testing.T) {
	g := NewGenerator(Config{APIVersion: "v1alpha1"})
	schema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "name",
				Type: "string",
			},
			{
				Name: "rbac_enabled",
				Type: "boolean",
			},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {
				Path: "github.com/Kong/sdk-konnect-go/models/components.CreatePortal",
			},
		},
	}

	content, err := g.generateSDKOps("Portal", schema, opsConfig)
	require.NoError(t, err)
	assert.Contains(t, content, "var PortalSDKOpsBoolFields = []PortalSDKOpsBoolField")
	assert.Contains(t, content, "func (s *PortalAPISpec) marshalSDKOpsPayload() ([]byte, error)")
	assert.Contains(t, content, "data, err := s.marshalSDKOpsPayload()")
	assert.Contains(t, content, "Label: \"rbac_enabled\"")
	assert.Contains(t, content, "failed to normalize PortalAPISpec SDK payload")
	assert.Contains(t, content, "case \"Enabled\":")
	assert.Contains(t, content, "return true, nil")
	assert.NotContains(t, content, "error) {\n\n\tdata")
	assert.NotContains(t, content, "err)\n\n\tvar target")
	assert.NotContains(t, content, "}var target")
	assert.NotContains(t, content, "}\n\n\n// ToCreate")
	assert.Contains(t, content, "}\n\tvar target")
	assert.Contains(t, content, "}\n\n// ToCreate")
	assert.Contains(t, content, "payload = flattenSDKUnions(payload)")
	assert.Contains(t, content, "if pm, ok := payload.(map[string]any); ok {")
	assert.Contains(t, content, "if err := normalizePortalSDKOpsBoolFields(pm); err != nil {")
}

func TestGenerateSDKOps_RootUnionUsesSelectedVariantPayload(t *testing.T) {
	g := NewGenerator(Config{APIVersion: "v1alpha1"})
	schema := &parser.Schema{
		OneOf: []*parser.Property{
			{
				Name:    "CreateDcrProviderRequestAuth0",
				RefName: "CreateDcrProviderRequestAuth0",
				Properties: []*parser.Property{
					{
						Name:     "provider_config",
						RefName:  "CreateDcrConfigAuth0InRequest",
						Required: true,
					},
				},
			},
			{
				Name:    "CreateDcrProviderRequestHttp",
				RefName: "CreateDcrProviderRequestHttp",
				Properties: []*parser.Property{
					{
						Name:     "provider_config",
						RefName:  "CreateDcrConfigHTTPInRequest",
						Required: true,
					},
				},
			},
		},
		Properties: []*parser.Property{
			{
				Name: "auth0",
				Type: "object",
				Properties: []*parser.Property{
					{
						Name: "provider_config",
						Type: "object",
						Properties: []*parser.Property{
							{
								Name: "use_developer_managed_scopes",
								Type: "boolean",
							},
						},
					},
				},
			},
			{
				Name: "http",
				Type: "object",
				Properties: []*parser.Property{
					{
						Name: "provider_config",
						Type: "object",
						Properties: []*parser.Property{
							{
								Name: "allow_multiple_credentials",
								Type: "boolean",
							},
						},
					},
				},
			},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {
				Path: "github.com/Kong/sdk-konnect-go/models/components.CreateDcrProviderRequest",
			},
			"update": {
				Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateDcrProviderRequest",
			},
		},
	}

	content, err := g.generateSDKOps("DcrProvider", schema, opsConfig)
	require.NoError(t, err)
	assert.Contains(t, content, "func (s *DcrProviderAPISpec) selectedSDKOpsPayload(payload map[string]any) ([]byte, string, error)")
	assert.Contains(t, content, `selected = payload["auth0"]`)
	assert.Contains(t, content, `selected = payload["http"]`)
	assert.Contains(t, content, "DcrProvider config is required")
	assert.Contains(t, content, "CreateCreateDcrProviderRequestAuth0")
	assert.Contains(t, content, "CreateCreateDcrProviderRequestHTTP")
	assert.Contains(t, content, `configPayload, ok := selected["provider_config"]`)
	assert.Contains(t, content, "CreateProviderConfigUpdateDcrConfigAuth0InRequest")
	assert.Contains(t, content, "target.ProviderConfig = &unionValue")
	assert.Contains(t, content, "failed to normalize DcrProviderAPISpec SDK payload")
	assert.NotContains(t, content, "}\n\n\n// ToCreate")
	assert.Contains(t, content, "}\n\n// ToCreate")
	assert.NotContains(t, content, `selected["dcr_config"]`)
	assert.NotContains(t, content, "target.DcrConfig = &unionValue")
}

func TestGenerateSDKOps_NestedUnionFlattensSelectedPayload(t *testing.T) {
	g := NewGenerator(Config{APIVersion: "v1alpha1"})
	schema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name:    "config",
				RefName: "Config",
				OneOf: []*parser.Property{
					{
						Name:    "OIDCIdentityProviderConfig",
						RefName: "OIDCIdentityProviderConfig",
					},
					{
						Name:    "SAMLIdentityProviderConfig",
						RefName: "SAMLIdentityProviderConfig",
					},
				},
			},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {
				Path: "github.com/Kong/sdk-konnect-go/models/components.CreateIdentityProvider",
			},
			"update": {
				Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateIdentityProvider",
			},
		},
	}

	content, err := g.generateSDKOps("IdentityProviderRequest", schema, opsConfig)
	require.NoError(t, err)
	// Generic walker handles all nested union shapes — no per-field codegen.
	assert.Contains(t, content, "payload = flattenSDKUnions(payload)")
	assert.NotContains(t, content, `rawConfig, ok := payload["config"]`)
	assert.NotContains(t, content, `objectConfig["type"]`)
	assert.NotContains(t, content, `selectedConfig = objectConfig["oidc"]`)
}

func TestFindRootUnionUpdatePayloadProperty(t *testing.T) {
	t.Run("prefers single required ref property", func(t *testing.T) {
		prop, err := findRootUnionUpdatePayloadProperty([]*parser.Property{
			{Name: "display_name"},
			{Name: "provider_config", RefName: "CreatePayload", Required: true},
			{Name: "labels"},
		})
		require.NoError(t, err)
		require.NotNil(t, prop)
		assert.Equal(t, "provider_config", prop.Name)
	})

	t.Run("falls back to single ref property", func(t *testing.T) {
		prop, err := findRootUnionUpdatePayloadProperty([]*parser.Property{
			{Name: "provider_config", RefName: "CreatePayload"},
			{Name: "labels"},
		})
		require.NoError(t, err)
		require.NotNil(t, prop)
		assert.Equal(t, "provider_config", prop.Name)
	})

	t.Run("errors on multiple required ref properties", func(t *testing.T) {
		prop, err := findRootUnionUpdatePayloadProperty([]*parser.Property{
			{Name: "provider_config", RefName: "CreatePayload", Required: true},
			{Name: "client_config", RefName: "CreateClientPayload", Required: true},
		})
		require.Error(t, err)
		assert.Nil(t, prop)
		assert.Contains(t, err.Error(), "multiple required ref payload properties")
	})

	t.Run("errors on ambiguous ref properties", func(t *testing.T) {
		prop, err := findRootUnionUpdatePayloadProperty([]*parser.Property{
			{Name: "provider_config", RefName: "CreatePayload"},
			{Name: "client_config", RefName: "CreateClientPayload"},
		})
		require.Error(t, err)
		assert.Nil(t, prop)
		assert.Contains(t, err.Error(), "multiple ref payload properties")
	})

	t.Run("returns nil when no ref properties", func(t *testing.T) {
		prop, err := findRootUnionUpdatePayloadProperty([]*parser.Property{
			{Name: "display_name"},
		})
		require.NoError(t, err)
		assert.Nil(t, prop)
	})
}

func TestGenerateSDKOpsTest_AssertsNormalizedPayload(t *testing.T) {
	g := NewGenerator(Config{APIVersion: "v1alpha1"})
	schema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "name",
				Type: "string",
			},
			{
				Name: "rbac_enabled",
				Type: "boolean",
			},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {
				Path: "github.com/Kong/sdk-konnect-go/models/components.CreatePortal",
			},
		},
	}

	content, err := g.generateSDKOpsTest("Portal", schema, opsConfig)
	require.NoError(t, err)
	assert.Contains(t, content, `RBACEnabled: "Enabled"`)
	assert.Contains(t, content, `require.Equal(t, true, payload["rbac_enabled"])`)
	assert.Contains(t, content, `require.Equal(t, "test-value", payload["name"])`)
}

func TestGenerateSDKOpsTest_SupportsPointerAndNamedFields(t *testing.T) {
	g := NewGenerator(Config{APIVersion: "v1alpha1"})
	schema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name:     "description",
				Type:     "string",
				Nullable: true,
			},
			{
				Name:    "labels",
				Type:    "object",
				RefName: "Labels",
				AdditionalProperties: &parser.Property{
					Name:    "value",
					Type:    "string",
					RefName: "LabelsValue",
				},
			},
			{
				Name:    "min_runtime_version",
				Type:    "string",
				RefName: "MinRuntimeVersion",
			},
			{
				Name:    "name",
				Type:    "string",
				RefName: "GatewayName",
			},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {
				Path: "github.com/Kong/sdk-konnect-go/models/components.CreateGatewayRequest",
			},
		},
	}

	content, err := g.generateSDKOpsTest("KonnectEventGateway", schema, opsConfig)
	require.NoError(t, err)
	assert.Contains(t, content, `Description: new("test-value")`)
	assert.Contains(t, content, `Labels: Labels{"test-key": "test-value"}`)
	assert.Contains(t, content, `MinRuntimeVersion: MinRuntimeVersion("test-value")`)
	assert.Contains(t, content, `Name: GatewayName("test-value")`)
	assert.Contains(t, content, `require.Equal(t, map[string]any{"test-key": "test-value"}, payload["labels"])`)
	assert.Contains(t, content, `require.Equal(t, "test-value", payload["min_runtime_version"])`)
	assert.Contains(t, content, `require.Equal(t, "test-value", payload["name"])`)
}

func TestGenerateSDKOpsTest_SkipsTypeAssertionsForUpdateMethods(t *testing.T) {
	g := NewGenerator(Config{APIVersion: "v1alpha1"})
	schema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "enabled",
				Type: "boolean",
			},
			{
				Name: "type",
				Type: "string",
				Enum: []any{"oidc", "saml"},
			},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {
				Path: "github.com/Kong/sdk-konnect-go/models/components.CreateIdentityProvider",
			},
			"update": {
				Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateIdentityProvider",
			},
		},
	}

	content, err := g.generateSDKOpsTest("IdentityProviderRequest", schema, opsConfig)
	require.NoError(t, err)
	assert.Contains(t, content, `func TestIdentityProviderRequestAPISpec_ToCreateIdentityProvider`)
	assert.Contains(t, content, `Type: "test-value"`)
	assert.Contains(t, content, `require.Equal(t, "test-value", payload["type"])`)
	assert.Contains(t, content, `func TestIdentityProviderRequestAPISpec_ToUpdateIdentityProvider`)
	assert.Equal(t, 1, strings.Count(content, `Type: "test-value"`))
}

func TestParseSDKTypePath(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantImport string
		wantType   string
		wantErr    bool
	}{
		{
			name:       "valid SDK type path",
			input:      "github.com/Kong/sdk-konnect-go/models/components.CreatePortal",
			wantImport: "github.com/Kong/sdk-konnect-go/models/components",
			wantType:   "CreatePortal",
		},
		{
			name:       "valid path with nested packages",
			input:      "github.com/Kong/sdk-konnect-go/models/operations.ListPortals",
			wantImport: "github.com/Kong/sdk-konnect-go/models/operations",
			wantType:   "ListPortals",
		},
		{
			name:    "no dot separator",
			input:   "noDotAtAll",
			wantErr: true,
		},
		{
			name:    "leading dot",
			input:   ".CreatePortal",
			wantErr: true,
		},
		{
			name:    "trailing dot",
			input:   "github.com/Kong/sdk-konnect-go/models/components.",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			importPath, typeName, err := ParseSDKTypePath(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantImport, importPath)
			assert.Equal(t, tc.wantType, typeName)
		})
	}
}

func TestGenerateKonnectControllerSetupDispatcher(t *testing.T) {
	infos := []*WatchFileInfo{
		{
			Entity:         "Portal",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		},
		{
			Entity:         "IdentityProviderRequest",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		},
		{
			Entity:         "KonnectEventDataPlaneCertificate",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		},
	}

	file, err := GenerateKonnectControllerSetupDispatcher(infos)
	require.NoError(t, err)
	require.NotNil(t, file)

	assert.Equal(t, "zz_generated_konnect_controller_setup.go", file.Name)
	assert.Equal(t, "modules/manager", file.RelativeDir)
	assert.Contains(t, file.Content, "package manager")
	assert.Contains(t, file.Content, "func generatedControllersForKonnectEntities(")
	assert.Contains(t, file.Content, "newKonnectEntityController[konnectv1alpha1.IdentityProviderRequest](controllerFactory)")
	assert.Contains(t, file.Content, "newKonnectEntityController[konnectv1alpha1.KonnectEventDataPlaneCertificate](controllerFactory)")
	assert.Contains(t, file.Content, "newKonnectEntityController[konnectv1alpha1.Portal](controllerFactory)")

	idxIdentity := strings.Index(file.Content, "IdentityProviderRequest")
	idxEventCert := strings.Index(file.Content, "KonnectEventDataPlaneCertificate")
	idxPortal := strings.Index(file.Content, "Portal")
	assert.Less(t, idxIdentity, idxEventCert)
	assert.Less(t, idxEventCert, idxPortal)

	formatted, err := format.Source([]byte(file.Content))
	require.NoError(t, err)
	assert.Equal(t, string(formatted), file.Content)
}

func TestGenerateKonnectIndexOptionsDispatcher(t *testing.T) {
	infos := []*WatchFileInfo{
		{
			Entity:         "Portal",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		},
		{
			Entity:         "IdentityProviderRequest",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		},
		{
			Entity:         "KonnectEventDataPlaneCertificate",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		},
	}

	file, err := GenerateKonnectIndexOptionsDispatcher(infos)
	require.NoError(t, err)
	require.NotNil(t, file)

	assert.Equal(t, "zz_generated_konnect_index_options.go", file.Name)
	assert.Equal(t, "modules/manager", file.RelativeDir)
	assert.Contains(t, file.Content, "package manager")
	assert.Contains(t, file.Content, "func generatedIndexOptionsForKonnectEntities(")
	assert.Contains(t, file.Content, "index.OptionsForIdentityProviderRequest()")
	assert.Contains(t, file.Content, "index.OptionsForKonnectEventDataPlaneCertificate()")
	assert.Contains(t, file.Content, "index.OptionsForPortal()")

	idxIdentity := strings.Index(file.Content, "OptionsForIdentityProviderRequest")
	idxEventCert := strings.Index(file.Content, "OptionsForKonnectEventDataPlaneCertificate")
	idxPortal := strings.Index(file.Content, "OptionsForPortal")
	assert.Less(t, idxIdentity, idxEventCert)
	assert.Less(t, idxEventCert, idxPortal)

	formatted, err := format.Source([]byte(file.Content))
	require.NoError(t, err)
	assert.Equal(t, string(formatted), file.Content)
}

func TestGenerateKonnectConstraintsDispatcher(t *testing.T) {
	infos := []*WatchFileInfo{
		{
			Entity:         "Portal",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		},
		{
			Entity:         "IdentityProviderRequest",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		},
	}

	file, err := GenerateKonnectConstraintsDispatcher(infos)
	require.NoError(t, err)
	require.NotNil(t, file)

	assert.Equal(t, "zz_generated_supported_types.go", file.Name)
	assert.Equal(t, "controller/konnect/constraints", file.RelativeDir)
	assert.Contains(t, file.Content, "package constraints")
	assert.Contains(t, file.Content, "type SupportedGeneratedKonnectEntityType interface")
	assert.Contains(t, file.Content, "konnectv1alpha1.IdentityProviderRequest")
	assert.Contains(t, file.Content, "konnectv1alpha1.Portal")

	formatted, err := format.Source([]byte(file.Content))
	require.NoError(t, err)
	assert.Equal(t, string(formatted), file.Content)
}

func TestGenerateKonnectAPIAuthWatchDispatcher(t *testing.T) {
	infos := []*WatchFileInfo{
		{
			Entity:         "Portal",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
			IsRoot:         true,
		},
		{
			Entity:         "IdentityProviderRequest",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		},
		{
			Entity:         "KonnectEventGateway",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
			IsRoot:         true,
		},
	}

	file, err := GenerateKonnectAPIAuthWatchDispatcher(infos)
	require.NoError(t, err)
	require.NotNil(t, file)

	assert.Equal(t, "zz_generated_konnectapiauth_watch.go", file.Name)
	assert.Equal(t, "controller/konnect", file.RelativeDir)
	assert.Contains(t, file.Content, "generatedKonnectAPIAuthReferencingTypes")
	assert.Contains(t, file.Content, "&konnectv1alpha1.KonnectEventGateway{}")
	assert.Contains(t, file.Content, "&konnectv1alpha1.Portal{}")
	assert.NotContains(t, file.Content, "IdentityProviderRequest")

	formatted, err := format.Source([]byte(file.Content))
	require.NoError(t, err)
	assert.Equal(t, string(formatted), file.Content)
}

func TestGenerateSDKFactoryDispatcher(t *testing.T) {
	infos := []*SDKFactoryFileInfo{
		{
			Entity:                 "Portal",
			SDKInterfaceImportPath: "github.com/Kong/sdk-konnect-go",
			SDKInterfaceTypeName:   "PortalsSDK",
			SDKFieldName:           "Portals",
		},
		{
			Entity:                 "IdentityProviderRequest",
			SDKInterfaceImportPath: "github.com/Kong/sdk-konnect-go",
			SDKInterfaceTypeName:   "PortalAuthSettingsSDK",
			SDKFieldName:           "PortalAuthSettings",
		},
		{
			Entity:                 "KonnectEventDataPlaneCertificate",
			SDKInterfaceImportPath: "github.com/Kong/sdk-konnect-go",
			SDKInterfaceTypeName:   "EventGatewayDataPlaneCertificatesSDK",
			SDKFieldName:           "EventGatewayDataPlaneCertificates",
		},
	}

	file, err := GenerateSDKFactoryDispatcher(infos)
	require.NoError(t, err)
	require.NotNil(t, file)

	assert.Equal(t, "zz_generated_sdkfactory.go", file.Name)
	assert.Equal(t, "controller/konnect/ops/sdk", file.RelativeDir)
	assert.Contains(t, file.Content, "type GeneratedSDK interface {")
	assert.Contains(t, file.Content, "GetPortalAuthSettingsSDK() sdkkonnectgo.PortalAuthSettingsSDK")
	assert.Contains(t, file.Content, "GetEventGatewayDataPlaneCertificatesSDK() sdkkonnectgo.EventGatewayDataPlaneCertificatesSDK")
	assert.Contains(t, file.Content, "return w.sdk.PortalAuthSettings")
	assert.Contains(t, file.Content, "return w.sdk.EventGatewayDataPlaneCertificates")

	idxIdentity := strings.Index(file.Content, "GetPortalAuthSettingsSDK()")
	idxEventCert := strings.Index(file.Content, "GetEventGatewayDataPlaneCertificatesSDK()")
	idxPortal := strings.Index(file.Content, "GetPortalsSDK()")
	assert.Less(t, idxIdentity, idxEventCert)
	assert.Less(t, idxEventCert, idxPortal)
}

func TestGenerateMockSDKFactoryDispatcher(t *testing.T) {
	infos := []*SDKFactoryFileInfo{
		{
			Entity:                 "Portal",
			SDKInterfaceImportPath: "github.com/Kong/sdk-konnect-go",
			SDKInterfaceTypeName:   "PortalsSDK",
			SDKFieldName:           "Portals",
		},
		{
			Entity:                 "IdentityProviderRequest",
			SDKInterfaceImportPath: "github.com/Kong/sdk-konnect-go",
			SDKInterfaceTypeName:   "PortalAuthSettingsSDK",
			SDKFieldName:           "PortalAuthSettings",
		},
		{
			Entity:                 "KonnectEventDataPlaneCertificate",
			SDKInterfaceImportPath: "github.com/Kong/sdk-konnect-go",
			SDKInterfaceTypeName:   "EventGatewayDataPlaneCertificatesSDK",
			SDKFieldName:           "EventGatewayDataPlaneCertificates",
		},
		{
			Entity:                 "KonnectEventGateway",
			SDKInterfaceImportPath: "github.com/Kong/sdk-konnect-go",
			SDKInterfaceTypeName:   "EventGatewaysSDK",
			SDKFieldName:           "EventGateways",
		},
	}

	file, err := GenerateMockSDKFactoryDispatcher(infos)
	require.NoError(t, err)
	require.NotNil(t, file)

	assert.Equal(t, "zz_generated_sdkfactory_mock.go", file.Name)
	assert.Equal(t, "test/mocks/sdkmocks", file.RelativeDir)
	assert.Contains(t, file.Content, "package sdkmocks")
	assert.Contains(t, file.Content, "type generatedMockSDKWrapper struct {")
	assert.Contains(t, file.Content, "*mocks.MockEventGatewaysSDK")
	assert.Contains(t, file.Content, "*mocks.MockEventGatewayDataPlaneCertificatesSDK")
	assert.Contains(t, file.Content, "*mocks.MockPortalsSDK")
	assert.Contains(t, file.Content, "*mocks.MockPortalAuthSettingsSDK")
	assert.Contains(t, file.Content, "func newGeneratedMockSDKWrapper(t *testing.T) generatedMockSDKWrapper {")
	assert.Contains(t, file.Content, "mocks.NewMockEventGatewaysSDK(t)")
	assert.Contains(t, file.Content, "mocks.NewMockEventGatewayDataPlaneCertificatesSDK(t)")
	assert.Contains(t, file.Content, "func (m generatedMockSDKWrapper) GetPortalsSDK() sdkkonnectgo.PortalsSDK {")
	assert.Contains(t, file.Content, "return m.PortalAuthSettingsSDK")
	formatted, err := format.Source([]byte(file.Content))
	require.NoError(t, err)
	assert.Equal(t, string(formatted), file.Content)

	idxIdentity := strings.Index(file.Content, "*mocks.MockPortalAuthSettingsSDK")
	idxEvent := strings.Index(file.Content, "*mocks.MockEventGatewaysSDK")
	idxEventCert := strings.Index(file.Content, "*mocks.MockEventGatewayDataPlaneCertificatesSDK")
	idxPortal := strings.Index(file.Content, "*mocks.MockPortalsSDK")
	assert.Less(t, idxIdentity, idxEvent)
	assert.Less(t, idxEventCert, idxEvent)
	assert.Less(t, idxEventCert, idxPortal)
}

func TestGenerateSchemaTypes_MapWithValueTypes(t *testing.T) {
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
	})

	parsed := &parser.ParsedSpec{
		Schemas: map[string]*parser.Schema{
			"Labels": {
				Name:          "Labels",
				Description:   "Labels store metadata.",
				Type:          "object",
				MaxProperties: func() *int64 { v := int64(50); return &v }(),
				AdditionalProperties: &parser.Property{
					Type:      "string",
					MinLength: func() *int64 { v := int64(1); return &v }(),
					MaxLength: func() *int64 { v := int64(63); return &v }(),
					Pattern:   `^[a-z0-9A-Z]+$`,
				},
			},
			"LabelsUpdate": {
				Name:        "LabelsUpdate",
				Description: "LabelsUpdate store metadata.",
				Type:        "object",
				AdditionalProperties: &parser.Property{
					Type:      "string",
					MinLength: func() *int64 { v := int64(1); return &v }(),
					MaxLength: func() *int64 { v := int64(63); return &v }(),
					Pattern:   `^[a-z0-9A-Z]+$`,
				},
			},
		},
	}

	refs := map[string]bool{
		"Labels":       true,
		"LabelsUpdate": true,
	}

	content := g.generateSchemaTypes(refs, parsed, nil)

	// Labels should generate a value type with native markers, then a map type using it
	assert.Contains(t, content, "type LabelsValue string")
	assert.Contains(t, content, "type Labels map[string]LabelsValue")
	assert.Contains(t, content, "+kubebuilder:validation:MinLength=1")
	assert.Contains(t, content, "+kubebuilder:validation:MaxLength=63")
	assert.Contains(t, content, "+kubebuilder:validation:Pattern=`^[a-z0-9A-Z]+$`")

	// LabelsUpdate should also generate a value type
	assert.Contains(t, content, "type LabelsUpdateValue string")
	assert.Contains(t, content, "type LabelsUpdate map[string]LabelsUpdateValue")

	// No CEL XValidation rules or MaxProperties on the type (goes on the field)
	assert.NotContains(t, content, "XValidation")
	assert.NotContains(t, content, "MaxProperties")
}

func TestGenerateSchemaTypes_NoValueTypeForNonMapTypes(t *testing.T) {
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
	})

	parsed := &parser.ParsedSpec{
		Schemas: map[string]*parser.Schema{
			"GatewayName": {
				Name:        "GatewayName",
				Description: "The name of the Gateway.",
				Type:        "string",
			},
		},
	}

	refs := map[string]bool{
		"GatewayName": true,
	}

	content := g.generateSchemaTypes(refs, parsed, nil)

	assert.Contains(t, content, "type GatewayName string")
	assert.NotContains(t, content, "Value")
	assert.NotContains(t, content, "XValidation")
	assert.NotContains(t, content, "MaxProperties")
}

func TestGenerateRBAC(t *testing.T) {
	t.Run("gateway kinds use gateways plural", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "konnect.konghq.com",
			APIVersion: "v1alpha1",
		})

		assert.Equal(t, "gateways", g.resourceNameForKind("Gateway"))
		assert.Equal(t, "konnecteventgateways", g.resourceNameForKind("KonnectEventGateway"))
	})

	t.Run("single entity", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
		})

		content, err := g.generateRBAC([]string{"Portal"})
		require.NoError(t, err)
		assert.Contains(t, content, "Code generated by CRD generation pipeline. DO NOT EDIT.")
		assert.Contains(t, content, "package konnect")
		assert.Contains(t, content, "//+kubebuilder:rbac:groups=x-konnect.konghq.com,resources=portals,verbs=get;list;watch;update;patch")
		assert.Contains(t, content, "//+kubebuilder:rbac:groups=x-konnect.konghq.com,resources=portals/status,verbs=update;patch")
		assert.Contains(t, content, "//+kubebuilder:rbac:groups=x-konnect.konghq.com,resources=portals/finalizers,verbs=update;patch")
	})

	t.Run("multiple entities sorted", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "konnect.konghq.com",
			APIVersion: "v1alpha1",
		})

		content, err := g.generateRBAC([]string{"KonnectEventGateway", "SomeOtherEntity"})
		require.NoError(t, err)
		assert.Contains(t, content, "resources=konnecteventgateways,")
		assert.Contains(t, content, "resources=someotherentities,")
	})
}

func TestGenerate_NoRBACWithoutReconcilerConfig(t *testing.T) {
	g := NewGenerator(Config{
		APIGroup:             "x-konnect.konghq.com",
		APIVersion:           "v1alpha1",
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1",
		APIGroupPackageAlias: "xkonnectv1alpha1",
		// No ReconcilerConfig set — entity has no reconciler entry.
	})

	files, err := g.Generate(&parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"CreatePortal": {Name: "CreatePortal"},
		},
		Schemas: map[string]*parser.Schema{},
	})
	require.NoError(t, err)

	for _, f := range files {
		assert.NotContains(t, f.Name, "zz_generated_reconciler_generic_rbac_",
			"expected no RBAC file when entity has no reconciler config, got %q", f.Name)
	}
}

func TestGenerateReconcilerFiles_IncludesRBAC(t *testing.T) {
	g := NewGenerator(Config{
		APIGroup:             "x-konnect.konghq.com",
		APIVersion:           "v1alpha1",
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1",
		APIGroupPackageAlias: "xkonnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"Portal": {IsRoot: ptr(true)},
		},
	})

	files, err := g.generateReconcilerFiles(
		[]string{"Portal"},
		map[string]*parser.Schema{
			"Portal": {Name: "CreatePortal"},
		},
	)
	require.NoError(t, err)

	var rbacFile *GeneratedFile
	for i, f := range files {
		if f.Name == "zz_generated_reconciler_generic_rbac_xkonnectv1alpha1.go" {
			rbacFile = &files[i]
			break
		}
	}
	require.NotNil(t, rbacFile, "expected RBAC file in generated files")
	assert.Equal(t, "controller/konnect", rbacFile.RelativeDir)
	assert.Contains(t, rbacFile.Content, "//+kubebuilder:rbac:groups=x-konnect.konghq.com,resources=portals,verbs=get;list;watch;update;patch")
}

func TestGenerateOpsCreate_RootEntity(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"Portal": {IsRoot: ptr(true)},
		},
	})

	schema := &parser.Schema{
		OperationID:        "create-portal",
		Tags:               []string{"Portals"},
		SuccessResponseRef: "PortalResponse",
		RespIDIsPointer:    false,
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreatePortal"},
		},
	}

	file, info, err := g.generateOpsCreate("Portal", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, file)
	require.NotNil(t, info)

	assert.Equal(t, "zz_generated_ops_portal.go", file.Name)
	assert.Equal(t, "GetPortalsSDK", info.SDKGetter)

	// Root entity: no parent ID guard, direct SDK call.
	assert.NotContains(t, file.Content, "parentID")
	assert.NotContains(t, file.Content, "CantPerformOperationWithoutParentIDError")
	assert.Contains(t, file.Content, "sdk.CreatePortal(ctx, *req)")

	// Non-pointer ID: no pointer dereference.
	assert.Contains(t, file.Content, `resp.PortalResponse.ID == ""`)
	assert.Contains(t, file.Content, "obj.SetKonnectID(resp.PortalResponse.ID)")
	assert.NotContains(t, file.Content, "*resp.PortalResponse.ID")
}

func TestGenerateOpsCreate_NonRootEntity(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"IdentityProviderRequest": {IsRoot: ptr(false)},
		},
	})

	schema := &parser.Schema{
		OperationID:        "create-portal-identity-provider",
		Tags:               []string{"Portal Auth Settings"},
		SuccessResponseRef: "IdentityProvider",
		RespIDIsPointer:    true,
		Dependencies: []*parser.Dependency{
			{ParamName: "portalId", EntityName: "Portal", FieldName: "PortalRef"},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateIdentityProvider"},
		},
	}

	file, info, err := g.generateOpsCreate("IdentityProviderRequest", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, file)
	require.NotNil(t, info)

	assert.Equal(t, "zz_generated_ops_identityproviderrequest.go", file.Name)
	assert.Equal(t, "GetPortalAuthSettingsSDK", info.SDKGetter)

	// Non-root: parentID guard present.
	assert.Contains(t, file.Content, "parentID := obj.GetPortalID()")
	assert.Contains(t, file.Content, `CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "Portal", Op: CreateOp}`)

	// Scoped SDK call passes parentID.
	assert.Contains(t, file.Content, "sdk.CreatePortalIdentityProvider(ctx, parentID, *req)")
	assert.NotContains(t, file.Content, "sdk.CreatePortalIdentityProvider(ctx, *req)")

	// Pointer ID: dereference in nil check and SetKonnectID.
	assert.Contains(t, file.Content, "resp.IdentityProvider.ID == nil")
	assert.Contains(t, file.Content, "*resp.IdentityProvider.ID")
	assert.Contains(t, file.Content, "obj.SetKonnectID(*resp.IdentityProvider.ID)")
}

func TestGenerateOpsCreate_NonRootEntityWithParentTypeOverride(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"KonnectEventDataPlaneCertificate": {
				IsRoot:           ptr(false),
				ParentEntityType: "KonnectEventGateway",
			},
		},
	})

	schema := &parser.Schema{
		OperationID:          "create-event-gateway-data-plane-certificate",
		Tags:                 []string{"Event Gateway Data Plane Certificates"},
		SuccessResponseRef:   "EventGatewayDataPlaneCertificate",
		RespIDIsPointer:      false,
		CreateReqBodyPointer: true,
		Dependencies: []*parser.Dependency{
			{ParamName: "gatewayId", EntityName: "Gateway", FieldName: "GatewayRef"},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		RequireClient: true,
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateEventGatewayDataPlaneCertificateRequest"},
		},
	}

	file, info, err := g.generateOpsCreate("KonnectEventDataPlaneCertificate", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, file)
	require.NotNil(t, info)

	// parentIDGetter must use dep.EntityName ("Gateway") not ParentEntityType ("KonnectEventGateway"),
	// because crdFuncsTemplate emits GetGatewayID() based on the OAS path param.
	assert.Contains(t, file.Content, "parentID := obj.GetGatewayID()")

	// ParentEntityName uses ParentEntityType override in the error message.
	assert.Contains(t, file.Content, `Parent: "KonnectEventGateway"`)
	assert.True(t, info.NeedsClient)
	assert.Contains(t, file.Content, `"sigs.k8s.io/controller-runtime/pkg/client"`)
	assert.Contains(t, file.Content, "cl client.Client")
	assert.Contains(t, file.Content, "obj.ToCreateEventGatewayDataPlaneCertificateRequest(ctx, cl)")
	assert.Contains(t, file.Content, "sdk.CreateEventGatewayDataPlaneCertificate(ctx, parentID, req)")
}

func TestGenerateOpsCreate_NonRootEntityMissingDependency_ReturnsError(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"Orphan": {IsRoot: ptr(false)},
		},
	})

	schema := &parser.Schema{
		OperationID:        "create-orphan",
		Tags:               []string{"Orphans"},
		SuccessResponseRef: "Orphan",
		Dependencies:       nil, // No deps despite IsRoot=false.
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateOrphan"},
		},
	}

	_, _, err := g.generateOpsCreate("Orphan", schema, opsConfig)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no parent dependency")
}

func TestGenerateOpsCreateDispatcher(t *testing.T) {
	infos := []*OpsCreateFileInfo{
		{
			Entity:         "Portal",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
			SDKGetter:      "GetPortalsSDK",
		},
		{
			Entity:         "KonnectEventDataPlaneCertificate",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
			SDKGetter:      "GetEventGatewayDataPlaneCertificatesSDK",
			NeedsClient:    true,
		},
	}

	file, err := GenerateOpsCreateDispatcher(infos)
	require.NoError(t, err)
	require.NotNil(t, file)

	assert.Equal(t, "zz_generated_ops_create.go", file.Name)
	assert.Contains(t, file.Content, "func CreateGeneratedOps[")
	assert.Contains(t, file.Content, `"sigs.k8s.io/controller-runtime/pkg/client"`)
	assert.Contains(t, file.Content, "cl client.Client")
	assert.Contains(t, file.Content, "return createPortal(ctx, sdk.GetPortalsSDK(), ent)")
	assert.Contains(t, file.Content, "return createKonnectEventDataPlaneCertificate(ctx, cl, sdk.GetEventGatewayDataPlaneCertificatesSDK(), ent)")
	assert.NotContains(t, file.Content, "updatePortal")
	assert.NotContains(t, file.Content, "deletePortal")
}

func TestGenerateEntityOpsFile_UsesConfiguredSDKInterface(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"PortalPage": {IsRoot: ptr(false)},
		},
	})

	schema := &parser.Schema{
		OperationID:        "create-portal-page",
		Tags:               []string{"Pages"},
		SuccessResponseRef: "PortalPageResponse",
		Dependencies: []*parser.Dependency{
			{ParamName: "portalId", EntityName: "Portal"},
		},
		UpdateOperationID: "update-portal-page",
		UpdateTags:        []string{"Pages"},
		UpdatePathParams:  []string{"portalId", "pageId"},
		DeleteOperationID: "delete-portal-page",
		DeleteTags:        []string{"Pages"},
		DeletePathParams:  []string{"portalId", "pageId"},
		ListOperationID:   "list-portal-pages",
		ListTags:          []string{"Pages"},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreatePortalPageRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdatePortalPageRequest"},
			"delete": {},
		},
		SDK: &config.OpSDKConfig{
			Interface: "github.com/Kong/sdk-konnect-go.PortalPagesSDK",
			FieldName: "PortalPages",
		},
	}

	res, err := g.generateEntityOpsFile("PortalPage", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, res.File)
	require.NotNil(t, res.CreateInfo)
	require.NotNil(t, res.UpdateInfo)
	require.NotNil(t, res.DeleteInfo)
	require.NotNil(t, res.GetForUIDInfo)

	assert.Contains(t, res.File.Content, "sdk sdkkonnectgo.PortalPagesSDK")
	assert.NotContains(t, res.File.Content, "sdk sdkkonnectgo.PagesSDK")
	assert.Equal(t, "GetPortalPagesSDK", res.CreateInfo.SDKGetter)
	assert.Equal(t, "GetPortalPagesSDK", res.UpdateInfo.SDKGetter)
	assert.Equal(t, "GetPortalPagesSDK", res.DeleteInfo.SDKGetter)
	assert.Equal(t, "GetPortalPagesSDK", res.GetForUIDInfo.SDKGetter)
	assert.NotNil(t, res.SDKFactoryInfo)
	assert.Equal(t, "PortalPagesSDK", res.SDKFactoryInfo.SDKInterfaceTypeName)
	assert.Equal(t, "PortalPages", res.SDKFactoryInfo.SDKFieldName)
}

func TestGenerateEntityOpsFile_GetForUIDUsesUIDTagFilter(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"PortalPage": {IsRoot: ptr(false)},
		},
	})

	schema := &parser.Schema{
		ListOperationID: "list-portal-pages",
		ListTags:        []string{"Pages"},
		Dependencies: []*parser.Dependency{
			{ParamName: "portalId", EntityName: "Portal"},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		UseUIDTagFilter: true,
		SDK: &config.OpSDKConfig{
			Interface: "github.com/Kong/sdk-konnect-go.PortalPagesSDK",
			FieldName: "PortalPages",
		},
	}

	res, err := g.generateEntityOpsFile("PortalPage", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, res.File)
	require.NotNil(t, res.GetForUIDInfo)

	assert.Contains(t, res.File.Content, "Tags: new(UIDLabelForObject(obj))")
	assert.Contains(t, res.File.Content, "PortalID: parentID")
	assert.Contains(t, res.File.Content, "switch id := any(entry.GetID()).(type)")
	assert.NotContains(t, res.File.Content, "entry.GetLabels()[KubernetesUIDLabelKey]")
}

func TestGenerateEntityOpsFile_GetForUIDUsesUIDTagFilter_Golden(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/configuration/v1alpha1",
		APIGroupPackageAlias: "configurationv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"KongService": {IsRoot: ptr(false), ParentEntityType: "KonnectGatewayControlPlane"},
		},
	})

	schema := &parser.Schema{
		ListOperationID: "list-service",
		ListTags:        []string{"Services"},
		Dependencies: []*parser.Dependency{
			{ParamName: "controlPlaneId", EntityName: "ControlPlane"},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		UseUIDTagFilter: true,
		SDK: &config.OpSDKConfig{
			Interface: "github.com/Kong/sdk-konnect-go.ServicesSDK",
			FieldName: "Services",
		},
	}

	res, err := g.generateEntityOpsFile("KongService", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, res.File)

	got, err := format.Source([]byte(res.File.Content))
	require.NoError(t, err)

	want, err := os.ReadFile(filepath.Join("testdata", "zz_generated_ops_kongservice_getforuid_uid_tag_filter.golden.go"))
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}

func TestGenerateEntityOpsFile_GetForUIDUsesConfiguredMatchFields(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"KonnectEventDataPlaneCertificate": {IsRoot: new(false), ParentEntityType: "KonnectEventGateway"},
		},
	})

	schema := &parser.Schema{
		ListOperationID:        "list-event-gateway-data-plane-certificates",
		ListTags:               []string{"EventGatewayDataPlaneCertificates"},
		ListSuccessResponseRef: "ListEventGatewayDataPlaneCertificatesResponse",
		Dependencies: []*parser.Dependency{
			{ParamName: "gatewayId", EntityName: "KonnectEventGateway"},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		GetForUID: &config.GetForUIDConfig{
			MatchFields: []config.GetForUIDMatchField{
				{
					ObjectField:   "Spec.APISpec.Certificate",
					ResponseField: "Certificate",
				},
				{
					ObjectField:   "Spec.APISpec.Name",
					ResponseField: "Name",
				},
				{
					ObjectField:   "Spec.APISpec.Description",
					ResponseField: "Description",
				},
			},
		},
		SDK: &config.OpSDKConfig{
			Interface: "github.com/Kong/sdk-konnect-go.EventGatewayDataPlaneCertificatesSDK",
			FieldName: "EventGatewayDataPlaneCertificates",
		},
	}

	res, err := g.generateEntityOpsFile("KonnectEventDataPlaneCertificate", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, res.File)
	require.NotNil(t, res.GetForUIDInfo)

	assert.Contains(t, res.File.Content, "if !matchStringField(obj.Spec.APISpec.Certificate, entry.Certificate)")
	assert.Contains(t, res.File.Content, "if !matchStringField(obj.Spec.APISpec.Name, entry.Name)")
	assert.Contains(t, res.File.Content, "if !matchStringField(obj.Spec.APISpec.Description, entry.Description)")
	assert.Contains(t, res.File.Content, "switch id := any(entry.GetID()).(type)")
	assert.NotContains(t, res.File.Content, "entry.GetLabels()[KubernetesUIDLabelKey]")
	assert.NotContains(t, res.File.Content, "entry.GetName()")
}

func TestGenerateEntityOpsFile_ManualGetForUIDStillEmitsDispatcherInfo(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"KonnectEventDataPlaneCertificate": {IsRoot: ptr(false), ParentEntityType: "KonnectEventGateway"},
		},
		ManualGetForUIDEntities: map[string]bool{
			"KonnectEventDataPlaneCertificate": true,
		},
	})

	schema := &parser.Schema{
		Dependencies: []*parser.Dependency{
			{ParamName: "gatewayId", EntityName: "KonnectEventGateway"},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		SkipGetForUID: true,
		SDK: &config.OpSDKConfig{
			Interface: "github.com/Kong/sdk-konnect-go.EventGatewayDataPlaneCertificatesSDK",
			FieldName: "EventGatewayDataPlaneCertificates",
		},
	}

	res, err := g.generateEntityOpsFile("KonnectEventDataPlaneCertificate", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, res.GetForUIDInfo)
	assert.Equal(t, "GetEventGatewayDataPlaneCertificatesSDK", res.GetForUIDInfo.SDKGetter)
}

// TestGenerateEntityOpsFile_GetForUIDWithNoMatchStrategy verifies that
// sdkkonnectops is NOT imported when the getForUID function falls into the
// fallback else-branch (no labels, no name, no match fields, no UID tag
// filter). The else-branch emits no SDK list call and therefore does not
// reference the operations package.
func TestGenerateEntityOpsFile_GetForUIDWithNoMatchStrategy(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"PortalEmailConfig": {IsRoot: ptr(false)},
		},
	})

	// Schema has a list operation but no labels/name/matchFields on the response,
	// so the generator falls into the else-branch of opsGetForUIDFuncTemplate.
	// No update or delete ops are configured so the only source of an sdkkonnectops
	// import would be the getForUID list call — which is absent in the else-branch.
	schema := &parser.Schema{
		OperationID:        "create-portal-email-config",
		Tags:               []string{"Portal Email Config"},
		SuccessResponseRef: "PortalEmailConfig",
		Dependencies: []*parser.Dependency{
			{ParamName: "portalId", EntityName: "Portal"},
		},
		ListOperationID: "list-portal-email-configs",
		ListTags:        []string{"Portal Email Config"},
		// No Properties with labels or name → HasLabels=false, HasName=false.
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreatePortalEmailConfig"},
		},
		SDK: &config.OpSDKConfig{
			Interface: "github.com/Kong/sdk-konnect-go.PortalEmailsSDK",
			FieldName: "PortalEmails",
		},
	}

	res, err := g.generateEntityOpsFile("PortalEmailConfig", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, res.File)
	require.NotNil(t, res.GetForUIDInfo)

	// The else-branch emits a TODO comment and early return — no SDK list call.
	assert.Contains(t, res.File.Content, "EntityWithMatchingUIDNotFoundError{Entity: obj}")
	assert.NotContains(t, res.File.Content, "sdk.ListPortalEmailConfigs")

	// sdkkonnectops must NOT be imported: getForUID's else-branch emits no SDK
	// list call and therefore never references the operations package.
	assert.NotContains(t, res.File.Content, `sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"`)
}

// TestGenerateEntityOpsFile_ParentScopedSingleton verifies correct code
// generation for resources like PortalEmailConfig whose PATCH and DELETE paths
// contain only the parent ID (no entity-specific ID). The generated update call
// must use the parent ID, and the generated delete call must omit the entity ID.
func TestGenerateEntityOpsFile_ParentScopedSingleton(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"PortalEmailConfig": {IsRoot: ptr(false)},
		},
	})

	// PATCH /portals/{portalId}/email-config — only parent ID, no entity ID.
	// DELETE /portals/{portalId}/email-config — only parent ID, no entity ID.
	schema := &parser.Schema{
		OperationID:        "create-portal-email-config",
		Tags:               []string{"Portal Email Config"},
		SuccessResponseRef: "PortalEmailConfig",
		Dependencies: []*parser.Dependency{
			{ParamName: "portalId", EntityName: "Portal"},
		},
		UpdateOperationID: "update-portal-email-config",
		UpdateTags:        []string{"Portal Email Config"},
		UpdatePathParams:  []string{"portalId"}, // singleton: only parent ID
		DeleteOperationID: "delete-portal-email-config",
		DeleteTags:        []string{"Portal Email Config"},
		DeletePathParams:  []string{"portalId"}, // singleton: only parent ID
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.PostPortalEmailConfig"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.PatchPortalEmailConfig"},
			"delete": {},
		},
		SDK: &config.OpSDKConfig{
			Interface: "github.com/Kong/sdk-konnect-go.PortalEmailsSDK",
			FieldName: "PortalEmails",
		},
	}

	res, err := g.generateEntityOpsFile("PortalEmailConfig", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, res.File)

	content := res.File.Content

	// Update: parent ID passed, no entity ID local variable or argument.
	assert.Contains(t, content, "sdk.UpdatePortalEmailConfig(ctx, parentID,")
	assert.NotContains(t, content, "sdk.UpdatePortalEmailConfig(ctx, id,")

	// Delete: parent ID only, no entity ID.
	assert.Contains(t, content, "sdk.DeletePortalEmailConfig(ctx, parentID)")
	assert.NotContains(t, content, "sdk.DeletePortalEmailConfig(ctx, parentID, id)")

	// No entity ID variable in the generated delete or update functions.
	// (id would be declared but unused, causing a compile error.)
	assert.NotContains(t, content, "id := obj.GetKonnectStatus().GetKonnectID()")
}

func TestGenerateOpsUpdate_RootEntity(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"Portal": {IsRoot: ptr(true)},
		},
	})

	schema := &parser.Schema{
		OperationID:        "create-portal",
		Tags:               []string{"Portals"},
		SuccessResponseRef: "PortalResponse",
		// PATCH /v3/portals/{portalId} — 1 path param → positional call.
		UpdateOperationID: "update-portal",
		UpdateTags:        []string{"Portals"},
		UpdatePathParams:  []string{"portalId"},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreatePortal"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdatePortal"},
		},
	}

	file, info, err := g.generateOpsUpdate("Portal", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, file)
	require.NotNil(t, info)

	assert.Equal(t, "zz_generated_ops_portal.go", file.Name)
	assert.Equal(t, "GetPortalsSDK", info.SDKGetter)

	// Contains both create and update functions.
	assert.Contains(t, file.Content, "func createPortal(")
	assert.Contains(t, file.Content, "func updatePortal(")

	// Root: no parent guard.
	assert.NotContains(t, file.Content, "CantPerformOperationWithoutParentIDError")

	// Positional call: sdk.UpdatePortal(ctx, id, *req).
	assert.Contains(t, file.Content, "sdk.UpdatePortal(ctx, id, *req)")

	// Uses GetKonnectID for the entity ID.
	assert.Contains(t, file.Content, "obj.GetKonnectStatus().GetKonnectID()")

	// UpdateOp constant in error wrapping.
	assert.Contains(t, file.Content, "wrapErrIfKonnectOpFailed(err, UpdateOp, obj)")
}

func TestGenerateOpsUpdate_NonRootEntity(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"IdentityProviderRequest": {IsRoot: ptr(false)},
		},
	})

	schema := &parser.Schema{
		OperationID:        "create-portal-identity-provider",
		Tags:               []string{"Portal Auth Settings"},
		SuccessResponseRef: "IdentityProvider",
		RespIDIsPointer:    true,
		Dependencies: []*parser.Dependency{
			{ParamName: "portalId", EntityName: "Portal"},
		},
		// PATCH /v3/portals/{portalId}/identity-providers/{id} — 2 params → wrapped struct.
		UpdateOperationID: "update-portal-identity-provider",
		UpdateTags:        []string{"Portal Auth Settings"},
		UpdatePathParams:  []string{"portalId", "id"},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateIdentityProvider"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateIdentityProvider"},
		},
	}

	file, info, err := g.generateOpsUpdate("IdentityProviderRequest", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, file)
	require.NotNil(t, info)

	assert.Equal(t, "GetPortalAuthSettingsSDK", info.SDKGetter)

	// Non-root: parent guard with UpdateOp.
	assert.Contains(t, file.Content, "parentID := obj.GetPortalID()")
	assert.Contains(t, file.Content, `CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "Portal", Op: UpdateOp}`)

	// Wrapped-struct call.
	assert.Contains(t, file.Content, "sdkkonnectops.UpdatePortalIdentityProviderRequest{")
	assert.Contains(t, file.Content, "PortalID: parentID,")
	assert.Contains(t, file.Content, "ID: id,")
	assert.Contains(t, file.Content, "UpdateIdentityProvider: *req,")

	// sdkkonnectops import present.
	assert.Contains(t, file.Content, `sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"`)
}

func TestGenerateOpsUpdate_NonRootEntityWithParentTypeOverride(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"KonnectEventDataPlaneCertificate": {
				IsRoot:           ptr(false),
				ParentEntityType: "KonnectEventGateway",
			},
		},
	})

	schema := &parser.Schema{
		OperationID:        "create-event-gateway-data-plane-certificate",
		Tags:               []string{"Event Gateway Data Plane Certificates"},
		SuccessResponseRef: "EventGatewayDataPlaneCertificate",
		Dependencies: []*parser.Dependency{
			{ParamName: "gatewayId", EntityName: "Gateway"},
		},
		// PATCH /v1/event-gateways/{gatewayId}/data-plane-certificates/{certificateId}.
		UpdateOperationID:    "update-event-gateway-data-plane-certificate",
		UpdateTags:           []string{"Event Gateway Data Plane Certificates"},
		UpdatePathParams:     []string{"gatewayId", "certificateId"},
		UpdateReqBodyPointer: true,
	}
	opsConfig := &config.EntityOpsConfig{
		RequireClient: true,
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateEventGatewayDataPlaneCertificateRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateEventGatewayDataPlaneCertificateRequest"},
		},
	}

	file, _, err := g.generateOpsUpdate("KonnectEventDataPlaneCertificate", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, file)

	// parentIDGetter uses dep.EntityName ("Gateway"), not ParentEntityType override.
	assert.Contains(t, file.Content, "parentID := obj.GetGatewayID()")

	// Error label uses ParentEntityType override.
	assert.Contains(t, file.Content, `Parent: "KonnectEventGateway"`)

	// Struct fields derived from path params: gatewayId → GatewayID, certificateId → CertificateID.
	assert.Contains(t, file.Content, "GatewayID: parentID,")
	assert.Contains(t, file.Content, "CertificateID: id,")
	assert.Contains(t, file.Content, "cl client.Client")
	assert.Contains(t, file.Content, "obj.ToUpdateEventGatewayDataPlaneCertificateRequest(ctx, cl)")
	assert.Contains(t, file.Content, "UpdateEventGatewayDataPlaneCertificateRequest: req,")
}

func TestGenerateSDKOps_ClientRequestMethodsResolveSecretRef(t *testing.T) {
	g := NewGenerator(Config{
		APIVersion:        "v1alpha1",
		SecretRefEntities: map[string]bool{"KonnectEventDataPlaneCertificate": true},
	})
	schema := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "certificate", Type: "string"},
			{Name: "description", Type: "string"},
			{Name: "name", Type: "string"},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		RequireClient: true,
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateEventGatewayDataPlaneCertificateRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateEventGatewayDataPlaneCertificateRequest"},
		},
	}

	content, err := g.generateSDKOps("KonnectEventDataPlaneCertificate", schema, opsConfig)
	require.NoError(t, err)
	assert.Contains(t, content, `"context"`)
	assert.Contains(t, content, `corev1 "k8s.io/api/core/v1"`)
	assert.Contains(t, content, `"sigs.k8s.io/controller-runtime/pkg/client"`)
	assert.Contains(t, content, "func (obj *KonnectEventDataPlaneCertificate) sdkOpsAPISpec(ctx context.Context, cl client.Client)")
	assert.Contains(t, content, "if obj.Spec.Type != nil && *obj.Spec.Type == SensitiveDataSourceTypeSecretRef {")
	assert.Contains(t, content, `secretBytes, ok := secret.Data["tls.crt"]`)
	assert.Contains(t, content, "apiSpec.Certificate = string(secretBytes)")
	assert.Contains(t, content, "func (obj *KonnectEventDataPlaneCertificate) ToCreateEventGatewayDataPlaneCertificateRequest(ctx context.Context, cl client.Client)")
	assert.Contains(t, content, "return spec.ToCreateEventGatewayDataPlaneCertificateRequest()")
	assert.Contains(t, content, "func (obj *KonnectEventDataPlaneCertificate) ToUpdateEventGatewayDataPlaneCertificateRequest(ctx context.Context, cl client.Client)")
	assert.Contains(t, content, "return spec.ToUpdateEventGatewayDataPlaneCertificateRequest()")
}

func TestGenerateOpsUpdate_PointerBody(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"Foo": {IsRoot: ptr(true)},
		},
	})

	schema := &parser.Schema{
		OperationID:          "create-foo",
		Tags:                 []string{"Foos"},
		SuccessResponseRef:   "Foo",
		UpdateOperationID:    "update-foo",
		UpdateTags:           []string{"Foos"},
		UpdatePathParams:     []string{"fooId"},
		UpdateReqBodyPointer: true,
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateFoo"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateFoo"},
		},
	}

	file, _, err := g.generateOpsUpdate("Foo", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, file)

	// Pointer body: pass req (pointer) not *req.
	assert.Contains(t, file.Content, "sdk.UpdateFoo(ctx, id, req)")
	assert.NotContains(t, file.Content, "sdk.UpdateFoo(ctx, id, *req)")
}

func TestGenerateOpsUpdate_NoUpdateOp_Skipped(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"Portal": {IsRoot: ptr(true)},
		},
	})

	schema := &parser.Schema{
		OperationID:        "create-portal",
		Tags:               []string{"Portals"},
		SuccessResponseRef: "PortalResponse",
		// No UpdateOperationID — no PATCH found.
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreatePortal"},
			// No "update" key.
		},
	}

	file, info, err := g.generateOpsUpdate("Portal", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, file) // file emitted for create
	require.Nil(t, info)    // no update info → not in dispatcher

	// File contains create but no update.
	assert.Contains(t, file.Content, "func createPortal(")
	assert.NotContains(t, file.Content, "func updatePortal(")
}

func TestGenerateOpsUpdateDispatcher(t *testing.T) {
	infos := []*OpsUpdateFileInfo{
		{
			Entity:         "Portal",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
			SDKGetter:      "GetPortalsSDK",
		},
		{
			Entity:         "IdentityProviderRequest",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
			SDKGetter:      "GetPortalAuthSettingsSDK",
		},
		{
			Entity:         "KonnectEventDataPlaneCertificate",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
			SDKGetter:      "GetEventGatewayDataPlaneCertificatesSDK",
			NeedsClient:    true,
		},
	}

	file, err := GenerateOpsUpdateDispatcher(infos)
	require.NoError(t, err)
	require.NotNil(t, file)

	assert.Equal(t, "zz_generated_ops_update.go", file.Name)
	assert.Contains(t, file.Content, "func UpdateGeneratedOps[")
	assert.Contains(t, file.Content, `"sigs.k8s.io/controller-runtime/pkg/client"`)
	assert.Contains(t, file.Content, "cl client.Client")

	// Alphabetical ordering of case labels.
	idxIdentity := strings.Index(file.Content, "case *konnectv1alpha1.IdentityProviderRequest:")
	idxKonnectEvent := strings.Index(file.Content, "case *konnectv1alpha1.KonnectEventDataPlaneCertificate:")
	idxPortal := strings.Index(file.Content, "case *konnectv1alpha1.Portal:")
	assert.Less(t, idxIdentity, idxPortal, "cases should be alphabetically sorted")
	assert.Less(t, idxKonnectEvent, idxPortal, "cases should be alphabetically sorted")

	// Dispatcher calls updateX not createX.
	assert.Contains(t, file.Content, "return updatePortal(ctx, sdk.GetPortalsSDK(), ent)")
	assert.Contains(t, file.Content, "return updateIdentityProviderRequest(ctx, sdk.GetPortalAuthSettingsSDK(), ent)")
	assert.Contains(t, file.Content, "return updateKonnectEventDataPlaneCertificate(ctx, cl, sdk.GetEventGatewayDataPlaneCertificatesSDK(), ent)")
	assert.NotContains(t, file.Content, "createPortal")
}

func TestGenerateOpsDelete_RootEntity(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"Portal": {IsRoot: ptr(true)},
		},
	})

	schema := &parser.Schema{
		OperationID:        "create-portal",
		Tags:               []string{"Portals"},
		SuccessResponseRef: "PortalResponse",
		UpdateOperationID:  "update-portal",
		UpdateTags:         []string{"Portals"},
		UpdatePathParams:   []string{"portalId"},
		// DELETE /v3/portals/{portalId} — 1 path param, 1 query param (force).
		DeleteOperationID:     "delete-portal",
		DeleteTags:            []string{"Portals"},
		DeletePathParams:      []string{"portalId"},
		DeleteQueryParamCount: 1,
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreatePortal"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdatePortal"},
			"delete": {},
		},
	}

	file, info, err := g.generateOpsDelete("Portal", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, file)
	require.NotNil(t, info)

	assert.Equal(t, "zz_generated_ops_portal.go", file.Name)
	assert.Equal(t, "GetPortalsSDK", info.SDKGetter)

	// File contains create, update, and delete functions.
	assert.Contains(t, file.Content, "func createPortal(")
	assert.Contains(t, file.Content, "func updatePortal(")
	assert.Contains(t, file.Content, "func deletePortal(")

	// Root: no parent guard.
	assert.NotContains(t, file.Content, "CantPerformOperationWithoutParentIDError")

	// Positional call with nil for the force query param.
	assert.Contains(t, file.Content, "sdk.DeletePortal(ctx, id, nil)")

	// Uses GetKonnectID for the entity ID.
	assert.Contains(t, file.Content, "obj.GetKonnectStatus().GetKonnectID()")

	// DeleteOp constant in error wrapping.
	assert.Contains(t, file.Content, "wrapErrIfKonnectOpFailed(err, DeleteOp, obj)")
}

func TestGenerateOpsDelete_NonRootEntity(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"IdentityProviderRequest": {IsRoot: ptr(false)},
		},
	})

	schema := &parser.Schema{
		OperationID:        "create-portal-identity-provider",
		Tags:               []string{"Portal Auth Settings"},
		SuccessResponseRef: "IdentityProvider",
		RespIDIsPointer:    true,
		Dependencies: []*parser.Dependency{
			{ParamName: "portalId", EntityName: "Portal"},
		},
		UpdateOperationID: "update-portal-identity-provider",
		UpdateTags:        []string{"Portal Auth Settings"},
		UpdatePathParams:  []string{"portalId", "id"},
		// DELETE /v3/portals/{portalId}/identity-providers/{id} — 2 path params, 0 query params.
		DeleteOperationID: "delete-portal-identity-provider",
		DeleteTags:        []string{"Portal Auth Settings"},
		DeletePathParams:  []string{"portalId", "id"},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateIdentityProvider"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateIdentityProvider"},
			"delete": {},
		},
	}

	file, info, err := g.generateOpsDelete("IdentityProviderRequest", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, file)
	require.NotNil(t, info)

	assert.Equal(t, "GetPortalAuthSettingsSDK", info.SDKGetter)

	// Non-root: parent guard with DeleteOp.
	assert.Contains(t, file.Content, "parentID := obj.GetPortalID()")
	assert.Contains(t, file.Content, `CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "Portal", Op: DeleteOp}`)

	// Positional call: sdk.DeletePortalIdentityProvider(ctx, parentID, id).
	assert.Contains(t, file.Content, "sdk.DeletePortalIdentityProvider(ctx, parentID, id)")

	// Delete does not reference sdkkonnectops directly (no wrapped struct for delete).
	assert.NotContains(t, file.Content, "sdkkonnectops.Delete")
}

func TestGenerateOpsDelete_NonRootEntityWithParentTypeOverride(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"KonnectEventDataPlaneCertificate": {
				IsRoot:           ptr(false),
				ParentEntityType: "KonnectEventGateway",
			},
		},
	})

	schema := &parser.Schema{
		OperationID:        "create-event-gateway-data-plane-certificate",
		Tags:               []string{"Event Gateway Data Plane Certificates"},
		SuccessResponseRef: "EventGatewayDataPlaneCertificate",
		Dependencies: []*parser.Dependency{
			{ParamName: "gatewayId", EntityName: "Gateway"},
		},
		UpdateOperationID: "update-event-gateway-data-plane-certificate",
		UpdateTags:        []string{"Event Gateway Data Plane Certificates"},
		UpdatePathParams:  []string{"gatewayId", "certificateId"},
		// DELETE /v1/event-gateways/{gatewayId}/data-plane-certificates/{certificateId}.
		DeleteOperationID: "delete-event-gateway-data-plane-certificate",
		DeleteTags:        []string{"Event Gateway Data Plane Certificates"},
		DeletePathParams:  []string{"gatewayId", "certificateId"},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateEventGatewayDataPlaneCertificateRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateEventGatewayDataPlaneCertificateRequest"},
			"delete": {},
		},
	}

	file, _, err := g.generateOpsDelete("KonnectEventDataPlaneCertificate", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, file)

	// parentIDGetter uses dep.EntityName ("Gateway"), not ParentEntityType override.
	assert.Contains(t, file.Content, "parentID := obj.GetGatewayID()")

	// Error label uses ParentEntityType override.
	assert.Contains(t, file.Content, `Parent: "KonnectEventGateway"`)

	// Positional call: sdk.DeleteEventGatewayDataPlaneCertificate(ctx, parentID, id).
	assert.Contains(t, file.Content, "sdk.DeleteEventGatewayDataPlaneCertificate(ctx, parentID, id)")
}

func TestGenerateOpsDelete_NoDeleteOp_Skipped(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"Portal": {IsRoot: ptr(true)},
		},
	})

	schema := &parser.Schema{
		OperationID:        "create-portal",
		Tags:               []string{"Portals"},
		SuccessResponseRef: "PortalResponse",
		UpdateOperationID:  "update-portal",
		UpdateTags:         []string{"Portals"},
		UpdatePathParams:   []string{"portalId"},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreatePortal"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdatePortal"},
			// No "delete" key.
		},
	}

	file, info, err := g.generateOpsDelete("Portal", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, file) // file emitted for create+update
	require.Nil(t, info)    // no delete info → not in dispatcher

	// File contains create and update but no delete.
	assert.Contains(t, file.Content, "func createPortal(")
	assert.Contains(t, file.Content, "func updatePortal(")
	assert.NotContains(t, file.Content, "func deletePortal(")
}

func TestGenerateOpsDeleteDispatcher(t *testing.T) {
	infos := []*OpsDeleteFileInfo{
		{
			Entity:         "Portal",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
			SDKGetter:      "GetPortalsSDK",
		},
		{
			Entity:         "IdentityProviderRequest",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
			SDKGetter:      "GetPortalAuthSettingsSDK",
		},
	}

	file, err := GenerateOpsDeleteDispatcher(infos)
	require.NoError(t, err)
	require.NotNil(t, file)

	assert.Equal(t, "zz_generated_ops_delete.go", file.Name)
	assert.Contains(t, file.Content, "func DeleteGeneratedOps[")

	// Alphabetical ordering: IdentityProviderRequest before Portal.
	idxIdentity := strings.Index(file.Content, "IdentityProviderRequest")
	idxPortal := strings.Index(file.Content, "Portal")
	assert.Less(t, idxIdentity, idxPortal, "cases should be alphabetically sorted")

	// Dispatcher calls deleteX not createX or updateX.
	assert.Contains(t, file.Content, "return deletePortal(ctx, sdk.GetPortalsSDK(), ent)")
	assert.Contains(t, file.Content, "return deleteIdentityProviderRequest(ctx, sdk.GetPortalAuthSettingsSDK(), ent)")
	assert.NotContains(t, file.Content, "createPortal")
	assert.NotContains(t, file.Content, "updatePortal")
}

func TestPathParamToFieldName(t *testing.T) {
	tests := []struct {
		param string
		want  string
	}{
		{"portalId", "PortalID"},
		{"id", "ID"},
		{"gatewayId", "GatewayID"},
		{"certificateId", "CertificateID"},
		{"fooId", "FooID"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, pathParamToFieldName(tc.param), "param=%q", tc.param)
	}
}

func TestGenerateSchemaTypes_ScalarOneOfEmitsIntOrString(t *testing.T) {
	g := NewGenerator(Config{APIVersion: "v1alpha1"})
	parsed := &parser.ParsedSpec{
		Schemas: map[string]*parser.Schema{
			"EventGatewayListenerPort": {
				Name:        "EventGatewayListenerPort",
				Description: "A port or a range of ports.",
				OneOf: []*parser.Property{
					{Name: "variant0", Type: "string"},
					{Name: "variant1", Type: "integer"},
				},
			},
		},
	}

	content := g.generateSchemaTypes(map[string]bool{"EventGatewayListenerPort": true}, parsed, nil)

	assert.Contains(t, content, `type EventGatewayListenerPort = intstr.IntOrString`)
	assert.Contains(t, content, `+kubebuilder:validation:XIntOrString`)
	assert.Contains(t, content, `intstr "k8s.io/apimachinery/pkg/util/intstr"`)
	assert.NotContains(t, content, `map[string]string`)
}

func TestGenerateSchemaTypes_ArrayItemsRefEmitsTypedSlice(t *testing.T) {
	g := NewGenerator(Config{APIVersion: "v1alpha1"})
	parsed := &parser.ParsedSpec{
		Schemas: map[string]*parser.Schema{
			"EventGatewayListenerPorts": {
				Name:        "EventGatewayListenerPorts",
				Description: "Which port or ports to listen on.",
				Type:        "array",
				Items:       &parser.Property{RefName: "EventGatewayListenerPort"},
			},
		},
	}

	content := g.generateSchemaTypes(map[string]bool{"EventGatewayListenerPorts": true}, parsed, nil)

	assert.Contains(t, content, `type EventGatewayListenerPorts []EventGatewayListenerPort`)
	assert.NotContains(t, content, `[]any`)
}

func TestGenerateSchemaTypes_NonScalarOneOfFallsBack(t *testing.T) {
	// A oneOf where variants have RefName must NOT be consumed by the scalar arm;
	// it falls through to default (map[string]string) until a dedicated follow-up
	// handles ref-bearing root oneOf.
	g := NewGenerator(Config{APIVersion: "v1alpha1"})
	parsed := &parser.ParsedSpec{
		Schemas: map[string]*parser.Schema{
			"AuthMethod": {
				Name: "AuthMethod",
				OneOf: []*parser.Property{
					{Name: "OIDCConfig", RefName: "OIDCConfig"},
					{Name: "SAMLConfig", RefName: "SAMLConfig"},
				},
			},
		},
	}

	content := g.generateSchemaTypes(map[string]bool{"AuthMethod": true}, parsed, nil)

	assert.NotContains(t, content, `intstr.IntOrString`)
	assert.NotContains(t, content, `XIntOrString`)
}

func TestJSONName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Single segment stays lowercase.
		{"enabled", "enabled"},
		{"id", "id"},
		// First segment always lowercase, even if an acronym.
		{"rbac_enabled", "rbacEnabled"},
		{"id_token", "idToken"},
		{"dns_label", "dnsLabel"},
		{"tls_server", "tlsServer"},
		// Subsequent segments: acronym caps applied.
		{"organization_id", "organizationID"},
		{"default_api_visibility", "defaultAPIVisibility"},
		{"parent_page_id_ref", "parentPageIDRef"},
		{"default_application_auth_strategy_id_ref", "defaultApplicationAuthStrategyIDRef"},
		{"event_gateway_listener_ref", "eventGatewayListenerRef"},
		// Multi-word plain fields.
		{"display_name", "displayName"},
		{"bootstrap_servers", "bootstrapServers"},
		{"min_runtime_version", "minRuntimeVersion"},
		// Discriminator values.
		{"sasl_plain", "saslPlain"},
		{"sasl_scram", "saslScram"},
		{"forward_to_virtual_cluster", "forwardToVirtualCluster"},
		// Already-camelCase input is idempotent (no underscores).
		{"displayName", "displayName"},
		// Empty string.
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := jsonName(tt.input)
			if got != tt.want {
				t.Errorf("jsonName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGenerateCRDType_Categories(t *testing.T) {
	schema := &parser.Schema{Name: "CreatePortal"}

	t.Run("type with categories emits categories marker", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "konnect.konghq.com",
			APIVersion: "v1alpha1",
			Categories: []string{"konnect", "kong"},
		})
		content, err := g.generateCRDType("CreatePortal", schema)
		require.NoError(t, err)
		assert.Contains(t, content, "+kubebuilder:resource:scope=Namespaced,categories=konnect;kong")
	})

	t.Run("non-root type also receives categories marker", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "konnect.konghq.com",
			APIVersion: "v1alpha1",
			ReconcilerConfig: map[string]*config.ReconcilerConfig{
				"Portal": {IsRoot: new(false)},
			},
			Categories: []string{"konnect", "kong"},
		})
		content, err := g.generateCRDType("CreatePortal", schema)
		require.NoError(t, err)
		assert.Contains(t, content, "+kubebuilder:resource:scope=Namespaced,categories=konnect;kong")
	})

	t.Run("empty categories omits categories from marker", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "konnect.konghq.com",
			APIVersion: "v1alpha1",
		})
		content, err := g.generateCRDType("CreatePortal", schema)
		require.NoError(t, err)
		assert.Contains(t, content, "+kubebuilder:resource:scope=Namespaced\n")
		assert.NotContains(t, content, "categories=")
	})
}
