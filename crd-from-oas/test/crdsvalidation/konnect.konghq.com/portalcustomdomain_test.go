package configuration_test

import (
	"testing"

	konnectv1alpha1 "github.com/kong/kong-operator/crd-from-oas/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/crd-from-oas/test/crdsvalidation/common"
	testscheme "github.com/kong/kong-operator/crd-from-oas/test/scheme"
	"github.com/kong/kong-operator/test/envtest"
	"k8s.io/utils/ptr"
)

func TestPortalCustomDomain(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := testscheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("type field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.PortalCustomDomain]{
			{
				Name: "basic spec passes validation",
				TestObject: &konnectv1alpha1.PortalCustomDomain{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalCustomDomainSpec{
						PortalRef: konnectv1alpha1.ObjectRef{
							Name: "test-portal",
						},
						PortalCustomDomainAPISpec: konnectv1alpha1.PortalCustomDomainAPISpec{
							Enabled:  ptr.To(true),
							Hostname: "custom.example.com",
							SSL: konnectv1alpha1.CreatePortalCustomDomainSSL{
								"type": "standard",
							},
						},
					},
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
