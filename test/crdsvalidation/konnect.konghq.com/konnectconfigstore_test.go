package crdsvalidation

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validKonnectConfigStore(ns string) *konnectv1alpha1.KonnectConfigStore {
	return &konnectv1alpha1.KonnectConfigStore{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: konnectv1alpha1.KonnectConfigStoreSpec{
			ControlPlaneRef: konnectv1alpha1.ConfigStoreControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-control-plane",
				},
			},
			APISpec: konnectv1alpha1.KonnectConfigStoreAPISpec{
				Name: "test-config-store",
			},
		},
	}
}

func TestKonnectConfigStore(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("controlPlaneRef validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectConfigStore]{
			{
				Name:       "konnectNamespacedRef is accepted",
				TestObject: validKonnectConfigStore(ns.Name),
				Update: func(obj *konnectv1alpha1.KonnectConfigStore) {
					obj.Spec.APISpec.Name = "updated-config-store"
				},
			},
			{
				Name: "konnectNamespacedRef is required",
				TestObject: func() *konnectv1alpha1.KonnectConfigStore {
					obj := validKonnectConfigStore(ns.Name)
					obj.Spec.ControlPlaneRef.KonnectNamespacedRef = nil
					return obj
				}(),
				ExpectedErrorMessage: new("spec.controlPlaneRef.konnectNamespacedRef: Required value"),
			},
			{
				Name: "type is required",
				TestObject: func() *konnectv1alpha1.KonnectConfigStore {
					obj := validKonnectConfigStore(ns.Name)
					obj.Spec.ControlPlaneRef.Type = ""
					return obj
				}(),
				ExpectedErrorMessage: new("spec.controlPlaneRef.type: Required value"),
			},
			{
				Name: "kic is rejected",
				TestObject: func() *konnectv1alpha1.KonnectConfigStore {
					obj := validKonnectConfigStore(ns.Name)
					obj.Spec.ControlPlaneRef.Type = commonv1alpha1.ControlPlaneRefKIC
					return obj
				}(),
				ExpectedErrorMessage: new("spec.controlPlaneRef.type: Unsupported value"),
			},
			{
				Name: "controlPlaneRef is required",
				TestObject: func() *konnectv1alpha1.KonnectConfigStore {
					obj := validKonnectConfigStore(ns.Name)
					obj.Spec.ControlPlaneRef = konnectv1alpha1.ConfigStoreControlPlaneRef{}
					return obj
				}(),
				ExpectedErrorMessage: new("spec.controlPlaneRef: Required value"),
			},
		}.RunWithConfig(t, cfg, scheme)

		for _, tt := range []struct {
			name string
			ref  map[string]any
		}{
			{
				name: "legacy namespacedRef is rejected",
				ref: map[string]any{
					"type":          "namespacedRef",
					"namespacedRef": map[string]any{"name": "test-control-plane"},
				},
			},
			{
				name: "legacy konnectID is rejected",
				ref: map[string]any{
					"type":      "konnectID",
					"konnectID": "12345678-1234-1234-1234-123456789abc",
				},
			},
		} {
			tc := common.TestCase[*unstructured.Unstructured]{
				Name: tt.name,
				TestObject: &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": konnectv1alpha1.GroupVersion.String(),
					"kind":       "KonnectConfigStore",
					"metadata":   map[string]any{"generateName": "config-store-", "namespace": ns.Name},
					"spec": map[string]any{
						"controlPlaneRef": tt.ref,
						"apiSpec":         map[string]any{"name": "test-config-store"},
					},
				}},
				ExpectedErrorMessage: new("spec.controlPlaneRef.type: Unsupported value"),
			}
			tc.RunWithConfig(t, cfg, scheme)
		}
	})

	t.Run("controlPlaneRef immutability", func(t *testing.T) {
		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, validKonnectConfigStore(ns.Name)).RunWithConfig(t, cfg, scheme)
	})

	t.Run("apiSpec.name validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectConfigStore]{
			{
				Name: "name at minimum length is accepted",
				TestObject: func() *konnectv1alpha1.KonnectConfigStore {
					obj := validKonnectConfigStore(ns.Name)
					obj.Spec.APISpec.Name = "a"
					return obj
				}(),
			},
			{
				Name: "name at maximum length is accepted",
				TestObject: func() *konnectv1alpha1.KonnectConfigStore {
					obj := validKonnectConfigStore(ns.Name)
					obj.Spec.APISpec.Name = strings.Repeat("a", 100)
					return obj
				}(),
			},
			{
				Name: "name exceeding maximum length is rejected",
				TestObject: func() *konnectv1alpha1.KonnectConfigStore {
					obj := validKonnectConfigStore(ns.Name)
					obj.Spec.APISpec.Name = strings.Repeat("a", 101)
					return obj
				}(),
				// NOTE: Different Kubernetes versions return different complete error
				// messages, so match on their common portion.
				ExpectedErrorMessage: new("spec.apiSpec.name: Too long: may not be"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
