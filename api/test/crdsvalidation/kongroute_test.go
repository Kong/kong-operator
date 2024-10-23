package crdsvalidation_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestKongRoute(t *testing.T) {
	t.Run("cp ref", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongRoute]{
			{
				Name: "cannot specify with service ref",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type: configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{
								Name: "test-konnect-service",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("Only one of controlPlaneRef or serviceRef can be set"),
			},
			{
				Name: "konnectNamespacedRef reference is valid",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{},
					},
				},
			},
			{
				Name: "not providing konnectNamespacedRef when type is konnectNamespacedRef yields an error",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectNamespacedRef must be set"),
			},
			{
				Name: "not providing konnectID when type is konnectID yields an error",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectID,
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
			},
			{
				Name: "providing namespace in konnectNamespacedRef yields an error",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name:      "test-konnect-control-plane",
								Namespace: "another-namespace",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.controlPlaneRef cannot specify namespace for namespaced resource"),
			},
			{
				Name: "providing konnectID when type is konnectNamespacedRef yields an error",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type:      configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectID: lo.ToPtr("123456"),
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectNamespacedRef must be set"),
			},

			{
				Name: "providing konnectNamespacedRef when type is konnectID yields an error",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectID,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectID must be set"),
			},
			{
				Name: "providing konnectNamespacedRef and konnectID when type is konnectID yields an error",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
							KonnectID: lo.ToPtr("123456"),
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectID, konnectNamespacedRef must not be set"),
			},
			{
				Name: "providing konnectID and konnectNamespacedRef when type is konnectNamespacedRef yields an error",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type:      configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectID: lo.ToPtr("123456"),
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("when type is konnectNamespacedRef, konnectID must not be set"),
			},
			{
				Name: "konnectNamespacedRef reference name cannot be changed when an entity is Programmed",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{},
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
					ks.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "new-konnect-control-plane"
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
			},
			{
				Name: "konnectNamespacedRef reference type cannot be changed when an entity is Programmed",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "test-konnect-control-plane",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{},
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
					ks.Spec.ControlPlaneRef.Type = configurationv1alpha1.ControlPlaneRefKonnectID
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.controlPlaneRef is immutable when an entity is already Programmed"),
			},
		}.Run(t)
	})

	t.Run("protocols", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongRoute]{
			{
				Name: "no http in protocols implies no other requirements",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type:          configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{Name: "svc"},
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
							NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{Name: "svc"},
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
							NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{Name: "svc"},
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

	t.Run("service ref", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongRoute]{
			{
				Name: "NamespacedRef reference is valid",
				TestObject: &configurationv1alpha1.KongRoute{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongRouteSpec{
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type: configurationv1alpha1.ServiceRefNamespacedRef,
							NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{
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
							NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{
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
							NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{
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
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.serviceRef is immutable when an entity is already Programmed"),
			},
		}.Run(t)
	})

	t.Run("tags validation", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongRoute]{
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
