package crdsvalidation_test

import (
	"fmt"
	"testing"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestKonnectAIGateway(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	validKonnectConfiguration := konnectv1alpha2.KonnectConfiguration{
		APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
			Name: "auth-1",
		},
	}

	t.Run("source, mirror and apiSpec constraints", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectAIGateway]{
			{
				Name: "origin (default source) with valid apiSpec is allowed",
				TestObject: &konnectv1alpha1.KonnectAIGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectAIGatewaySpec{
						KonnectConfiguration: validKonnectConfiguration,
						APISpec: &konnectv1alpha1.KonnectAIGatewayAPISpec{
							DisplayName: "my-ai-gw",
							Name:        "my-ai-gw",
						},
					},
				},
			},
			{
				Name: "origin with mirror set is rejected",
				TestObject: &konnectv1alpha1.KonnectAIGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectAIGatewaySpec{
						KonnectConfiguration: validKonnectConfiguration,
						Source:               new(commonv1alpha1.EntitySourceOrigin),
						Mirror: &konnectv1alpha2.MirrorSpec{
							Konnect: konnectv1alpha2.MirrorKonnect{
								ID: commonv1alpha1.KonnectIDType("a7c8b120-cdec-4310-84c1-4b19caf67967"),
							},
						},
						APISpec: &konnectv1alpha1.KonnectAIGatewayAPISpec{
							DisplayName: "my-ai-gw",
							Name:        "my-ai-gw",
						},
					},
				},
				ExpectedErrorMessage: new("mirror field cannot be set for type Origin"),
			},
			{
				Name: "origin without apiSpec is rejected",
				TestObject: &konnectv1alpha1.KonnectAIGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectAIGatewaySpec{
						KonnectConfiguration: validKonnectConfiguration,
						Source:               new(commonv1alpha1.EntitySourceOrigin),
					},
				},
				ExpectedErrorMessage: new("apiSpec must be set for type Origin"),
			},
			{
				Name: "mirror with id and no apiSpec is allowed",
				TestObject: &konnectv1alpha1.KonnectAIGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectAIGatewaySpec{
						KonnectConfiguration: validKonnectConfiguration,
						Source:               new(commonv1alpha1.EntitySourceMirror),
						Mirror: &konnectv1alpha2.MirrorSpec{
							Konnect: konnectv1alpha2.MirrorKonnect{
								ID: commonv1alpha1.KonnectIDType("a7c8b120-cdec-4310-84c1-4b19caf67967"),
							},
						},
					},
				},
			},
			{
				Name: "mirror without mirror block is rejected",
				TestObject: &konnectv1alpha1.KonnectAIGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectAIGatewaySpec{
						KonnectConfiguration: validKonnectConfiguration,
						Source:               new(commonv1alpha1.EntitySourceMirror),
					},
				},
				ExpectedErrorMessage: new("mirror field must be set for type Mirror"),
			},
			{
				Name: "mirror with apiSpec is rejected",
				TestObject: &konnectv1alpha1.KonnectAIGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectAIGatewaySpec{
						KonnectConfiguration: validKonnectConfiguration,
						Source:               new(commonv1alpha1.EntitySourceMirror),
						Mirror: &konnectv1alpha2.MirrorSpec{
							Konnect: konnectv1alpha2.MirrorKonnect{
								ID: commonv1alpha1.KonnectIDType("a7c8b120-cdec-4310-84c1-4b19caf67967"),
							},
						},
						APISpec: &konnectv1alpha1.KonnectAIGatewayAPISpec{
							DisplayName: "my-ai-gw",
							Name:        "my-ai-gw",
						},
					},
				},
				ExpectedErrorMessage: new("apiSpec cannot be set for type Mirror"),
			},
			{
				Name: "source is immutable",
				TestObject: &konnectv1alpha1.KonnectAIGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectAIGatewaySpec{
						KonnectConfiguration: validKonnectConfiguration,
						Source:               new(commonv1alpha1.EntitySourceOrigin),
						APISpec: &konnectv1alpha1.KonnectAIGatewayAPISpec{
							DisplayName: "my-ai-gw",
							Name:        "my-ai-gw",
						},
					},
				},
				Update: func(obj *konnectv1alpha1.KonnectAIGateway) {
					obj.Spec.Source = new(commonv1alpha1.EntitySourceMirror)
				},
				ExpectedUpdateErrorMessage: new("spec.source is immutable"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("labels constraints", func(t *testing.T) {
		labels := func(n int) map[string]konnectv1alpha1.PublicLabelsValue {
			out := make(map[string]konnectv1alpha1.PublicLabelsValue, n)
			for i := range n {
				out[fmt.Sprintf("label%02d", i)] = konnectv1alpha1.PublicLabelsValue("value")
			}
			return out
		}

		common.TestCasesGroup[*konnectv1alpha1.KonnectAIGateway]{
			{
				Name: "up to 50 labels is allowed",
				TestObject: &konnectv1alpha1.KonnectAIGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectAIGatewaySpec{
						KonnectConfiguration: validKonnectConfiguration,
						APISpec: &konnectv1alpha1.KonnectAIGatewayAPISpec{
							DisplayName: "my-ai-gw",
							Name:        "my-ai-gw",
							Labels:      labels(50),
						},
					},
				},
			},
			{
				Name: "more than 50 labels is rejected",
				TestObject: &konnectv1alpha1.KonnectAIGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectAIGatewaySpec{
						KonnectConfiguration: validKonnectConfiguration,
						APISpec: &konnectv1alpha1.KonnectAIGatewayAPISpec{
							DisplayName: "my-ai-gw",
							Name:        "my-ai-gw",
							Labels:      labels(51),
						},
					},
				},
				ExpectedErrorMessage: new("spec.apiSpec.labels: Too many: 51: must have at most 50 items"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
