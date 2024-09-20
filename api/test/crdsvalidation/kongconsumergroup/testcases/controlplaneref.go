package testcases

import (
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
)

var controlPlaneRef = testCasesGroup{
	Name: "fields of controlPlaneRef",
	TestCases: []testCase{
		{
			Name: "cpRef cannot have namespace",
			KongConsumerGroup: configurationv1beta1.KongConsumerGroup{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1beta1.KongConsumerGroupSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name:      "test-konnect-control-plane",
							Namespace: "another-namespace",
						},
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.controlPlaneRef cannot specify namespace for namespaced resource"),
		},
	},
}
