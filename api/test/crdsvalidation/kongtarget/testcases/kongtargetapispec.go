package testcases

import (
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var kongTargetAPISpec = testCasesGroup{
	Name: "kongTargetAPISpec",
	TestCases: []testCase{
		{
			Name: "weight must between 0 and 65535",
			KongTarget: configurationv1alpha1.KongTarget{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongTargetSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
						KonnectID: lo.ToPtr("konnect-1"),
					},
					UpstreamRef: configurationv1alpha1.TargetRef{
						Name: "upstream",
					},
					KongTargetAPISpec: configurationv1alpha1.KongTargetAPISpec{
						Target: "example.com",
						Weight: 100000,
					},
				},
			},
			ExpectedErrorMessage: lo.ToPtr("spec.weight: Invalid value: 100000"),
			Update: func(kt *configurationv1alpha1.KongTarget) {
				kt.Spec.KongTargetAPISpec.Weight = -1
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.weight: Invalid value: -1"),
		},
	},
}
