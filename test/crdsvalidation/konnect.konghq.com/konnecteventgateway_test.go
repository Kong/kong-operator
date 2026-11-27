package crdsvalidation_test

import (
	"testing"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestKonnectEventGateway(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	validKonnectConfiguration := konnectv1alpha2.KonnectConfiguration{
		APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
			Name: "auth-1",
		},
	}

	t.Run("name pattern", func(t *testing.T) {
		withName := func(name string) *konnectv1alpha1.KonnectEventGateway {
			return &konnectv1alpha1.KonnectEventGateway{
				ObjectMeta: common.CommonObjectMeta(ns.Name),
				Spec: konnectv1alpha1.KonnectEventGatewaySpec{
					KonnectConfiguration: validKonnectConfiguration,
					APISpec:              &konnectv1alpha1.KonnectEventGatewayAPISpec{Name: konnectv1alpha1.GatewayName(name)},
				},
			}
		}
		const patternErr = "spec.apiSpec.name in body should match '^[\\p{L}\\p{N}][\\p{L}\\p{N} _\\-\\.:/+']*[\\p{L}\\p{N}]$'"
		common.TestCasesGroup[*konnectv1alpha1.KonnectEventGateway]{
			{Name: "non-ASCII name with every allowed separator", TestObject: withName("Zürich a b_c-d.e:f/g+h'i 東京")},
			{Name: "name with !", TestObject: withName("gw!"), ExpectedErrorMessage: new(patternErr)},
			{Name: "one-character name", TestObject: withName("g"), ExpectedErrorMessage: new(patternErr)},
			{Name: "name starting with -", TestObject: withName("-gw"), ExpectedErrorMessage: new(patternErr)},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("source, mirror and apiSpec constraints", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectEventGateway]{
			{
				Name: "origin (default source) with valid apiSpec is allowed",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						KonnectConfiguration: validKonnectConfiguration,
						APISpec: &konnectv1alpha1.KonnectEventGatewayAPISpec{
							Name: "my-event-gw",
						},
					},
				},
			},
			{
				Name: "origin with mirror set is rejected",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						KonnectConfiguration: validKonnectConfiguration,
						Source:               new(commonv1alpha1.EntitySourceOrigin),
						Mirror: &konnectv1alpha2.MirrorSpec{
							Konnect: konnectv1alpha2.MirrorKonnect{
								ID: commonv1alpha1.KonnectIDType("a7c8b120-cdec-4310-84c1-4b19caf67967"),
							},
						},
						APISpec: &konnectv1alpha1.KonnectEventGatewayAPISpec{
							Name: "my-event-gw",
						},
					},
				},
				ExpectedErrorMessage: new("mirror field cannot be set for type Origin"),
			},
			{
				Name: "origin without apiSpec is rejected",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						KonnectConfiguration: validKonnectConfiguration,
						Source:               new(commonv1alpha1.EntitySourceOrigin),
					},
				},
				ExpectedErrorMessage: new("apiSpec must be set for type Origin"),
			},
			{
				Name: "mirror with id and no apiSpec is allowed",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
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
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						KonnectConfiguration: validKonnectConfiguration,
						Source:               new(commonv1alpha1.EntitySourceMirror),
					},
				},
				ExpectedErrorMessage: new("mirror field must be set for type Mirror"),
			},
			{
				Name: "mirror with apiSpec is rejected",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						KonnectConfiguration: validKonnectConfiguration,
						Source:               new(commonv1alpha1.EntitySourceMirror),
						Mirror: &konnectv1alpha2.MirrorSpec{
							Konnect: konnectv1alpha2.MirrorKonnect{
								ID: commonv1alpha1.KonnectIDType("a7c8b120-cdec-4310-84c1-4b19caf67967"),
							},
						},
						APISpec: &konnectv1alpha1.KonnectEventGatewayAPISpec{
							Name: "my-event-gw",
						},
					},
				},
				ExpectedErrorMessage: new("apiSpec cannot be set for type Mirror"),
			},
			{
				Name: "source is immutable",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						KonnectConfiguration: validKonnectConfiguration,
						Source:               new(commonv1alpha1.EntitySourceOrigin),
						APISpec: &konnectv1alpha1.KonnectEventGatewayAPISpec{
							Name: "my-event-gw",
						},
					},
				},
				Update: func(obj *konnectv1alpha1.KonnectEventGateway) {
					obj.Spec.Source = new(commonv1alpha1.EntitySourceMirror)
				},
				ExpectedUpdateErrorMessage: new("spec.source is immutable"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
