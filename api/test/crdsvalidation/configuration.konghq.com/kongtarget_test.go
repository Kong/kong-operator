package configuration_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/v2/test/crdsvalidation/common"
)

func TestKongTarget(t *testing.T) {
	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongTarget]{
			{
				Name: "weight must between 0 and 65535",
				TestObject: &configurationv1alpha1.KongTarget{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongTargetSpec{
						UpstreamRef: commonv1alpha1.NameRef{
							Name: "upstream",
						},
						KongTargetAPISpec: configurationv1alpha1.KongTargetAPISpec{
							Target: "example.com",
							Weight: 100000,
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.weight: Invalid value: 100000"),
			},
			{
				Name: "weight must between 0 and 65535",
				TestObject: &configurationv1alpha1.KongTarget{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongTargetSpec{
						UpstreamRef: commonv1alpha1.NameRef{
							Name: "upstream",
						},
						KongTargetAPISpec: configurationv1alpha1.KongTargetAPISpec{
							Target: "example.com",
							Weight: -1,
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.weight: Invalid value: -1"),
			},
		}.Run(t)
	})

	t.Run("upstream ref", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongTarget]{
			{
				Name: "upstream ref is immutable",
				TestObject: &configurationv1alpha1.KongTarget{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongTargetSpec{
						UpstreamRef: commonv1alpha1.NameRef{
							Name: "upstream",
						},
						KongTargetAPISpec: configurationv1alpha1.KongTargetAPISpec{
							Target: "example.com",
							Weight: 100,
						},
					},
				},
				Update: func(kt *configurationv1alpha1.KongTarget) {
					kt.Spec.UpstreamRef = commonv1alpha1.NameRef{
						Name: "upstream-2",
					}
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.upstreamRef is immutable"),
			},
		}.Run(t)
	})

	t.Run("tags validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongTarget]{
			{
				Name: "up to 20 tags are allowed",
				TestObject: &configurationv1alpha1.KongTarget{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongTargetSpec{
						UpstreamRef: commonv1alpha1.NameRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongTargetSpec{
						UpstreamRef: commonv1alpha1.NameRef{
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
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongTargetSpec{
						UpstreamRef: commonv1alpha1.NameRef{
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
