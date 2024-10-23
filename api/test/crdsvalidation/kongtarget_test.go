package crdsvalidation_test

import (
	"fmt"
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

	t.Run("tags validation", func(t *testing.T) {
		CRDValidationTestCasesGroup[*configurationv1alpha1.KongTarget]{
			{
				Name: "up to 20 tags are allowed",
				TestObject: &configurationv1alpha1.KongTarget{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongTargetSpec{
						UpstreamRef: configurationv1alpha1.TargetRef{
							Name: "upstream",
						},
						KongTargetAPISpec: configurationv1alpha1.KongTargetAPISpec{
							Target: "example.com",
							Weight: 100,
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
				TestObject: &configurationv1alpha1.KongTarget{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongTargetSpec{
						UpstreamRef: configurationv1alpha1.TargetRef{
							Name: "upstream",
						},
						KongTargetAPISpec: configurationv1alpha1.KongTargetAPISpec{
							Target: "example.com",
							Weight: 100,
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
				TestObject: &configurationv1alpha1.KongTarget{
					ObjectMeta: commonObjectMeta,
					Spec: configurationv1alpha1.KongTargetSpec{
						UpstreamRef: configurationv1alpha1.TargetRef{
							Name: "upstream",
						},
						KongTargetAPISpec: configurationv1alpha1.KongTargetAPISpec{
							Target: "example.com",
							Weight: 100,
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
