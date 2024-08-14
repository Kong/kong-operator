package testcases

import (
	"github.com/samber/lo"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var requiredFields = testCasesGroup{
	Name: "consumer required fields",
	TestCases: []testCase{
		{
			Name: "username or custom_id required (username provided)",
			KongConsumer: configurationv1.KongConsumer{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1.KongConsumerSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
				},
				Username: "username-1",
			},
		},
		{
			Name: "username or custom_id required (custom_id provided)",
			KongConsumer: configurationv1.KongConsumer{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1.KongConsumerSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
				},
				CustomID: "customid-1",
			},
		},
		{
			Name: "username or custom_id required (none provided)",
			KongConsumer: configurationv1.KongConsumer{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1.KongConsumerSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("Need to provide either username or custom_id"),
		},
	},
}
