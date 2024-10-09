package testcases

import (
	"github.com/samber/lo"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var controlPlaneRef = testCasesGroup{
	Name: "fields of controlPlaneRef",
	TestCases: []testCase{
		{
			// Since KongConsumers managed by KIC do not require spec.controlPlane, KongConsumers without spec.controlPlaneRef should be allowed.
			Name: "no cpRef is valid",
			KongConsumer: configurationv1.KongConsumer{
				ObjectMeta: commonObjectMeta,
				Username:   "username-1",
			},
		},
		{
			Name: "cpRef cannot have namespace",
			KongConsumer: configurationv1.KongConsumer{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1.KongConsumerSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name:      "test-konnect-control-plane",
							Namespace: "another-namespace",
						},
					},
				},
				Username: "username-1",
			},
			ExpectedErrorMessage: lo.ToPtr("spec.controlPlaneRef cannot specify namespace for namespaced resource"),
		},
		{
			Name: "providing konnectID when type is konnectNamespacedRef yields an error",
			KongConsumer: configurationv1.KongConsumer{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1.KongConsumerSpec{
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
			KongConsumer: configurationv1.KongConsumer{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1.KongConsumerSpec{
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
			KongConsumer: configurationv1.KongConsumer{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1.KongConsumerSpec{
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
			KongConsumer: configurationv1.KongConsumer{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1.KongConsumerSpec{
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
	},
}
