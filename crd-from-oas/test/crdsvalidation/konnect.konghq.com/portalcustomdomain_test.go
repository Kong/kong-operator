package configuration_test

import (
	"strings"
	"testing"

	"github.com/samber/lo"
	"k8s.io/utils/ptr"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/crd-from-oas/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/crd-from-oas/test/crdsvalidation/common"
	testscheme "github.com/kong/kong-operator/v2/crd-from-oas/test/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestPortalCustomDomain(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := testscheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	validSpec := func() konnectv1alpha1.PortalCustomDomainSpec {
		return konnectv1alpha1.PortalCustomDomainSpec{
			PortalRef: konnectv1alpha1.ObjectRef{
				Name: "test-portal",
			},
			APISpec: konnectv1alpha1.PortalCustomDomainAPISpec{
				Enabled:  ptr.To(true),
				Hostname: "custom.example.com",
				SSL: konnectv1alpha1.CreatePortalCustomDomainSSL{
					"type": "standard",
				},
			},
		}
	}

	t.Run("portal_ref field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.PortalCustomDomain]{
			{
				Name: "portal_ref with valid name passes validation",
				TestObject: &konnectv1alpha1.PortalCustomDomain{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec:       validSpec(),
				},
			},
			{
				Name: "portal_ref name at max length (253) passes validation",
				TestObject: &konnectv1alpha1.PortalCustomDomain{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: func() konnectv1alpha1.PortalCustomDomainSpec {
						s := validSpec()
						s.PortalRef.Name = strings.Repeat("a", 253)
						return s
					}(),
				},
			},
			{
				Name: "portal_ref name exceeding max length (254) fails validation",
				TestObject: &konnectv1alpha1.PortalCustomDomain{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: func() konnectv1alpha1.PortalCustomDomainSpec {
						s := validSpec()
						s.PortalRef.Name = strings.Repeat("a", 254)
						return s
					}(),
				},
				ExpectedErrorMessage: lo.ToPtr("spec.portal_ref.name: Too long: may not be more than 253"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("hostname field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.PortalCustomDomain]{
			{
				Name: "hostname with valid value passes validation",
				TestObject: &konnectv1alpha1.PortalCustomDomain{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec:       validSpec(),
				},
			},
			{
				Name: "hostname at max length (256) passes validation",
				TestObject: &konnectv1alpha1.PortalCustomDomain{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: func() konnectv1alpha1.PortalCustomDomainSpec {
						s := validSpec()
						s.APISpec.Hostname = strings.Repeat("h", 256)
						return s
					}(),
				},
			},
			{
				Name: "hostname exceeding max length (257) fails validation",
				TestObject: &konnectv1alpha1.PortalCustomDomain{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: func() konnectv1alpha1.PortalCustomDomainSpec {
						s := validSpec()
						s.APISpec.Hostname = strings.Repeat("h", 257)
						return s
					}(),
				},
				ExpectedErrorMessage: lo.ToPtr("spec.apiSpec.hostname: Too long: may not be more than 256"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("enabled field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.PortalCustomDomain]{
			{
				Name: "enabled set to true passes validation",
				TestObject: &konnectv1alpha1.PortalCustomDomain{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec:       validSpec(),
				},
			},
			{
				Name: "enabled set to false passes validation",
				TestObject: &konnectv1alpha1.PortalCustomDomain{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: func() konnectv1alpha1.PortalCustomDomainSpec {
						s := validSpec()
						s.APISpec.Enabled = ptr.To(false)
						return s
					}(),
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("full spec with all fields passes validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.PortalCustomDomain]{
			{
				Name: "all fields populated passes validation",
				TestObject: &konnectv1alpha1.PortalCustomDomain{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalCustomDomainSpec{
						PortalRef: konnectv1alpha1.ObjectRef{
							Name: "test-portal",
						},
						APISpec: konnectv1alpha1.PortalCustomDomainAPISpec{
							Enabled:  ptr.To(true),
							Hostname: "portal.custom-domain.example.com",
							SSL: konnectv1alpha1.CreatePortalCustomDomainSSL{
								"type":                       "standard",
								"domain_verification_method": "dns",
							},
						},
					},
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
