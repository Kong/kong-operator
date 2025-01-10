package crdsvalidation_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/test/crdsvalidation"
)

func TestKongRoute(t *testing.T) {
	obj := &configurationv1alpha1.KongRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongRoute",
			APIVersion: configurationv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: commonObjectMeta,
	}

	t.Run("cp ref", func(t *testing.T) {
		NewCRDValidationTestCasesGroupCPRefChange(t, obj, NotSupportedByKIC).Run(t)
	})

	t.Run("cp ref, type=kic", func(t *testing.T) {
		// NOTE: empty cp ref is not allowed in this context because a route can be attached to a service
		// but this test doesn't check that.
		NewCRDValidationTestCasesGroupCPRefChangeKICUnsupportedTypes(t, obj, EmptyControlPlaneRefNotAllowed).Run(t)
	})

	t.Run("protocols", func(t *testing.T) {
		crdsvalidation.TestCasesGroup[*configurationv1alpha1.KongRoute]{
			{
				Name: "no http in protocols implies no other requirements",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type:          configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &configurationv1alpha1.KongObjectRef{Name: "svc"},
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
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type:          configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &configurationv1alpha1.KongObjectRef{Name: "svc"},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Protocols: []sdkkonnectcomp.RouteProtocols{"http"},
							Hosts:     []string{"example.com"},
						},
					},
				},
			},
			{
				Name: "http in protocols no hosts, methods, paths or headers yields an error",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type:          configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &configurationv1alpha1.KongObjectRef{Name: "svc"},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Protocols: []sdkkonnectcomp.RouteProtocols{"http"},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("If protocols has 'http', at least one of 'hosts', 'methods', 'paths' or 'headers' must be set"),
			},
		}.Run(t)
	})

	t.Run("no service ref and no cp ref provided", func(t *testing.T) {
		crdsvalidation.TestCasesGroup[*configurationv1alpha1.KongRoute]{
			{
				Name: "have to provide either controlPlaneRef or serviceRef",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Paths: []string{"/"},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Has to set either controlPlaneRef or serviceRef"),
			},
		}.Run(t)
	})

	t.Run("service ref", func(t *testing.T) {
		crdsvalidation.TestCasesGroup[*configurationv1alpha1.KongRoute]{
			{
				Name: "NamespacedRef reference is valid",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type: configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &configurationv1alpha1.KongObjectRef{
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
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type: configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &configurationv1alpha1.KongObjectRef{
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
					ObjectMeta: commonObjectMeta,
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
					ObjectMeta: commonObjectMeta,
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
				Name: "NamespacedRef reference name cannot be changed when an entity is Programmed",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type: configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &configurationv1alpha1.KongObjectRef{
								Name: "test-konnect-service",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Paths: []string{"/"},
						},
					},
					Status: configurationv1alpha1.KongRouteStatus{
						Conditions: []metav1.Condition{
							{
								Type:               "Programmed",
								Status:             metav1.ConditionTrue,
								Reason:             "Programmed",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
				Update: func(ks *configurationv1alpha1.KongRoute) {
					ks.Spec.ServiceRef.NamespacedRef.Name = "new-konnect-service"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.serviceRef is immutable when an entity is already Programmed"),
			},
			{
				Name: "NamespacedRef reference type cannot be changed when an entity is Programmed",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type: configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &configurationv1alpha1.KongObjectRef{
								Name: "test-konnect-service",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Paths: []string{"/"},
						},
					},
					Status: configurationv1alpha1.KongRouteStatus{
						Conditions: []metav1.Condition{
							{
								Type:               "Programmed",
								Status:             metav1.ConditionTrue,
								Reason:             "Programmed",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
				Update: func(ks *configurationv1alpha1.KongRoute) {
					ks.Spec.ServiceRef.Type = "otherRef"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("Unsupported value: \"otherRef\": supported values: \"namespacedRef\""),
			},
		}.Run(t)
	})

	t.Run("tags validation", func(t *testing.T) {
		crdsvalidation.TestCasesGroup[*configurationv1alpha1.KongRoute]{
			{
				Name: "up to 20 tags are allowed",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
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
