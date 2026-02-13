package configuration_test

import (
	"testing"

	konnectv1alpha1 "github.com/kong/kong-operator/crd-from-oas/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/crd-from-oas/test/crdsvalidation/common"
	testscheme "github.com/kong/kong-operator/crd-from-oas/test/scheme"
	"github.com/kong/kong-operator/test/envtest"
)

func TestPortalTeam(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := testscheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("type field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.PortalTeam]{
			{
				Name: "basic spec passes validation",
				TestObject: &konnectv1alpha1.PortalTeam{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalTeamSpec{
						PortalRef: konnectv1alpha1.ObjectRef{
							Name: "test-portal",
						},
						PortalTeamAPISpec: konnectv1alpha1.PortalTeamAPISpec{
							Name: "test-team",
						},
					},
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
