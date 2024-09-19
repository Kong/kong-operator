package testcases

import (
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

var upstreamRef = testCasesGroup{
	Name: "upstreamRef",
	TestCases: []testCase{
		{
			Name: "upstream ref is immutable",
			KongTarget: configurationv1alpha1.KongTarget{
				ObjectMeta: commonObjectMeta,
				Spec: configurationv1alpha1.KongTargetSpec{
					UpstreamRef: configurationv1alpha1.TargetRef{
						Name: "upstream",
					},
					KongTargetAPISpec: configurationv1alpha1.KongTargetAPISpec{
						Target: "example.com",
						Weight: 100,
					},
				},
			},
			Update: func(kt *configurationv1alpha1.KongTarget) {
				kt.Spec.UpstreamRef = configurationv1alpha1.TargetRef{
					Name: "upstream-2",
				}
			},
			ExpectedUpdateErrorMessage: lo.ToPtr("spec.upstreamRef is immutable"),
		},
	},
}
