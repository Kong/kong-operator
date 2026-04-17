package crdsvalidation

import (
	"strings"
	"testing"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestPortal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("name field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.Portal]{
			{
				Name: "name with valid value passes validation",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
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
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
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
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name: strings.Repeat("a", 256),
						},
					},
				},
				// NOTE: Different versions of k8s return a different error
				// message hence this trying to match on the common part of the message.
				ExpectedErrorMessage: new("spec.apiSpec.name: Too long: may not be"),
			},
			{
				Name: "name is immutable",
				TestObject: &konnectv1alpha1.Portal{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalSpec{
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
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
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
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
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
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
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name:        "portal-display-name-over",
							DisplayName: strings.Repeat("d", 256),
						},
					},
				},
				// NOTE: Different versions of k8s return a different error
				// message hence this trying to match on the common part of the message.
				ExpectedErrorMessage: new("spec.apiSpec.display_name: Too long: may not be"),
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
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
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
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name:        "portal-desc-over",
							Description: new(strings.Repeat("x", 513)),
						},
					},
				},
				// NOTE: Different versions of k8s return a different error
				// message hence this trying to match on the common part of the message.
				ExpectedErrorMessage: new("spec.apiSpec.description: Too long: may not be"),
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
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
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
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
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
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
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
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
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
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
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
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
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

	t.Run("labels field validation", func(t *testing.T) {
		portalWithLabelValue := func(labelValue string) *konnectv1alpha1.Portal {
			return &konnectv1alpha1.Portal{
				ObjectMeta: common.CommonObjectMeta(ns.Name),
				Spec: konnectv1alpha1.PortalSpec{
					KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
							Name: "test-auth",
						},
					},
					APISpec: konnectv1alpha1.PortalAPISpec{
						Name: "portal-labels",
						Labels: konnectv1alpha1.LabelsUpdate{
							"team": konnectv1alpha1.LabelsUpdateValue(labelValue),
						},
					},
				},
			}
		}

		common.TestCasesGroup[*konnectv1alpha1.Portal]{
			{
				Name:       "labels value at max length (63) passes validation",
				TestObject: portalWithLabelValue(strings.Repeat("a", 63)),
			},
			{
				Name:                 "labels value exceeding max length (64) fails validation",
				TestObject:           portalWithLabelValue(strings.Repeat("a", 64)),
				ExpectedErrorMessage: new("Too long: may not be"),
			},
			{
				Name:                 "labels value with invalid pattern fails validation",
				TestObject:           portalWithLabelValue("invalid!"),
				ExpectedErrorMessage: new("^[a-z0-9A-Z]{1}([a-z0-9A-Z-._]*[a-z0-9A-Z]+)?$"),
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
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
						APISpec: konnectv1alpha1.PortalAPISpec{
							Name:                    "portal-full-spec",
							DisplayName:             "Full Spec Portal",
							Description:             new("A full spec portal"),
							AuthenticationEnabled:   "Enabled",
							AutoApproveApplications: "Enabled",
							AutoApproveDevelopers:   "Enabled",
							DefaultAPIVisibility:    "public",
							DefaultPageVisibility:   "private",
							RBACEnabled:             "Enabled",
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
