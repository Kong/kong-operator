package configuration_test

import (
	"testing"

	konnectv1alpha1 "github.com/kong/kong-operator/crd-from-oas/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/crd-from-oas/test/crdsvalidation/common"
	testscheme "github.com/kong/kong-operator/crd-from-oas/test/scheme"
	"github.com/kong/kong-operator/test/envtest"
)

func TestDcrProvider(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := testscheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("type field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.DcrProvider]{
			{
				Name: "basic spec passes validation",
				TestObject: &konnectv1alpha1.DcrProvider{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.DcrProviderSpec{
						DcrProviderAPISpec: konnectv1alpha1.DcrProviderAPISpec{
							DcrProviderConfig: &konnectv1alpha1.DcrProviderConfig{
								Type: konnectv1alpha1.DcrProviderConfigTypeAuth0,
							},
						},
					},
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
