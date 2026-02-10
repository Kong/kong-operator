package configuration_test

import (
	"testing"

	konnectv1alpha1 "github.com/kong/kong-operator/crd-from-oas/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/crd-from-oas/test/crdsvalidation/common"
	"github.com/kong/kong-operator/test/envtest"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestPortal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := runtime.NewScheme()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("required fields", func(t *testing.T) {
		t.Run("tags validation", func(t *testing.T) {
			common.TestCasesGroup[*konnectv1alpha1.Portal]{
				{
					Name: "up to 20 tags are allowed",
					TestObject: &konnectv1alpha1.Portal{
						ObjectMeta: common.CommonObjectMeta(ns.Name),
						Spec:       konnectv1alpha1.PortalSpec{},
					},
				},
			}.
				RunWithConfig(t, cfg, scheme)
		})
	})

	t.Run("type field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.Portal]{
			{
				Name: "base 2",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec:       konnectv1alpha1.PortalSpec{},
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
