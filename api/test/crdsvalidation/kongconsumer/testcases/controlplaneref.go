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
	},
}
