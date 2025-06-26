package controlplane

import (
	"bytes"
	"testing"

	managercfg "github.com/kong/kubernetes-ingress-controller/v3/pkg/manager/config"
	"github.com/stretchr/testify/assert"
	"github.com/tonglil/buflogr"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestWithControllers(t *testing.T) {
	tests := []struct {
		name        string
		controllers []gwtypes.ControlPlaneController
		validate    func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer)
	}{
		{
			name: "enable ingress controllers",
			controllers: []gwtypes.ControlPlaneController{
				{
					Name:  "INGRESS_NETWORKINGV1",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  "INGRESS_CLASS_NETWORKINGV1",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  "INGRESS_CLASS_PARAMETERS",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
			},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
				assert.True(t, cfg.IngressNetV1Enabled)
				assert.True(t, cfg.IngressClassNetV1Enabled)
				assert.True(t, cfg.IngressClassParametersEnabled)
			},
		},
		{
			name: "disable ingress controllers",
			controllers: []gwtypes.ControlPlaneController{
				{
					Name:  "INGRESS_NETWORKINGV1",
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  "INGRESS_CLASS_NETWORKINGV1",
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
			},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
				assert.False(t, cfg.IngressNetV1Enabled)
				assert.False(t, cfg.IngressClassNetV1Enabled)
			},
		},
		{
			name: "enable kong controllers",
			controllers: []gwtypes.ControlPlaneController{
				{
					Name:  "KONG_CLUSTERPLUGIN",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  "KONG_PLUGIN",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  "KONG_CONSUMER",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  "KONG_VAULT",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
			},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
				assert.True(t, cfg.KongClusterPluginEnabled)
				assert.True(t, cfg.KongPluginEnabled)
				assert.True(t, cfg.KongConsumerEnabled)
				assert.True(t, cfg.KongVaultEnabled)
			},
		},
		{
			name: "enable gateway api controllers",
			controllers: []gwtypes.ControlPlaneController{
				{
					Name:  "GWAPI_GATEWAY",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  "GWAPI_HTTPROUTE",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  "GWAPI_GRPCROUTE",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  "GWAPI_REFERENCE_GRANT",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
			},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
				assert.True(t, cfg.GatewayAPIGatewayController)
				assert.True(t, cfg.GatewayAPIHTTPRouteController)
				assert.True(t, cfg.GatewayAPIGRPCRouteController)
				assert.True(t, cfg.GatewayAPIReferenceGrantController)
			},
		},
		{
			name: "deprecated kong controllers",
			controllers: []gwtypes.ControlPlaneController{
				{
					Name:  "KONG_UDPINGRESS",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  "KONG_TCPINGRESS",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  "KONG_INGRESS",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
			},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
				assert.True(t, cfg.UDPIngressEnabled)
				assert.True(t, cfg.TCPIngressEnabled)
				assert.True(t, cfg.KongIngressEnabled)
			},
		},
		{
			name: "unknown controller",
			controllers: []gwtypes.ControlPlaneController{
				{
					Name:  "KONG_PLUGIN",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  "DUMMY_CONTROLLER",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
			},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
				assert.Contains(t, logs.String(), "unknown controller")
				assert.Contains(t, logs.String(), "DUMMY_CONTROLLER")
			},
		},
		{
			name: "mixed enabled and disabled controllers",
			controllers: []gwtypes.ControlPlaneController{
				{
					Name:  "KONG_PLUGIN",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  "KONG_CONSUMER",
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  "SERVICE",
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
			},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
				assert.True(t, cfg.KongPluginEnabled)
				assert.False(t, cfg.KongConsumerEnabled)
				assert.True(t, cfg.ServiceEnabled)
			},
		},
		{
			name:        "empty controllers list",
			controllers: []gwtypes.ControlPlaneController{},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := managercfg.Config{}
			b := bytes.Buffer{}
			logger := buflogr.NewWithBuffer(&b)

			opt := WithControllers(logger, tt.controllers)
			opt(&cfg)

			tt.validate(t, &cfg, &b)
		})
	}
}

func TestWithFeatureGates(t *testing.T) {
	tests := []struct {
		name         string
		featureGates []gwtypes.ControlPlaneFeatureGate
		validate     func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer)
	}{
		{
			name: "enable feature gates",
			featureGates: []gwtypes.ControlPlaneFeatureGate{
				{
					Name:  "FillIDs",
					State: gwtypes.FeatureGateStateEnabled,
				},
				{
					Name:  "RewriteURIs",
					State: gwtypes.FeatureGateStateEnabled,
				},
			},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
				assert.True(t, cfg.FeatureGates["FillIDs"])
				assert.True(t, cfg.FeatureGates["RewriteURIs"])
				assert.Len(t, cfg.FeatureGates, len(managercfg.GetFeatureGatesDefaults()))
			},
		},
		{
			name: "disable feature gates",
			featureGates: []gwtypes.ControlPlaneFeatureGate{
				{
					Name:  "FillIDs",
					State: gwtypes.FeatureGateStateDisabled,
				},
				{
					Name:  "RewriteURIs",
					State: gwtypes.FeatureGateStateDisabled,
				},
			},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
				assert.False(t, cfg.FeatureGates["FillIDs"])
				assert.False(t, cfg.FeatureGates["RewriteURIs"])
				assert.Len(t, cfg.FeatureGates, len(managercfg.GetFeatureGatesDefaults()))
			},
		},
		{
			name: "mixed enabled and disabled feature gates",
			featureGates: []gwtypes.ControlPlaneFeatureGate{
				{
					Name:  "FillIDs",
					State: gwtypes.FeatureGateStateEnabled,
				},
				{
					Name:  "RewriteURIs",
					State: gwtypes.FeatureGateStateDisabled,
				},
				{
					Name:  "FallbackConfiguration",
					State: gwtypes.FeatureGateStateEnabled,
				},
			},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
				assert.True(t, cfg.FeatureGates["FillIDs"])
				assert.False(t, cfg.FeatureGates["RewriteURIs"])
				assert.True(t, cfg.FeatureGates["FallbackConfiguration"])
				assert.Len(t, cfg.FeatureGates, len(managercfg.GetFeatureGatesDefaults()))
			},
		},
		{
			name: "single feature gate enabled",
			featureGates: []gwtypes.ControlPlaneFeatureGate{
				{
					Name:  "FillIDs",
					State: gwtypes.FeatureGateStateEnabled,
				},
			},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
				assert.True(t, cfg.FeatureGates["FillIDs"])
				assert.Len(t, cfg.FeatureGates, len(managercfg.GetFeatureGatesDefaults()))
			},
		},
		{
			name: "single feature gate disabled",
			featureGates: []gwtypes.ControlPlaneFeatureGate{
				{
					Name:  "FillIDs",
					State: gwtypes.FeatureGateStateDisabled,
				},
			},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
				assert.False(t, cfg.FeatureGates["FillIDs"])
				assert.Len(t, cfg.FeatureGates, len(managercfg.GetFeatureGatesDefaults()))
			},
		},
		{
			name: "duplicate feature gate names",
			featureGates: []gwtypes.ControlPlaneFeatureGate{
				{
					Name:  "FillIDs",
					State: gwtypes.FeatureGateStateEnabled,
				},
				{
					Name:  "FillIDs",
					State: gwtypes.FeatureGateStateDisabled,
				},
			},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
				assert.True(t, cfg.FeatureGates["FillIDs"])
				assert.Len(t, cfg.FeatureGates, len(managercfg.GetFeatureGatesDefaults()))
				assert.Contains(t, logs.String(), "feature gate already set")
				assert.Contains(t, logs.String(), "FillIDs")
			},
		},
		{
			name: "unknown feature gate",
			featureGates: []gwtypes.ControlPlaneFeatureGate{
				{
					Name:  "UnknownFeature",
					State: gwtypes.FeatureGateStateEnabled,
				},
			},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
				assert.Contains(t, logs.String(), "unknown feature gate")
				assert.Contains(t, logs.String(), "UnknownFeature")
			},
		},
		{
			name:         "nothing specified, should use defaults",
			featureGates: []gwtypes.ControlPlaneFeatureGate{},
			validate: func(t *testing.T, cfg *managercfg.Config, logs *bytes.Buffer) {
				assert.Equal(t, managercfg.GetFeatureGatesDefaults(), cfg.FeatureGates)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &managercfg.Config{}
			b := bytes.Buffer{}
			logger := buflogr.NewWithBuffer(&b)

			opt := WithFeatureGates(logger, tt.featureGates)
			opt(cfg)

			tt.validate(t, cfg, &b)
		})
	}
}
