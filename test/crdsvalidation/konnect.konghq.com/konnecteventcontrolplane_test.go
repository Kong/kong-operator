package crdsvalidation

import (
	"strings"
	"testing"

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

	t.Run("labels field validation", func(t *testing.T) {
		controlPlaneWithLabelValue := func(labelValue string) *konnectv1alpha1.KonnectEventGateway {
			return &konnectv1alpha1.KonnectEventGateway{
				ObjectMeta: common.CommonObjectMeta(ns.Name),
				Spec: konnectv1alpha1.KonnectEventGatewaySpec{
					KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
							Name: "test-auth",
						},
					},
					APISpec: konnectv1alpha1.KonnectEventGatewayAPISpec{
						Name: "event-control-plane",
						Labels: konnectv1alpha1.Labels{
							"team": konnectv1alpha1.LabelsValue(labelValue),
						},
					},
				},
			}
		}

		common.TestCasesGroup[*konnectv1alpha1.KonnectEventGateway]{
			{
				Name:       "labels value at max length (63) passes validation",
				TestObject: controlPlaneWithLabelValue(strings.Repeat("a", 63)),
			},
			{
				Name:                 "labels value exceeding max length (64) fails validation",
				TestObject:           controlPlaneWithLabelValue(strings.Repeat("a", 64)),
				ExpectedErrorMessage: new("Too long: may not be"),
			},
			{
				Name:                 "labels value with invalid pattern fails validation",
				TestObject:           controlPlaneWithLabelValue("invalid!"),
				ExpectedErrorMessage: new("^[a-z0-9A-Z]{1}([a-z0-9A-Z-._]*[a-z0-9A-Z]+)?$"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
