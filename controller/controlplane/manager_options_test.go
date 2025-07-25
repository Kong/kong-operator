package controlplane

import (
	"bytes"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tonglil/buflogr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv2alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2alpha1"

	"github.com/kong/kong-operator/ingress-controller/pkg/manager"
	managercfg "github.com/kong/kong-operator/ingress-controller/pkg/manager/config"
	"github.com/kong/kong-operator/internal/telemetry"
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

func TestManagerConfigToStatusControllers(t *testing.T) {
	tests := []struct {
		name     string
		config   managercfg.Config
		expected []gwtypes.ControlPlaneController
	}{
		{
			name: "all controllers disabled",
			config: managercfg.Config{
				IngressNetV1Enabled:                false,
				IngressClassNetV1Enabled:           false,
				IngressClassParametersEnabled:      false,
				UDPIngressEnabled:                  false,
				TCPIngressEnabled:                  false,
				KongIngressEnabled:                 false,
				KongClusterPluginEnabled:           false,
				KongPluginEnabled:                  false,
				KongConsumerEnabled:                false,
				KongUpstreamPolicyEnabled:          false,
				KongServiceFacadeEnabled:           false,
				KongVaultEnabled:                   false,
				KongLicenseEnabled:                 false,
				KongCustomEntityEnabled:            false,
				ServiceEnabled:                     false,
				GatewayAPIGatewayController:        false,
				GatewayAPIHTTPRouteController:      false,
				GatewayAPIGRPCRouteController:      false,
				GatewayAPIReferenceGrantController: false,
			},
			expected: []gwtypes.ControlPlaneController{
				{
					Name:  ControllerNameIngress,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameIngressClass,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameIngressClassParameters,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongUDPIngress,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongTCPIngress,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongIngress,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongClusterPlugin,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongPlugin,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongConsumer,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongUpstreamPolicy,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongServiceFacade,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongVault,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongLicense,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongCustomEntity,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameService,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameGatewayAPIGateway,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameGatewayAPIHTTPRoute,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameGatewayAPIGRPCRoute,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameGatewayAPIReferenceGrant,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
			},
		},
		{
			name: "ingress controllers enabled",
			config: managercfg.Config{
				IngressNetV1Enabled:           true,
				IngressClassNetV1Enabled:      true,
				IngressClassParametersEnabled: true,
			},
			expected: []gwtypes.ControlPlaneController{
				{
					Name:  ControllerNameIngress,
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  ControllerNameIngressClass,
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  ControllerNameIngressClassParameters,
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  ControllerNameKongUDPIngress,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongTCPIngress,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongIngress,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongClusterPlugin,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongPlugin,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongConsumer,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongUpstreamPolicy,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongServiceFacade,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongVault,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongLicense,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongCustomEntity,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameService,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameGatewayAPIGateway,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameGatewayAPIHTTPRoute,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameGatewayAPIGRPCRoute,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameGatewayAPIReferenceGrant,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
			},
		},
		{
			name: "single controller enabled",
			config: managercfg.Config{
				KongVaultEnabled: true,
			},
			expected: []gwtypes.ControlPlaneController{
				{
					Name:  ControllerNameIngress,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameIngressClass,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameIngressClassParameters,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongUDPIngress,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongTCPIngress,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongIngress,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongClusterPlugin,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongPlugin,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongConsumer,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongUpstreamPolicy,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongServiceFacade,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongVault,
					State: gwtypes.ControlPlaneControllerStateEnabled,
				},
				{
					Name:  ControllerNameKongLicense,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameKongCustomEntity,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameService,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameGatewayAPIGateway,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameGatewayAPIHTTPRoute,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameGatewayAPIGRPCRoute,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
				{
					Name:  ControllerNameGatewayAPIReferenceGrant,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := managerConfigToStatusControllers(tt.config)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestManagerConfigToStatusFeatureGates(t *testing.T) {
	tests := []struct {
		name     string
		config   managercfg.Config
		expected []gwtypes.ControlPlaneFeatureGate
	}{
		{
			name: "empty feature gates",
			config: managercfg.Config{
				FeatureGates: managercfg.FeatureGates{},
			},
			expected: []gwtypes.ControlPlaneFeatureGate{},
		},
		{
			name: "single feature gate enabled",
			config: managercfg.Config{
				FeatureGates: managercfg.FeatureGates{
					"FillIDs": true,
				},
			},
			expected: []gwtypes.ControlPlaneFeatureGate{
				{
					Name:  "FillIDs",
					State: gwtypes.FeatureGateStateEnabled,
				},
			},
		},
		{
			name: "single feature gate disabled",
			config: managercfg.Config{
				FeatureGates: managercfg.FeatureGates{
					"FillIDs": false,
				},
			},
			expected: []gwtypes.ControlPlaneFeatureGate{
				{
					Name:  "FillIDs",
					State: gwtypes.FeatureGateStateDisabled,
				},
			},
		},
		{
			name: "multiple feature gates mixed states",
			config: managercfg.Config{
				FeatureGates: managercfg.FeatureGates{
					"FillIDs":               true,
					"RewriteURIs":           false,
					"FallbackConfiguration": true,
					"KongServiceFacade":     false,
				},
			},
			expected: []gwtypes.ControlPlaneFeatureGate{
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
				{
					Name:  "KongServiceFacade",
					State: gwtypes.FeatureGateStateDisabled,
				},
			},
		},
		{
			name: "all feature gates enabled",
			config: managercfg.Config{
				FeatureGates: managercfg.FeatureGates{
					"FillIDs":               true,
					"RewriteURIs":           true,
					"FallbackConfiguration": true,
				},
			},
			expected: []gwtypes.ControlPlaneFeatureGate{
				{
					Name:  "FillIDs",
					State: gwtypes.FeatureGateStateEnabled,
				},
				{
					Name:  "RewriteURIs",
					State: gwtypes.FeatureGateStateEnabled,
				},
				{
					Name:  "FallbackConfiguration",
					State: gwtypes.FeatureGateStateEnabled,
				},
			},
		},
		{
			name: "all feature gates disabled",
			config: managercfg.Config{
				FeatureGates: managercfg.FeatureGates{
					"FillIDs":               false,
					"RewriteURIs":           false,
					"FallbackConfiguration": false,
				},
			},
			expected: []gwtypes.ControlPlaneFeatureGate{
				{
					Name:  "FillIDs",
					State: gwtypes.FeatureGateStateDisabled,
				},
				{
					Name:  "RewriteURIs",
					State: gwtypes.FeatureGateStateDisabled,
				},
				{
					Name:  "FallbackConfiguration",
					State: gwtypes.FeatureGateStateDisabled,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := managerConfigToStatusFeatureGates(tt.config)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestWithAnonymousReports(t *testing.T) {
	testCases := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "anonymous reports enabled",
			enabled:  true,
			expected: true,
		},
		{
			name:     "anonymous reports disabled",
			enabled:  false,
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &managercfg.Config{}
			opt := WithAnonymousReports(tc.enabled)
			opt(cfg)
			assert.Equal(t, tc.expected, cfg.AnonymousReports)
		})
	}
}

func TestWithAnonymousReportsFixedPayloadCustomizer(t *testing.T) {
	called := false
	mockCustomizer := func(payload telemetry.Payload) telemetry.Payload {
		called = true
		payload["test"] = "value"
		return payload
	}

	cfg := &managercfg.Config{}
	opt := WithAnonymousReportsFixedPayloadCustomizer(mockCustomizer)
	opt(cfg)

	require.NotNil(t, cfg.AnonymousReportsFixedPayloadCustomizer)
	testPayload := telemetry.Payload{}
	result := cfg.AnonymousReportsFixedPayloadCustomizer(testPayload)
	assert.True(t, called)
	assert.Equal(t, "value", result["test"])
}

func TestWithIngressClass(t *testing.T) {
	testCases := []struct {
		name         string
		ingressClass *string
		expected     string
	}{
		{
			name:         "ingress class set to non-empty string",
			ingressClass: lo.ToPtr("kong"),
			expected:     "kong",
		},
		{
			name:         "ingress class set to empty string",
			ingressClass: lo.ToPtr(""),
			expected:     "", // Should remain empty/default
		},
		{
			name:         "ingress class is nil",
			ingressClass: nil,
			expected:     "", // Should remain empty/default
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &managercfg.Config{}
			opt := WithIngressClass(tc.ingressClass)
			opt(cfg)
			assert.Equal(t, tc.expected, cfg.IngressClassName)
		})
	}
}

func TestWithGatewayDiscoveryReadinessCheckInterval(t *testing.T) {
	testCases := []struct {
		name     string
		interval *metav1.Duration
		expected time.Duration
	}{
		{
			name:     "with nil interval uses default",
			interval: nil,
			expected: managercfg.DefaultDataPlanesReadinessReconciliationInterval,
		},
		{
			name: "with custom interval",
			interval: &metav1.Duration{
				Duration: 30 * time.Second,
			},
			expected: 30 * time.Second,
		},
		{
			name: "with zero interval",
			interval: &metav1.Duration{
				Duration: 0,
			},
			expected: 0,
		},
		{
			name: "with large interval",
			interval: &metav1.Duration{
				Duration: 10 * time.Minute,
			},
			expected: 10 * time.Minute,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &managercfg.Config{}
			opt := WithGatewayDiscoveryReadinessCheckInterval(tc.interval)
			opt(cfg)
			assert.Equal(t, tc.expected, cfg.GatewayDiscoveryReadinessCheckInterval)
		})
	}
}

func TestWithGatewayDiscoveryReadinessCheckTimeout(t *testing.T) {
	testCases := []struct {
		name     string
		timeout  *metav1.Duration
		expected time.Duration
	}{
		{
			name:     "with nil timeout uses default",
			timeout:  nil,
			expected: managercfg.DefaultDataPlanesReadinessCheckTimeout,
		},
		{
			name: "with custom timeout",
			timeout: &metav1.Duration{
				Duration: 45 * time.Second,
			},
			expected: 45 * time.Second,
		},
		{
			name: "with zero timeout",
			timeout: &metav1.Duration{
				Duration: 0,
			},
			expected: 0,
		},
		{
			name: "with large timeout",
			timeout: &metav1.Duration{
				Duration: 5 * time.Minute,
			},
			expected: 5 * time.Minute,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &managercfg.Config{}
			opt := WithGatewayDiscoveryReadinessCheckTimeout(tc.timeout)
			opt(cfg)
			assert.Equal(t, tc.expected, cfg.GatewayDiscoveryReadinessCheckTimeout)
		})
	}
}

func TestWithDataPlaneSyncOptions(t *testing.T) {
	testCases := []struct {
		name             string
		options          *operatorv2alpha1.ControlPlaneDataPlaneSync
		expectedInterval time.Duration
		expectedTimeout  time.Duration
	}{
		{
			name:             "empty value sets default interval and timeout",
			options:          &operatorv2alpha1.ControlPlaneDataPlaneSync{},
			expectedInterval: 3 * time.Second,
			expectedTimeout:  30 * time.Second,
		},
		{
			name: "only specify interval",
			options: &operatorv2alpha1.ControlPlaneDataPlaneSync{
				Interval: &metav1.Duration{
					Duration: 5 * time.Second,
				},
			},
			expectedInterval: 5 * time.Second,
			expectedTimeout:  30 * time.Second,
		},
		{
			name: "only specify timeout",
			options: &operatorv2alpha1.ControlPlaneDataPlaneSync{
				Timeout: &metav1.Duration{
					Duration: time.Minute,
				},
			},
			expectedInterval: 3 * time.Second,
			expectedTimeout:  time.Minute,
		},
		{
			name: "specify both interval and timeout",
			options: &operatorv2alpha1.ControlPlaneDataPlaneSync{
				Interval: &metav1.Duration{
					Duration: 10 * time.Second,
				},
				Timeout: &metav1.Duration{
					Duration: time.Minute,
				},
			},
			expectedInterval: 10 * time.Second,
			expectedTimeout:  time.Minute,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := manager.NewConfig(
				WithDataPlaneSyncOptions(*tc.options),
			)
			require.NoError(t, err)
			require.Equal(t, tc.expectedInterval, cfg.ProxySyncInterval)
			require.Equal(t, tc.expectedTimeout, cfg.ProxySyncTimeout)
		})
	}
}

func TestWithClusterDomain(t *testing.T) {
	cfg := &managercfg.Config{}
	opt := WithClusterDomain("foo.bar")
	opt(cfg)
	assert.Equal(t, cfg.ClusterDomain, "foo.bar")
}

func TestWithEmitKubernetesEvents(t *testing.T) {
	testCases := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "emit kubernetes events enabled",
			enabled:  true,
			expected: true,
		},
		{
			name:     "emit kubernetes events disabled",
			enabled:  false,
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &managercfg.Config{}
			opt := WithEmitKubernetesEvents(tc.enabled)
			opt(cfg)
			assert.Equal(t, tc.expected, cfg.EmitKubernetesEvents)
		})
	}
}

func TestWithTranslationOptions(t *testing.T) {
	testCases := []struct {
		name     string
		opts     *operatorv2alpha1.ControlPlaneTranslationOptions
		expected bool
	}{
		{
			name:     "nil options should not modify config",
			opts:     nil,
			expected: false, // default value
		},
		{
			name: "options with nil CombinedServicesFromDifferentHTTPRoutes should not modify config",
			opts: &operatorv2alpha1.ControlPlaneTranslationOptions{
				CombinedServicesFromDifferentHTTPRoutes: nil,
			},
			expected: false, // default value
		},
		{
			name: "options with CombinedServicesFromDifferentHTTPRoutes enabled",
			opts: &operatorv2alpha1.ControlPlaneTranslationOptions{
				CombinedServicesFromDifferentHTTPRoutes: lo.ToPtr(operatorv2alpha1.ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateEnabled),
			},
			expected: true,
		},
		{
			name: "options with CombinedServicesFromDifferentHTTPRoutes disabled",
			opts: &operatorv2alpha1.ControlPlaneTranslationOptions{
				CombinedServicesFromDifferentHTTPRoutes: lo.ToPtr(operatorv2alpha1.ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateDisabled),
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &managercfg.Config{}
			opt := WithTranslationOptions(tc.opts)
			opt(cfg)
			assert.Equal(t, tc.expected, cfg.CombinedServicesFromDifferentHTTPRoutes)
		})
	}
}
