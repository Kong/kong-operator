package crdsvalidation

import (
	"testing"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestPortalPage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	portalRef := commonv1alpha1.ObjectRef{
		Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: &commonv1alpha1.NamespacedRef{
			Name: "test-portal",
		},
	}

	t.Run("field validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.PortalPage]{
			{
				Name: "base case",
				TestObject: &konnectv1alpha1.PortalPage{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalPageSpec{
						PortalRef: portalRef,
						APISpec: konnectv1alpha1.PortalPageAPISpec{
							Content: "Page content",
							Slug:    "slug-1",
						},
					},
				},
			},
			{
				Name: "base case with more fields set",
				TestObject: &konnectv1alpha1.PortalPage{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalPageSpec{
						PortalRef: portalRef,
						APISpec: konnectv1alpha1.PortalPageAPISpec{
							Content:     "Page content",
							Slug:        "slug-1",
							Description: "Page description",
							Status:      "published",
							Visibility:  "public",
						},
					},
				},
			},
			{
				Name: "parentPageIDRef can reference another PortalPage by namespacedRef",
				TestObject: &konnectv1alpha1.PortalPage{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalPageSpec{
						PortalRef: portalRef,
						APISpec: konnectv1alpha1.PortalPageAPISpec{
							Content: "Page content",
							Slug:    "slug-1",
							ParentPageIDRef: &commonv1alpha1.ObjectRef{
								Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
								NamespacedRef: &commonv1alpha1.NamespacedRef{
									Name: "page-1",
								},
							},
						},
					},
				},
			},
			{
				Name: "parentPageIDRef can reference another PortalPage by konnectID",
				TestObject: &konnectv1alpha1.PortalPage{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.PortalPageSpec{
						PortalRef: portalRef,
						APISpec: konnectv1alpha1.PortalPageAPISpec{
							Content: "Page content",
							Slug:    "slug-2",
							ParentPageIDRef: &commonv1alpha1.ObjectRef{
								Type:      commonv1alpha1.ObjectRefTypeKonnectID,
								KonnectID: new("f6b7c8d9-0000-0000-0000-000000000000"),
							},
						},
					},
				},
			},
			{
				Name: "parentPageIDRef cannot reference the PortalPage itself",
				TestObject: &konnectv1alpha1.PortalPage{
					Name:      "self-ref-page",
					Namespace: ns.Name,
					Spec: konnectv1alpha1.PortalPageSpec{
						PortalRef: portalRef,
						APISpec: konnectv1alpha1.PortalPageAPISpec{
							Content: "Page content",
							Slug:    "slug-3",
							ParentPageIDRef: &commonv1alpha1.ObjectRef{
								Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
								NamespacedRef: &commonv1alpha1.NamespacedRef{
									Name: "self-ref-page",
								},
							},
						},
					},
				},
				ExpectedErrorMessage: new("parentPageIDRef must not reference the PortalPage itself"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("portalRef immutability", func(t *testing.T) {
		obj := &konnectv1alpha1.PortalPage{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: konnectv1alpha1.PortalPageSpec{
				PortalRef: portalRef,
				APISpec: konnectv1alpha1.PortalPageAPISpec{
					Content: "Page content",
					Slug:    "slug-1",
				},
			},
		}
		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, obj).RunWithConfig(t, cfg, scheme)
	})

}
