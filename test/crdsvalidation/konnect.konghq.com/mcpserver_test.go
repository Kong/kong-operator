package crdsvalidation

import (
	"strings"
	"testing"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

const validUUID = "12345678-1234-1234-1234-123456789abc"

func validMCPServer(ns string) *konnectv1alpha1.MCPServer {
	return &konnectv1alpha1.MCPServer{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: konnectv1alpha1.MCPServerSpec{
			ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			Mirror: konnectv1alpha1.MirrorSpec{
				Konnect: konnectv1alpha1.MirrorKonnect{
					ID: commonv1alpha1.KonnectIDType(validUUID),
				},
			},
		},
	}
}

func TestMCPServer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("controlPlaneRef validation", func(t *testing.T) {
		obj := validMCPServer(ns.Name)
		common.NewCRDValidationTestCasesGroupCPRefChange(
			t, cfg, obj, common.NotSupportedByKIC, common.ControlPlaneRefRequired,
		).RunWithConfig(t, cfg, scheme)
	})

	t.Run("mirror field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.MCPServer]{
			{
				Name:       "valid UUID in mirror.konnect.id is accepted",
				TestObject: validMCPServer(ns.Name),
			},
			{
				Name: "missing mirror field is rejected",
				TestObject: &konnectv1alpha1.MCPServer{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.MCPServerSpec{
						ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-cp",
							},
						},
					},
				},
				ExpectedErrorMessage: new("spec.mirror.konnect.id: Required value"),
			},
			{
				Name: "invalid UUID pattern in mirror.konnect.id is rejected",
				TestObject: func() *konnectv1alpha1.MCPServer {
					obj := validMCPServer(ns.Name)
					obj.Spec.Mirror.Konnect.ID = "not-a-valid-uuid"
					return obj
				}(),
				ExpectedErrorMessage: new("spec.mirror.konnect.id in body should match"),
			},
			{
				Name: "mirror.konnect.id exceeding max length is rejected",
				TestObject: func() *konnectv1alpha1.MCPServer {
					obj := validMCPServer(ns.Name)
					obj.Spec.Mirror.Konnect.ID = commonv1alpha1.KonnectIDType(strings.Repeat("a", 37))
					return obj
				}(),
				ExpectedErrorMessage: new("spec.mirror.konnect.id: Too long"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("controlPlaneRef immutability", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.MCPServer]{
			{
				Name:       "changing controlPlaneRef is rejected",
				TestObject: validMCPServer(ns.Name),
				Update: func(obj *konnectv1alpha1.MCPServer) {
					obj.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "different-cp"
				},
				ExpectedUpdateErrorMessage: new("spec.controlPlaneRef is immutable"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
