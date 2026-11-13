package crdsvalidation

import (
	"testing"

	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/helpers/helm"
	"github.com/kong/kong-operator/v2/test/helpers/kcfg"
)

// ValidationPolicyEventGateway contains Event Gateway validation policies path relative to the kong-operator chart.
const ValidationPolicyEventGateway = "templates/validation-policy-eventgateway.yaml"

func TestKegDataPlaneValidatingAdmissionPolicy(t *testing.T) {
	var (
		ctx     = t.Context()
		scheme  = scheme.Get()
		cfg, ns = envtest.Setup(t, ctx, scheme)
	)

	// Prevents controller-runtime from logging
	// [controller-runtime] log.SetLogger(...) was never called; logs will not be displayed.
	ctrl.SetLogger(zapr.NewLogger(zap.New(zapcore.NewNopCore())))

	wc := &common.WarningCollector{}
	cfg.WarningHandler = wc

	helm.ApplyTemplate(ctx, t, cfg, kcfg.ChartPath(), []string{ValidationPolicyEventGateway})

	dataPlane := func(config *eventgatewayv1alpha1.KegDataPlaneConfiguration) *eventgatewayv1alpha1.KegDataPlane {
		return &eventgatewayv1alpha1.KegDataPlane{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
				ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
					Type:                 eventgatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
					KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{Name: "my-event-gateway"},
				},
				Config: config,
			},
		}
	}

	common.TestCasesGroup[*eventgatewayv1alpha1.KegDataPlane]{
		{
			Name: "configPollIntervalSeconds is deprecated",
			TestObject: dataPlane(&eventgatewayv1alpha1.KegDataPlaneConfiguration{
				ConfigPollIntervalSeconds: new(int32(5)), //nolint:staticcheck // testing deprecated field
			}),
			ExpectedErrorEventuallyConfig: common.SharedEventuallyConfig,
			WarningCollector:              wc,
			ExpectedWarningMessage:        new("spec.config.configPollIntervalSeconds is deprecated"),
		},
		{
			Name: "enableDebugEndpoints is deprecated",
			TestObject: dataPlane(&eventgatewayv1alpha1.KegDataPlaneConfiguration{
				EnableDebugEndpoints: new(eventgatewayv1alpha1.DebugEndpointsStateEnabled), //nolint:staticcheck // testing deprecated field
			}),
			ExpectedErrorEventuallyConfig: common.SharedEventuallyConfig,
			WarningCollector:              wc,
			ExpectedWarningMessage:        new("spec.config.enableDebugEndpoints is deprecated"),
		},
		{
			Name:       "a KegDataPlane without config is admitted",
			TestObject: dataPlane(nil),
		},
		{
			Name: "a config without the removed settings is admitted",
			TestObject: dataPlane(&eventgatewayv1alpha1.KegDataPlaneConfiguration{
				Runtime: &eventgatewayv1alpha1.RuntimeOptions{DrainDurationSeconds: new(int32(5))},
			}),
		},
	}.RunWithConfig(t, cfg, scheme)
}
