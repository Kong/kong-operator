package testcases

import (
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
)

var fields = testCasesGroup{
	Name: "fields",
	TestCases: []testCase{
		{
			Name: "name field can be set",
			KongConsumerGroup: configurationv1beta1.KongConsumerGroup{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1beta1.KongConsumerGroupSpec{
					Name: lo.ToPtr("test-consumer-group"),
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: "test-konnect-control-plane",
						},
					},
				},
			},
		},
	},
}
