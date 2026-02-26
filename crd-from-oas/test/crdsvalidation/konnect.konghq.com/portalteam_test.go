package configuration_test

import (
	"strings"
	"testing"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/crd-from-oas/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/crd-from-oas/test/crdsvalidation/common"
	testscheme "github.com/kong/kong-operator/v2/crd-from-oas/test/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestPortalTeam(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := testscheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme,
		envtest.WithInstallGatewayCRDs(false),
	)

	validSpec := func() konnectv1alpha1.PortalTeamSpec {
		return konnectv1alpha1.PortalTeamSpec{
			PortalRef: konnectv1alpha1.ObjectRef{
				Name: "test-portal",
			},
			APISpec: konnectv1alpha1.PortalTeamAPISpec{
				Name: "test-team",
			},
		}
	}

	t.Run("portal_ref field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.PortalTeam]{
			{
				Name: "portal_ref with valid name passes validation",
				TestObject: &konnectv1alpha1.PortalTeam{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec:       validSpec(),
				},
			},
			{
				Name: "portal_ref name at max length (253) passes validation",
				TestObject: &konnectv1alpha1.PortalTeam{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalTeamSpec{
						PortalRef: konnectv1alpha1.ObjectRef{
							Name: strings.Repeat("a", 253),
						},
						APISpec: konnectv1alpha1.PortalTeamAPISpec{
							Name: "team-ref-max",
						},
					},
				},
			},
			{
				Name: "portal_ref name exceeding max length (254) fails validation",
				TestObject: &konnectv1alpha1.PortalTeam{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalTeamSpec{
						PortalRef: konnectv1alpha1.ObjectRef{
							Name: strings.Repeat("a", 254),
						},
						APISpec: konnectv1alpha1.PortalTeamAPISpec{
							Name: "team-ref-over",
						},
					},
				},
				ExpectedErrorMessage: new("spec.portal_ref.name: Too long: may not be more than 253"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("name field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.PortalTeam]{
			{
				Name: "name with valid value passes validation",
				TestObject: &konnectv1alpha1.PortalTeam{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec:       validSpec(),
				},
			},
			{
				Name: "name at max length (256) passes validation",
				TestObject: &konnectv1alpha1.PortalTeam{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalTeamSpec{
						PortalRef: konnectv1alpha1.ObjectRef{
							Name: "test-portal",
						},
						APISpec: konnectv1alpha1.PortalTeamAPISpec{
							Name: strings.Repeat("a", 256),
						},
					},
				},
			},
			{
				Name: "name exceeding max length (257) fails validation",
				TestObject: &konnectv1alpha1.PortalTeam{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalTeamSpec{
						PortalRef: konnectv1alpha1.ObjectRef{
							Name: "test-portal",
						},
						APISpec: konnectv1alpha1.PortalTeamAPISpec{
							Name: strings.Repeat("a", 257),
						},
					},
				},
				ExpectedErrorMessage: new("spec.apiSpec.name: Too long: may not be more than 256"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("description field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.PortalTeam]{
			{
				Name: "description with valid value passes validation",
				TestObject: &konnectv1alpha1.PortalTeam{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalTeamSpec{
						PortalRef: konnectv1alpha1.ObjectRef{
							Name: "test-portal",
						},
						APISpec: konnectv1alpha1.PortalTeamAPISpec{
							Name:        "team-desc-valid",
							Description: "A valid description",
						},
					},
				},
			},
			{
				Name: "description at max length (250) passes validation",
				TestObject: &konnectv1alpha1.PortalTeam{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalTeamSpec{
						PortalRef: konnectv1alpha1.ObjectRef{
							Name: "test-portal",
						},
						APISpec: konnectv1alpha1.PortalTeamAPISpec{
							Name:        "team-desc-max",
							Description: strings.Repeat("d", 250),
						},
					},
				},
			},
			{
				Name: "description exceeding max length (251) fails validation",
				TestObject: &konnectv1alpha1.PortalTeam{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalTeamSpec{
						PortalRef: konnectv1alpha1.ObjectRef{
							Name: "test-portal",
						},
						APISpec: konnectv1alpha1.PortalTeamAPISpec{
							Name:        "team-desc-over",
							Description: strings.Repeat("d", 251),
						},
					},
				},
				ExpectedErrorMessage: new("spec.apiSpec.description: Too long: may not be more than 250"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("full spec with all fields passes validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.PortalTeam]{
			{
				Name: "all fields populated passes validation",
				TestObject: &konnectv1alpha1.PortalTeam{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalTeamSpec{
						PortalRef: konnectv1alpha1.ObjectRef{
							Name: "test-portal",
						},
						APISpec: konnectv1alpha1.PortalTeamAPISpec{
							Name:        "full-spec-team",
							Description: "A team with all fields",
						},
					},
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
