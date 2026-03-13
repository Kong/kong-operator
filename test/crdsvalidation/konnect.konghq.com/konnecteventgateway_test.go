package crdsvalidation_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

	validKonnectConfig := konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
		Name: "auth-1",
	}

	t.Run("source and mirror constraints", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectEventGateway]{
			{
				Name: "Origin source with createGatewayRequest is valid",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "eg-1",
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
			},
			{
				Name: "Origin source without createGatewayRequest is invalid",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source:               new(commonv1alpha1.EntitySourceOrigin),
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.createGatewayRequest with name must be set when source is Origin"),
			},
			{
				Name: "Origin source with mirror field is invalid",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						Mirror: &konnectv1alpha1.EventGatewayMirrorSpec{
							Konnect: konnectv1alpha1.EventGatewayMirrorKonnect{
								ID: "8ae65120-cdec-4310-84c1-4b19caf67967",
							},
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.mirror cannot be set when source is Origin"),
			},
			{
				Name: "Mirror source with valid Konnect ID is valid",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceMirror),
						Mirror: &konnectv1alpha1.EventGatewayMirrorSpec{
							Konnect: konnectv1alpha1.EventGatewayMirrorKonnect{
								ID: "8ae65120-cdec-4310-84c1-4b19caf67967",
							},
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
			},
			{
				Name: "Mirror source without mirror field is invalid",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source:               new(commonv1alpha1.EntitySourceMirror),
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.mirror must be set when source is Mirror"),
			},
			{
				Name: "Mirror source with createGatewayRequest set is invalid",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceMirror),
						Mirror: &konnectv1alpha1.EventGatewayMirrorSpec{
							Konnect: konnectv1alpha1.EventGatewayMirrorKonnect{
								ID: "8ae65120-cdec-4310-84c1-4b19caf67967",
							},
						},
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "eg-1",
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.createGatewayRequest cannot be set when source is Mirror"),
			},
			{
				Name: "Mirror source with malformed Konnect ID is invalid",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceMirror),
						Mirror: &konnectv1alpha1.EventGatewayMirrorSpec{
							Konnect: konnectv1alpha1.EventGatewayMirrorKonnect{
								ID: "not-a-uuid",
							},
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.mirror.konnect.id in body should match"),
			},
			{
				Name: "source is immutable",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "eg-1",
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				Update: func(eg *konnectv1alpha1.KonnectEventGateway) {
					eg.Spec.Source = new(commonv1alpha1.EntitySourceMirror)
					eg.Spec.CreateGatewayRequest = nil
					eg.Spec.Mirror = &konnectv1alpha1.EventGatewayMirrorSpec{
						Konnect: konnectv1alpha1.EventGatewayMirrorKonnect{
							ID: "8ae65120-cdec-4310-84c1-4b19caf67967",
						},
					}
				},
				ExpectedUpdateErrorMessage: new("spec.source is immutable"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("konnect ref immutability", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectEventGateway]{
			{
				Name: "spec.konnect change is not allowed when Programmed=True",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "eg-1",
						},
						KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
							Name: "auth-1",
						},
					},
					Status: konnectv1alpha1.KonnectEventGatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:               "Programmed",
								Status:             metav1.ConditionTrue,
								Reason:             "Programmed",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
				Update: func(eg *konnectv1alpha1.KonnectEventGateway) {
					eg.Spec.KonnectConfiguration.Name = "auth-2"
				},
				ExpectedUpdateErrorMessage: new("spec.konnect is immutable when an entity is already Programmed"),
			},
			{
				Name: "spec.konnect change is not allowed when APIAuthValid=True",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "eg-1",
						},
						KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
							Name: "auth-1",
						},
					},
					Status: konnectv1alpha1.KonnectEventGatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:               "APIAuthValid",
								Status:             metav1.ConditionTrue,
								Reason:             "Valid",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
				Update: func(eg *konnectv1alpha1.KonnectEventGateway) {
					eg.Spec.KonnectConfiguration.Name = "auth-2"
				},
				ExpectedUpdateErrorMessage: new("spec.konnect is immutable when an entity refers to a Valid API Auth Configuration"),
			},
			{
				Name: "spec.konnect change is allowed when not Programmed and not APIAuthValid",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "eg-1",
						},
						KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
							Name: "auth-1",
						},
					},
					Status: konnectv1alpha1.KonnectEventGatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:               "Programmed",
								Status:             metav1.ConditionFalse,
								Reason:             "Pending",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
				Update: func(eg *konnectv1alpha1.KonnectEventGateway) {
					eg.Spec.KonnectConfiguration.Name = "auth-2"
				},
			},
			{
				Name: "spec.konnect change is allowed when status is not set",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "eg-1",
						},
						KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
							Name: "auth-1",
						},
					},
				},
				Update: func(eg *konnectv1alpha1.KonnectEventGateway) {
					eg.Spec.KonnectConfiguration.Name = "auth-2"
				},
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("labels constraints", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectEventGateway]{
			{
				Name: "labels of length 40 is allowed",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "eg-1",
							Labels: func() map[string]string {
								labels := make(map[string]string)
								for i := range 40 {
									labels[fmt.Sprintf("label-%d", i)] = fmt.Sprintf("value-%d", i)
								}
								return labels
							}(),
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
			},
			{
				Name: "labels length must not exceed 40",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "eg-1",
							Labels: func() map[string]string {
								labels := make(map[string]string)
								for i := range 41 {
									labels[fmt.Sprintf("label-%d", i)] = fmt.Sprintf("value-%d", i)
								}
								return labels
							}(),
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.createGatewayRequest.labels must not have more than 40 entries"),
			},
			{
				Name: "label key length must not exceed 63 characters",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "eg-1",
							Labels: map[string]string{
								lo.RandomString(64, lo.LowerCaseLettersCharset): "value",
							},
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.createGatewayRequest.labels keys must be of length 1-63 characters"),
			},
			{
				Name: "label key must be at least 1 character long",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name:   "eg-1",
							Labels: map[string]string{"": "value"},
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.createGatewayRequest.labels keys must be of length 1-63 characters"),
			},
			{
				Name: "label value length must not exceed 63 characters",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "eg-1",
							Labels: map[string]string{
								"key": lo.RandomString(64, lo.LowerCaseLettersCharset),
							},
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.createGatewayRequest.labels values must be of length 1-63 characters"),
			},
			{
				Name: "label value must be at least 1 character long",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name:   "eg-1",
							Labels: map[string]string{"key": ""},
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.createGatewayRequest.labels values must be of length 1-63 characters"),
			},
			{
				Name: "label key must not start with 'kong'",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name:   "eg-1",
							Labels: map[string]string{"kong_key": "value"},
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.createGatewayRequest.labels keys must not start with 'kong', 'konnect', 'mesh', 'kic' or '_'"),
			},
			{
				Name: "label key must not start with 'konnect'",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name:   "eg-1",
							Labels: map[string]string{"konnect_key": "value"},
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.createGatewayRequest.labels keys must not start with 'kong', 'konnect', 'mesh', 'kic' or '_'"),
			},
			{
				Name: "label key must not start with 'mesh'",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name:   "eg-1",
							Labels: map[string]string{"mesh_key": "value"},
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.createGatewayRequest.labels keys must not start with 'kong', 'konnect', 'mesh', 'kic' or '_'"),
			},
			{
				Name: "label key must not start with 'kic'",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name:   "eg-1",
							Labels: map[string]string{"kic_key": "value"},
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.createGatewayRequest.labels keys must not start with 'kong', 'konnect', 'mesh', 'kic' or '_'"),
			},
			{
				Name: "label key must not start with underscore",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name:   "eg-1",
							Labels: map[string]string{"_key": "value"},
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.createGatewayRequest.labels keys must not start with 'kong', 'konnect', 'mesh', 'kic' or '_'"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})

	t.Run("minRuntimeVersion format", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.KonnectEventGateway]{
			{
				Name: "valid minRuntimeVersion '1.1' is accepted",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name:              "eg-1",
							MinRuntimeVersion: new("1.1"),
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
			},
			{
				Name: "valid minRuntimeVersion '10.20' is accepted",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name:              "eg-1",
							MinRuntimeVersion: new("10.20"),
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
			},
			{
				Name: "minRuntimeVersion without a dot is rejected",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name:              "eg-1",
							MinRuntimeVersion: new("1"),
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.createGatewayRequest.minRuntimeVersion in body should match"),
			},
			{
				Name: "minRuntimeVersion with letters is rejected",
				TestObject: &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name:              "eg-1",
							MinRuntimeVersion: new("v1.1"),
						},
						KonnectConfiguration: validKonnectConfig,
					},
				},
				ExpectedErrorMessage: new("spec.createGatewayRequest.minRuntimeVersion in body should match"),
			},
		}.
			RunWithConfig(t, cfg, scheme)
	})
}
