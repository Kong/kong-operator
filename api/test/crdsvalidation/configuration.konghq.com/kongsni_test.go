package configuration_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/test/crdsvalidation/common"
)

func TestKongSNI(t *testing.T) {
	t.Run("certificate ref", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongSNI]{
			{
				Name: "certificate ref name is required",
				TestObject: &configurationv1alpha1.KongSNI{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongSNISpec{
						CertificateRef: commonv1alpha1.NameRef{},
						KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
							Name: "example.com",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.certificateRef.name in body should be at least 1 chars long"),
			},
			{
				Name: "certificate ref can be changed before programmed",
				TestObject: &configurationv1alpha1.KongSNI{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongSNISpec{
						CertificateRef: commonv1alpha1.NameRef{
							Name: "cert1",
						},
						KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
							Name: "example.com",
						},
					},
				},
				Update: func(sni *configurationv1alpha1.KongSNI) {
					sni.Spec.CertificateRef = commonv1alpha1.NameRef{
						Name: "cert-2",
					}
				},
			},
			{
				Name: "certiifacate ref is immutable after programmed",
				TestObject: &configurationv1alpha1.KongSNI{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongSNISpec{
						CertificateRef: commonv1alpha1.NameRef{
							Name: "cert1",
						},
						KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
							Name: "example.com",
						},
					},
					Status: configurationv1alpha1.KongSNIStatus{
						Conditions: []metav1.Condition{
							{
								Type:               "Programmed",
								Status:             metav1.ConditionTrue,
								Reason:             "programmed",
								ObservedGeneration: int64(1),
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
				Update: func(sni *configurationv1alpha1.KongSNI) {
					sni.Spec.CertificateRef = commonv1alpha1.NameRef{
						Name: "cert-2",
					}
				},
				ExpectedUpdateErrorMessage: lo.ToPtr("spec.certificateRef is immutable when programmed"),
			},
		}.Run(t)
	})

	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongSNI]{
			{
				Name: "spec.name must not be empty",
				TestObject: &configurationv1alpha1.KongSNI{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongSNISpec{
						CertificateRef: commonv1alpha1.NameRef{
							Name: "cert1",
						},
					},
				},
				ExpectedErrorMessage: lo.ToPtr("spec.name in body should be at least 1 chars long"),
			},
		}.Run(t)
	})

	t.Run("tags validation", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.KongSNI]{
			{
				Name: "up to 20 tags are allowed",
				TestObject: &configurationv1alpha1.KongSNI{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongSNISpec{
						CertificateRef: commonv1alpha1.NameRef{
							Name: "cert1",
						},
						KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
							Name: "example.com",
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
				TestObject: &configurationv1alpha1.KongSNI{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongSNISpec{
						CertificateRef: commonv1alpha1.NameRef{
							Name: "cert1",
						},
						KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
							Name: "example.com",
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
				TestObject: &configurationv1alpha1.KongSNI{
					ObjectMeta: common.CommonObjectMeta,
					Spec: configurationv1alpha1.KongSNISpec{
						CertificateRef: commonv1alpha1.NameRef{
							Name: "cert1",
						},
						KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
							Name: "example.com",
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
