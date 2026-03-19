package configuration_test

import (
	"strings"
	"testing"

	xkonnectv1alpha1 "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestKonnectEventControlPlane(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := Scheme(t)
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("labels field validation", func(t *testing.T) {
		controlPlaneWithLabelValue := func(labelValue string) *xkonnectv1alpha1.KonnectEventControlPlane {
			return &xkonnectv1alpha1.KonnectEventControlPlane{
				ObjectMeta: common.CommonObjectMeta(ns.Name),
				Spec: xkonnectv1alpha1.KonnectEventControlPlaneSpec{
					APISpec: xkonnectv1alpha1.KonnectEventControlPlaneAPISpec{
						Name: "event-control-plane",
						Labels: xkonnectv1alpha1.Labels{
							"team": xkonnectv1alpha1.LabelsValue(labelValue),
						},
					},
				},
			}
		}

		common.TestCasesGroup[*xkonnectv1alpha1.KonnectEventControlPlane]{
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
