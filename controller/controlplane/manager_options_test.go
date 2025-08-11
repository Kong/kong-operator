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

	operatorv2beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2beta1"

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
		options          *gwtypes.ControlPlaneDataPlaneSync
		expectedInterval time.Duration
		expectedTimeout  time.Duration
	}{
		{
			name:             "empty value sets default interval and timeout",
			options:          &gwtypes.ControlPlaneDataPlaneSync{},
			expectedInterval: 3 * time.Second,
			expectedTimeout:  30 * time.Second,
		},
		{
			name: "only specify interval",
			options: &gwtypes.ControlPlaneDataPlaneSync{
				Interval: &metav1.Duration{
					Duration: 5 * time.Second,
				},
			},
			expectedInterval: 5 * time.Second,
			expectedTimeout:  30 * time.Second,
		},
		{
			name: "only specify timeout",
			options: &gwtypes.ControlPlaneDataPlaneSync{
				Timeout: &metav1.Duration{
					Duration: time.Minute,
				},
			},
			expectedInterval: 3 * time.Second,
			expectedTimeout:  time.Minute,
		},
		{
			name: "specify both interval and timeout",
			options: &gwtypes.ControlPlaneDataPlaneSync{
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
		name   string
		opts   *gwtypes.ControlPlaneTranslationOptions
		assert func(t *testing.T, cfg *managercfg.Config)
	}{
		{
			name: "nil options should not modify config",
			opts: nil,
			assert: func(t *testing.T, cfg *managercfg.Config) {
				assert.False(t, cfg.CombinedServicesFromDifferentHTTPRoutes) // default value
				assert.False(t, cfg.UseLastValidConfigForFallback)           // default value
			},
		},
		{
			name: "options with nil CombinedServicesFromDifferentHTTPRoutes should not modify config",
			opts: &gwtypes.ControlPlaneTranslationOptions{
				CombinedServicesFromDifferentHTTPRoutes: nil,
			},
			assert: func(t *testing.T, cfg *managercfg.Config) {
				assert.False(t, cfg.CombinedServicesFromDifferentHTTPRoutes) // default value
			},
		},
		{
			name: "options with CombinedServicesFromDifferentHTTPRoutes enabled",
			opts: &gwtypes.ControlPlaneTranslationOptions{
				CombinedServicesFromDifferentHTTPRoutes: lo.ToPtr(gwtypes.ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateEnabled),
				FallbackConfiguration: &gwtypes.ControlPlaneFallbackConfiguration{
					UseLastValidConfig: lo.ToPtr(gwtypes.ControlPlaneFallbackConfigurationStateEnabled),
				},
			},
			assert: func(t *testing.T, cfg *managercfg.Config) {
				assert.True(t, cfg.CombinedServicesFromDifferentHTTPRoutes)
				assert.True(t, cfg.UseLastValidConfigForFallback)
			},
		},
		{
			name: "disabled options",
			opts: &gwtypes.ControlPlaneTranslationOptions{
				CombinedServicesFromDifferentHTTPRoutes: lo.ToPtr(gwtypes.ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateDisabled),
				FallbackConfiguration: &gwtypes.ControlPlaneFallbackConfiguration{
					UseLastValidConfig: lo.ToPtr(gwtypes.ControlPlaneFallbackConfigurationStateDisabled),
				},
			},
			assert: func(t *testing.T, cfg *managercfg.Config) {
				assert.False(t, cfg.CombinedServicesFromDifferentHTTPRoutes)
			},
		},
		{
			name: "options with DrainSupport enabled",
			opts: &gwtypes.ControlPlaneTranslationOptions{
				DrainSupport: lo.ToPtr(gwtypes.ControlPlaneDrainSupportStateEnabled),
			},
			assert: func(t *testing.T, cfg *managercfg.Config) {
				assert.True(t, cfg.EnableDrainSupport)
				assert.False(t, cfg.UseLastValidConfigForFallback)
			},
		},
		{
			name: "options with DrainSupport disabled",
			opts: &gwtypes.ControlPlaneTranslationOptions{
				DrainSupport: lo.ToPtr(gwtypes.ControlPlaneDrainSupportStateDisabled),
			},
			assert: func(t *testing.T, cfg *managercfg.Config) {
				assert.False(t, cfg.EnableDrainSupport)
				assert.False(t, cfg.UseLastValidConfigForFallback)
			},
		},
		{
			name: "options with both CombinedServicesFromDifferentHTTPRoutes and DrainSupport enabled",
			opts: &gwtypes.ControlPlaneTranslationOptions{
				CombinedServicesFromDifferentHTTPRoutes: lo.ToPtr(gwtypes.ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateEnabled),
				DrainSupport:                            lo.ToPtr(gwtypes.ControlPlaneDrainSupportStateEnabled),
			},
			assert: func(t *testing.T, cfg *managercfg.Config) {
				assert.True(t, cfg.CombinedServicesFromDifferentHTTPRoutes)
				assert.True(t, cfg.EnableDrainSupport)
				assert.False(t, cfg.UseLastValidConfigForFallback)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &managercfg.Config{}
			opt := WithTranslationOptions(tc.opts)
			opt(cfg)
			tc.assert(t, cfg)
		})
	}
}

func TestWithSecretLabelSelectorMatchLabel(t *testing.T) {
	cfg := &managercfg.Config{}
	opt := WithSecretLabelSelectorMatchLabel("konghq.com/secret", "true")
	opt(cfg)
	assert.Equal(t, map[string]string{
		"konghq.com/secret": "true",
	}, cfg.SecretLabelSelector)
}

func TestConfigMapLabelSelector(t *testing.T) {
	cfg := &managercfg.Config{}
	opt := WithConfigMapLabelSelectorMatchLabel("konghq.com/configmap", "true")
	opt(cfg)
	assert.Equal(t, map[string]string{
		"konghq.com/configmap": "true",
	}, cfg.ConfigMapLabelSelector)
}

func TestWithKonnectOptions(t *testing.T) {
	tests := []struct {
		name                  string
		konnectOptions        *operatorv2beta1.ControlPlaneKonnectOptions
		existingKonnectConfig *managercfg.KonnectConfig
		expectedConfig        managercfg.KonnectConfig
	}{
		{
			name:           "nil konnect options should not modify config",
			konnectOptions: nil,
			existingKonnectConfig: &managercfg.KonnectConfig{
				ConfigSynchronizationEnabled: true,
				ControlPlaneID:               "test-cp-id",
			},
			expectedConfig: managercfg.KonnectConfig{
				ConfigSynchronizationEnabled: true,
				ControlPlaneID:               "test-cp-id",
			},
		},
		{
			name: "should configure consumer sync disabled",
			konnectOptions: &operatorv2beta1.ControlPlaneKonnectOptions{
				ConsumersSync: func() *operatorv2beta1.ControlPlaneKonnectConsumersSyncState {
					state := operatorv2beta1.ControlPlaneKonnectConsumersSyncStateDisabled
					return &state
				}(),
			},
			existingKonnectConfig: &managercfg.KonnectConfig{},
			expectedConfig: managercfg.KonnectConfig{
				ConsumersSyncDisabled: true,
			},
		},
		{
			name: "should configure consumer sync enabled",
			konnectOptions: &operatorv2beta1.ControlPlaneKonnectOptions{
				ConsumersSync: func() *operatorv2beta1.ControlPlaneKonnectConsumersSyncState {
					state := operatorv2beta1.ControlPlaneKonnectConsumersSyncStateEnabled
					return &state
				}(),
			},
			existingKonnectConfig: &managercfg.KonnectConfig{},
			expectedConfig: managercfg.KonnectConfig{
				ConsumersSyncDisabled: false,
			},
		},
		{
			name: "should configure licensing options",
			konnectOptions: &operatorv2beta1.ControlPlaneKonnectOptions{
				Licensing: &operatorv2beta1.ControlPlaneKonnectLicensing{
					State: func() *operatorv2beta1.ControlPlaneKonnectLicensingState {
						state := operatorv2beta1.ControlPlaneKonnectLicensingStateEnabled
						return &state
					}(),
					InitialPollingPeriod: &metav1.Duration{Duration: 5 * time.Minute},
					PollingPeriod:        &metav1.Duration{Duration: 10 * time.Minute},
					StorageState: func() *operatorv2beta1.ControlPlaneKonnectLicensingState {
						state := operatorv2beta1.ControlPlaneKonnectLicensingStateEnabled
						return &state
					}(),
				},
			},
			existingKonnectConfig: &managercfg.KonnectConfig{},
			expectedConfig: managercfg.KonnectConfig{
				LicenseSynchronizationEnabled: true,
				InitialLicensePollingPeriod:   5 * time.Minute,
				LicensePollingPeriod:          10 * time.Minute,
				LicenseStorageEnabled:         true,
			},
		},
		{
			name: "should configure node refresh and config upload periods",
			konnectOptions: &operatorv2beta1.ControlPlaneKonnectOptions{
				NodeRefreshPeriod:  &metav1.Duration{Duration: 15 * time.Second},
				ConfigUploadPeriod: &metav1.Duration{Duration: 30 * time.Second},
			},
			existingKonnectConfig: &managercfg.KonnectConfig{},
			expectedConfig: managercfg.KonnectConfig{
				RefreshNodePeriod:  15 * time.Second,
				UploadConfigPeriod: 30 * time.Second,
			},
		},
		{
			name: "should merge with existing config",
			konnectOptions: &operatorv2beta1.ControlPlaneKonnectOptions{
				ConsumersSync: func() *operatorv2beta1.ControlPlaneKonnectConsumersSyncState {
					state := operatorv2beta1.ControlPlaneKonnectConsumersSyncStateDisabled
					return &state
				}(),
			},
			existingKonnectConfig: &managercfg.KonnectConfig{
				ConfigSynchronizationEnabled: true,
				ControlPlaneID:               "existing-cp-id",
				Address:                      "https://konnect.konghq.com",
			},
			expectedConfig: managercfg.KonnectConfig{
				ConfigSynchronizationEnabled: true,
				ControlPlaneID:               "existing-cp-id",
				Address:                      "https://konnect.konghq.com",
				ConsumersSyncDisabled:        true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &managercfg.Config{}
			opt := WithKonnectOptions(tt.konnectOptions, tt.existingKonnectConfig)
			opt(cfg)
			assert.Equal(t, tt.expectedConfig, cfg.Konnect)
		})
	}
}
