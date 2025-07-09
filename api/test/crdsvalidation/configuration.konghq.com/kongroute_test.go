package configuration_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/v2/test/crdsvalidation/common"
)

func TestKongRoute(t *testing.T) {
	obj := &configurationv1alpha1.KongRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongRoute",
			APIVersion: configurationv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: common.CommonObjectMeta,
	}

	t.Run("cp ref", func(t *testing.T) {
		common.NewCRDValidationTestCasesGroupCPRefChange(t, obj, common.NotSupportedByKIC, common.ControlPlaneRefNotRequired).Run(t)
	})

	t.Run("service ref", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongRoute]{
			{
				Name: "bind servicebound to controlplane too",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type: configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &commonv1alpha1.NameRef{
								Name: "test-konnect-service",
							},
						},
					},
				},
				Update: func(kr *configurationv1alpha1.KongRoute) {
					kr.Spec.ControlPlaneRef = &commonv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					}
				},
			},
			{
				Name: "make serviceless",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type: configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &commonv1alpha1.NameRef{
								Name: "test-konnect-service",
							},
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
				},
				Update: func(kr *configurationv1alpha1.KongRoute) {
					kr.Spec.ServiceRef = nil
				},
			},
		}.Run(t)
	})

	t.Run("protocols", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongRoute]{
			{
				Name: "no http in protocols implies no other requirements",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type:          configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &commonv1alpha1.NameRef{Name: "svc"},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Paths: []string{"/"},
						},
					},
				},
			},
			{
				Name: "http in protocols with hosts set yields no error",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type:          configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &commonv1alpha1.NameRef{Name: "svc"},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Protocols: []sdkkonnectcomp.RouteJSONProtocols{"http"},
							Hosts:     []string{"example.com"},
						},
					},
				},
			},
			{
				Name: "http in protocols no hosts, methods, paths or headers yields an error",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type:          configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &commonv1alpha1.NameRef{Name: "svc"},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Protocols: []sdkkonnectcomp.RouteJSONProtocols{"http"},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("If protocols has 'http', at least one of 'hosts', 'methods', 'paths' or 'headers' must be set"),
			},
		}.Run(t)
	})

	t.Run("service ref", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongRoute]{
			{
				Name: "NamespacedRef reference is valid",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type: configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &commonv1alpha1.NameRef{
								Name: "test-konnect-service",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Paths: []string{"/"},
						},
					},
				},
			},
			{
				Name: "NamespacedRef reference is invalid when empty name is provided",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type: configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &commonv1alpha1.NameRef{
								Name: "",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Paths: []string{"/"},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.serviceRef.namespacedRef.name in body should be at least 1 chars long"),
			},
			{
				Name: "NamespacedRef reference is invalid when name is not provided",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type: configurationv1alpha1.ServiceRefNamespacedRef,
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Paths: []string{"/"},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is namespacedRef, namespacedRef must be set"),
			},
			{
				Name: "not providing namespacedRef when type is namespacedRef yields an error",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type: configurationv1alpha1.ServiceRefNamespacedRef,
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Paths: []string{"/"},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is namespacedRef, namespacedRef must be set"),
			},
		}.Run(t)
	})

	t.Run("tags validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongRoute]{
			{
				Name: "up to 20 tags are allowed",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Tags: func() []string {
								var tags []string
								for i := range 20 {
									tags = append(tags, fmt.Sprintf("tag-%d", i))
								}
								return tags
							}(),
						},
					},
				},
			},
			{
				Name: "more than 20 tags are not allowed",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Tags: func() []string {
								var tags []string
								for i := range 21 {
									tags = append(tags, fmt.Sprintf("tag-%d", i))
								}
								return tags
							}(),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.tags: Too many: 21: must have at most 20 items"),
			},
			{
				Name: "tags entries must not be longer than 128 characters",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Tags: []string{
								lo.RandomString(129, lo.AlphanumericCharset),
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("tags entries must not be longer than 128 characters"),
			},
		}.Run(t)
	})
}
