package configuration_test

import (
	"strings"
	"testing"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/crd-from-oas/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/crd-from-oas/test/crdsvalidation/common"
	testscheme "github.com/kong/kong-operator/v2/crd-from-oas/test/scheme"
)

func TestPortal(t *testing.T) {
	t.Parallel()

	scheme := testscheme.Get()
	cfg, ns := common.Setup(t, scheme)

	t.Run("name field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.Portal]{
			{
				Name: "name with valid value passes validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name: "test-portal",
						},
					},
				},
			},
			{
				Name: "name at max length (255) passes validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name: strings.Repeat("a", 255),
						},
					},
				},
			},
			{
				Name: "name exceeding max length (256) fails validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name: strings.Repeat("a", 256),
						},
					},
				},
				ExpectedErrorMessage: new("spec.apiSpec.name: Too long: may not be more than 255"),
			},
			{
				Name: "name is immutable",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name: "immutable-portal-name",
						},
					},
				},
				Update: func(p *konnectv1alpha1.Portal) {
					p.Spec.APISpec.Name = "changed-portal-name"
				},
				ExpectedUpdateErrorMessage: new("name is immutable"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("display_name field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.Portal]{
			{
				Name: "display_name with valid value passes validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name:        "portal-display-name-valid",
							DisplayName: "My Portal",
						},
					},
				},
			},
			{
				Name: "display_name at max length (255) passes validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name:        "portal-display-name-max",
							DisplayName: strings.Repeat("d", 255),
						},
					},
				},
			},
			{
				Name: "display_name exceeding max length (256) fails validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name:        "portal-display-name-over",
							DisplayName: strings.Repeat("d", 256),
						},
					},
				},
				ExpectedErrorMessage: new("spec.apiSpec.display_name: Too long: may not be more than 255"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("description field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.Portal]{
			{
				Name: "description at max length (512) passes validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name:        "portal-desc-max",
							Description: new(strings.Repeat("x", 512)),
						},
					},
				},
			},
			{
				Name: "description exceeding max length (513) fails validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name:        "portal-desc-over",
							Description: new(strings.Repeat("x", 513)),
						},
					},
				},
				ExpectedErrorMessage: new("spec.apiSpec.description: Too long: may not be more than 512"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("default_api_visibility field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.Portal]{
			{
				Name: "default_api_visibility set to public passes validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name:                 "portal-vis-public",
							DefaultAPIVisibility: "public",
						},
					},
				},
			},
			{
				Name: "default_api_visibility set to private passes validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name:                 "portal-vis-private",
							DefaultAPIVisibility: "private",
						},
					},
				},
			},
			{
				Name: "default_api_visibility with invalid value fails validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name:                 "portal-vis-invalid",
							DefaultAPIVisibility: "invalid",
						},
					},
				},
				ExpectedErrorMessage: new(`spec.apiSpec.default_api_visibility: Unsupported value: "invalid"`),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("default_page_visibility field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.Portal]{
			{
				Name: "default_page_visibility set to public passes validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name:                  "portal-page-vis-public",
							DefaultPageVisibility: "public",
						},
					},
				},
			},
			{
				Name: "default_page_visibility set to private passes validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name:                  "portal-page-vis-private",
							DefaultPageVisibility: "private",
						},
					},
				},
			},
			{
				Name: "default_page_visibility with invalid value fails validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name:                  "portal-page-vis-invalid",
							DefaultPageVisibility: "invalid",
						},
					},
				},
				ExpectedErrorMessage: new(`spec.apiSpec.default_page_visibility: Unsupported value: "invalid"`),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("full spec with all fields passes validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.Portal]{
			{
				Name: "all fields populated passes validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name:                    "portal-full-spec",
							DisplayName:             "Full Spec Portal",
							Description:             new("A full spec portal"),
							AuthenticationEnabled:   true,
							AutoApproveApplications: true,
							AutoApproveDevelopers:   true,
							DefaultAPIVisibility:    "public",
							DefaultPageVisibility:   "private",
							RBACEnabled:             true,
							Labels: konnectv1alpha1.LabelsUpdate{
								"env": "test",
							},
						},
					},
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
