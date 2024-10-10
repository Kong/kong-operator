package crdsvalidation_test

import (
	"testing"

	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestKongTarget(t *testing.T) {
	t.Run("spec", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongTarget]{
			{
				Name: "weight must between 0 and 65535",
				TestObject: &configurationv1alpha1.KongTarget{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongTargetSpec{
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
		}.Run(t)
	})

	t.Run("upstream ref", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongTarget]{
			{
				Name: "upstream ref is immutable",
				TestObject: &configurationv1alpha1.KongTarget{
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
		}.Run(t)
	})
}
