package crdsvalidation

import (
	"strings"
	"testing"

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
			ControlPlaneRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
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
				Name:       "namespacedRef is accepted",
				TestObject: validKonnectConfigStore(ns.Name),
			},
			{
				Name: "konnectID is accepted",
				TestObject: func() *konnectv1alpha1.KonnectConfigStore {
					obj := validKonnectConfigStore(ns.Name)
					obj.Spec.ControlPlaneRef = commonv1alpha1.ObjectRef{
						Type:      commonv1alpha1.ObjectRefTypeKonnectID,
						KonnectID: new("12345678-1234-1234-1234-123456789abc"),
					}
					return obj
				}(),
			},
			{
				Name: "controlPlaneRef is required",
				TestObject: func() *konnectv1alpha1.KonnectConfigStore {
					obj := validKonnectConfigStore(ns.Name)
					obj.Spec.ControlPlaneRef = commonv1alpha1.ObjectRef{}
					return obj
				}(),
				ExpectedErrorMessage: new("spec.controlPlaneRef: Required value"),
			},
			{
				Name: "namespacedRef type requires namespacedRef",
				TestObject: func() *konnectv1alpha1.KonnectConfigStore {
					obj := validKonnectConfigStore(ns.Name)
					obj.Spec.ControlPlaneRef.NamespacedRef = nil
					return obj
				}(),
				ExpectedErrorMessage: new("when type is namespacedRef, namespacedRef must be set"),
			},
			{
				Name: "namespacedRef type rejects konnectID",
				TestObject: func() *konnectv1alpha1.KonnectConfigStore {
					obj := validKonnectConfigStore(ns.Name)
					obj.Spec.ControlPlaneRef.KonnectID = new("12345678-1234-1234-1234-123456789abc")
					return obj
				}(),
				ExpectedErrorMessage: new("when type is namespacedRef, konnectID must not be set"),
			},
			{
				Name: "konnectID type requires konnectID",
				TestObject: func() *konnectv1alpha1.KonnectConfigStore {
					obj := validKonnectConfigStore(ns.Name)
					obj.Spec.ControlPlaneRef = commonv1alpha1.ObjectRef{
						Type: commonv1alpha1.ObjectRefTypeKonnectID,
					}
					return obj
				}(),
				ExpectedErrorMessage: new("when type is konnectID, konnectID must be set"),
			},
			{
				Name: "konnectID type rejects namespacedRef",
				TestObject: func() *konnectv1alpha1.KonnectConfigStore {
					obj := validKonnectConfigStore(ns.Name)
					obj.Spec.ControlPlaneRef = commonv1alpha1.ObjectRef{
						Type: commonv1alpha1.ObjectRefTypeKonnectID,
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name: "test-control-plane",
						},
					}
					return obj
				}(),
				ExpectedErrorMessage: new("when type is konnectID, namespacedRef must not be set"),
			},
		}.RunWithConfig(t, cfg, scheme)
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
