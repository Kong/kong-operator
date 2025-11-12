package v1beta1_test

import (
	"errors"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	operatorv2beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2beta1"
)

func TestGatewayConfiguration_ConvertTo(t *testing.T) {
	t.Run("error: wrong hub type", func(t *testing.T) {
		obj := &operatorv1beta1.GatewayConfiguration{}
		testConversionError(t, func() error {
			return obj.ConvertTo(&dummyHub{})
		}, "GatewayConfiguration ConvertTo: expected *operatorv2beta1.GatewayConfiguration, got %T")
	})

	cases := []struct {
		name                     string
		spec                     operatorv1beta1.GatewayConfigurationSpec
		expectedDataPlane        *operatorv2beta1.GatewayConfigDataPlaneOptions
		expectedControlPlane     *operatorv2beta1.GatewayConfigControlPlaneOptions
		expectedListenersOptions []operatorv2beta1.GatewayConfigurationListenerOptions
		expectedExtensions       []commonv1alpha1.ExtensionRef
		expectedError            error
	}{
		{
			name: "empty spec",
			spec: operatorv1beta1.GatewayConfigurationSpec{},
		},
		{
			name: "DataPlane Options",
			spec: operatorv1beta1.GatewayConfigurationSpec{
				DataPlaneOptions: &operatorv1beta1.GatewayConfigDataPlaneOptions{
					Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1beta1.DeploymentOptions{
							Replicas: lo.ToPtr(int32(2)),
						},
					},
				},
			},
			expectedDataPlane: &operatorv2beta1.GatewayConfigDataPlaneOptions{
				Deployment: operatorv2beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv2beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(2)),
					},
				},
			},
		},
		{
			name: "ControlPlane IngressClass Option",
			spec: operatorv1beta1.GatewayConfigurationSpec{
				ControlPlaneOptions: &operatorv1beta1.ControlPlaneOptions{
					Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "controller",
										Image: "kong/controller:latest",
										Env: []corev1.EnvVar{
											{
												Name:  "CONTROLLER_INGRESS_CLASS",
												Value: "kong",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedControlPlane: &operatorv2beta1.GatewayConfigControlPlaneOptions{
				ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
					IngressClass: lo.ToPtr("kong"),
				},
			},
		},
		{
			name: "DataPlane and ControlPlane Options",
			spec: operatorv1beta1.GatewayConfigurationSpec{
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: "test-extension",
						},
						Group: "test.group",
						Kind:  "Konnect",
					},
				},
				DataPlaneOptions: &operatorv1beta1.GatewayConfigDataPlaneOptions{
					Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1beta1.DeploymentOptions{
							Replicas: lo.ToPtr(int32(2)),
						},
					},
					Extensions: []commonv1alpha1.ExtensionRef{
						{
							NamespacedRef: commonv1alpha1.NamespacedRef{
								Name: "test-extension",
							},
							Group: "test.group",
							Kind:  "Konnect",
						},
					},
				},
				ControlPlaneOptions: &operatorv1beta1.ControlPlaneOptions{
					Extensions: []commonv1alpha1.ExtensionRef{
						{
							NamespacedRef: commonv1alpha1.NamespacedRef{
								Name: "extension-1",
							},
							Group: "test.group",
							Kind:  "Test1",
						},
						{
							NamespacedRef: commonv1alpha1.NamespacedRef{
								Name: "extension-2",
							},
							Group: "test.group",
							Kind:  "Test2",
						},
					},
					Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "controller",
										Image: "kong/controller:latest",
										Env: []corev1.EnvVar{
											{
												Name:  "CONTROLLER_FEATURE_GATES",
												Value: "GatewayAlpha=true,ExperimentalFeature=false",
											},
											{
												Name:  "CONTROLLER_ENABLE_CONTROLLER_INGRESS_CLASS_NETWORKINGV1",
												Value: "true",
											},
											{
												Name:  "CONTROLLER_ENABLE_CONTROLLER_KONG_PLUGIN",
												Value: "false",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedDataPlane: &operatorv2beta1.GatewayConfigDataPlaneOptions{
				Deployment: operatorv2beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv2beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(2)),
					},
				},
			},
			expectedControlPlane: &operatorv2beta1.GatewayConfigControlPlaneOptions{
				ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
					FeatureGates: []operatorv2beta1.ControlPlaneFeatureGate{
						{Name: "GatewayAlpha", State: operatorv2beta1.FeatureGateStateEnabled},
						{Name: "ExperimentalFeature", State: operatorv2beta1.FeatureGateStateDisabled},
					},
					Controllers: []operatorv2beta1.ControlPlaneController{
						{Name: "INGRESS_CLASS_NETWORKINGV1", State: operatorv2beta1.ControllerStateEnabled},
						{Name: "KONG_PLUGIN", State: operatorv2beta1.ControllerStateDisabled},
					},
				},
			},
			expectedExtensions: []commonv1alpha1.ExtensionRef{
				{
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "extension-1",
					},
					Group: "test.group",
					Kind:  "Test1",
				},
				{
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "extension-2",
					},
					Group: "test.group",
					Kind:  "Test2",
				},
			},
		},
		{
			name: "With ListenersOptions",
			spec: operatorv1beta1.GatewayConfigurationSpec{
				ListenersOptions: []operatorv1beta1.GatewayConfigurationListenerOptions{
					{
						Name:     "listener-1",
						NodePort: int32(3000),
					},
				},
			},
			expectedListenersOptions: []operatorv2beta1.GatewayConfigurationListenerOptions{
				{
					Name:     "listener-1",
					NodePort: int32(3000),
				},
			},
		},
		{
			name: "With ControlPlane's Dataplane option set",
			spec: operatorv1beta1.GatewayConfigurationSpec{
				DataPlaneOptions: &operatorv1beta1.GatewayConfigDataPlaneOptions{
					Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1beta1.DeploymentOptions{
							Replicas: lo.ToPtr(int32(2)),
						},
					},
				},
				ControlPlaneOptions: &operatorv1beta1.ControlPlaneOptions{
					DataPlane: lo.ToPtr("test-data-plane"),
				},
			},
			expectedError: errors.New("GatewayConfiguration ConvertTo: ControlPlaneOptions.DataPlane is not supported"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj := &operatorv1beta1.GatewayConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
				},
				Spec: tc.spec,
				Status: operatorv1beta1.GatewayConfigurationStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "AllGood",
						},
					},
				},
			}

			dst := &operatorv2beta1.GatewayConfiguration{}
			err := obj.ConvertTo(dst)
			if tc.expectedError != nil {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedError.Error())
				return
			}
			require.NoError(t, err)

			require.Equal(t, obj.ObjectMeta, dst.ObjectMeta)
			if tc.expectedControlPlane != nil {
				require.Equal(t, tc.expectedControlPlane.IngressClass, dst.Spec.ControlPlaneOptions.IngressClass)

				if tc.expectedControlPlane.WatchNamespaces != nil {
					require.NotNil(t, dst.Spec.ControlPlaneOptions.WatchNamespaces)
					require.Equal(t, tc.expectedControlPlane.WatchNamespaces.Type, dst.Spec.ControlPlaneOptions.WatchNamespaces.Type)
					require.Equal(t, tc.expectedControlPlane.WatchNamespaces.List, dst.Spec.ControlPlaneOptions.WatchNamespaces.List)
				} else {
					require.Empty(t, dst.Spec.ControlPlaneOptions.WatchNamespaces)
				}

				if tc.expectedControlPlane.FeatureGates != nil {
					require.ElementsMatch(t, tc.expectedControlPlane.FeatureGates, dst.Spec.ControlPlaneOptions.FeatureGates)
				}

				if tc.expectedControlPlane.Controllers != nil {
					require.ElementsMatch(t, tc.expectedControlPlane.Controllers, dst.Spec.ControlPlaneOptions.Controllers)
				}
			}
			require.Equal(t, tc.expectedDataPlane, dst.Spec.DataPlaneOptions)
			require.Equal(t, tc.expectedListenersOptions, dst.Spec.ListenersOptions)
			require.Equal(t, tc.expectedExtensions, dst.Spec.Extensions)
			require.Equal(t, obj.Status.Conditions, dst.Status.Conditions)
		})
	}
}

func TestGatewayConfiguration_ConvertFrom(t *testing.T) {
	t.Run("error: wrong hub type", func(t *testing.T) {
		obj := &operatorv1beta1.GatewayConfiguration{}
		testConversionError(t, func() error {
			return obj.ConvertFrom(&dummyHub{})
		}, "GatewayConfiguration ConvertFrom: expected *operatorv2beta1.GatewayConfiguration, got %T")
	})

	cases := []struct {
		name                     string
		spec                     operatorv2beta1.GatewayConfigurationSpec
		expectedDataPlane        *operatorv1beta1.GatewayConfigDataPlaneOptions
		expectedControlPlane     *operatorv1beta1.ControlPlaneOptions
		expectedListenersOptions []operatorv2beta1.GatewayConfigurationListenerOptions
		expectedExtensions       []commonv1alpha1.ExtensionRef
		expectedError            error
	}{
		{
			name: "Full fledged case",
			spec: operatorv2beta1.GatewayConfigurationSpec{
				ControlPlaneOptions: &operatorv2beta1.GatewayConfigControlPlaneOptions{
					ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
						IngressClass: lo.ToPtr("kong"),
						WatchNamespaces: &operatorv2beta1.WatchNamespaces{
							Type: operatorv2beta1.WatchNamespacesTypeAll,
						},
						Controllers: []operatorv2beta1.ControlPlaneController{
							{Name: "FEATURE_A", State: operatorv2beta1.ControllerStateEnabled},
							{Name: "FEATURE_B", State: operatorv2beta1.ControllerStateDisabled},
						},
						FeatureGates: []operatorv2beta1.ControlPlaneFeatureGate{
							{Name: "GatewayAlpha", State: operatorv2beta1.FeatureGateStateEnabled},
							{Name: "ExperimentalFeature", State: operatorv2beta1.FeatureGateStateDisabled},
						},
					},
				},
				DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
					Deployment: operatorv2beta1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv2beta1.DeploymentOptions{
							Replicas: lo.ToPtr(int32(2)),
						},
					},
				},
				ListenersOptions: []operatorv2beta1.GatewayConfigurationListenerOptions{
					{
						Name:     "listener-1",
						NodePort: int32(3000),
					},
				},
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						Group: "konnect.konghq.com",
						Kind:  "KonnectExtension",
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: "konnect-extension",
						},
					},
					{
						Group: "gateway-operator.konghq.com",
						Kind:  "DataPlaneMetricsExtension",
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: "metrics-extension",
						},
					},
				},
			},
			expectedDataPlane: &operatorv1beta1.GatewayConfigDataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(2)),
					},
				},
			},
			expectedControlPlane: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "controller",
									Image: "kong/controller:latest",
									Env: []corev1.EnvVar{
										{
											Name:  "CONTROLLER_FEATURE_GATES",
											Value: "GatewayAlpha=true,ExperimentalFeature=false",
										},
										{
											Name:  "CONTROLLER_ENABLE_CONTROLLER_FEATURE_A",
											Value: "true",
										},
										{
											Name:  "CONTROLLER_ENABLE_CONTROLLER_FEATURE_B",
											Value: "false",
										},
										{
											Name:  "CONTROLLER_INGRESS_CLASS",
											Value: "kong",
										},
									},
								},
							},
						},
					},
				},
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						Group: "gateway-operator.konghq.com",
						Kind:  "DataPlaneMetricsExtension",
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: "metrics-extension",
						},
					},
				},
			},
			expectedExtensions: []commonv1alpha1.ExtensionRef{
				{
					Group: "konnect.konghq.com",
					Kind:  "KonnectExtension",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: "konnect-extension",
					},
				},
			},
			expectedListenersOptions: []operatorv2beta1.GatewayConfigurationListenerOptions{
				{
					Name:     "listener-1",
					NodePort: int32(3000),
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj := &operatorv1beta1.GatewayConfiguration{}

			src := &operatorv2beta1.GatewayConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
				},
				Spec: tc.spec,
				Status: operatorv2beta1.GatewayConfigurationStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "AllGood",
						},
					},
				},
			}

			require.NoError(t, obj.ConvertFrom(src))

			require.Equal(t, src.ObjectMeta, obj.ObjectMeta)

			require.EqualValues(t,
				tc.expectedControlPlane.Deployment.PodTemplateSpec.Spec.Containers[0].Env,
				obj.Spec.ControlPlaneOptions.Deployment.PodTemplateSpec.Spec.Containers[0].Env)
			require.Equal(t, tc.expectedControlPlane.Extensions, obj.Spec.ControlPlaneOptions.Extensions)
			require.Equal(t, tc.expectedExtensions, obj.Spec.Extensions)
			require.Equal(t, src.Status.Conditions, obj.Status.Conditions)
		})
	}
}

func TestGatewayConfiguration_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		src  operatorv2beta1.GatewayConfiguration
	}{
		{
			name: "Complete configuration with all options",
			src: operatorv2beta1.GatewayConfiguration{
				Spec: operatorv2beta1.GatewayConfigurationSpec{
					ControlPlaneOptions: &operatorv2beta1.GatewayConfigControlPlaneOptions{
						ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
							IngressClass: lo.ToPtr("kong"),
							WatchNamespaces: &operatorv2beta1.WatchNamespaces{
								Type: operatorv2beta1.WatchNamespacesTypeAll,
							},
							FeatureGates: []operatorv2beta1.ControlPlaneFeatureGate{
								{Name: "GatewayAlpha", State: operatorv2beta1.FeatureGateStateEnabled},
								{Name: "ExperimentalFeature", State: operatorv2beta1.FeatureGateStateDisabled},
							},
							Controllers: []operatorv2beta1.ControlPlaneController{
								{Name: "INGRESS_CLASS_NETWORKINGV1", State: operatorv2beta1.ControllerStateEnabled},
								{Name: "KONG_PLUGIN", State: operatorv2beta1.ControllerStateDisabled},
							},
							DataPlaneSync: &operatorv2beta1.ControlPlaneDataPlaneSync{
								ReverseSync: lo.ToPtr(operatorv2beta1.ControlPlaneReverseSyncStateEnabled),
							},
							GatewayDiscovery: &operatorv2beta1.ControlPlaneGatewayDiscovery{
								ReadinessCheckInterval: &metav1.Duration{Duration: 30 * time.Second},
								ReadinessCheckTimeout:  &metav1.Duration{Duration: 5 * time.Second},
							},
							Cache: &operatorv2beta1.ControlPlaneK8sCache{
								InitSyncDuration: &metav1.Duration{Duration: 60 * time.Second},
							},
							Translation: &operatorv2beta1.ControlPlaneTranslationOptions{
								CombinedServicesFromDifferentHTTPRoutes: lo.ToPtr(operatorv2beta1.ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateEnabled),
								FallbackConfiguration: &operatorv2beta1.ControlPlaneFallbackConfiguration{
									UseLastValidConfig: lo.ToPtr(operatorv2beta1.ControlPlaneFallbackConfigurationStateDisabled),
								},
								DrainSupport: lo.ToPtr(operatorv2beta1.ControlPlaneDrainSupportStateEnabled),
							},
							ConfigDump: &operatorv2beta1.ControlPlaneConfigDump{
								State:         operatorv2beta1.ConfigDumpStateEnabled,
								DumpSensitive: operatorv2beta1.ConfigDumpStateDisabled,
							},
							Konnect: &operatorv2beta1.ControlPlaneKonnectOptions{
								ConsumersSync: lo.ToPtr(operatorv2beta1.ControlPlaneKonnectConsumersSyncStateEnabled),
								Licensing: &operatorv2beta1.ControlPlaneKonnectLicensing{
									State:                lo.ToPtr(operatorv2beta1.ControlPlaneKonnectLicensingStateEnabled),
									InitialPollingPeriod: &metav1.Duration{Duration: 10 * time.Second},
									PollingPeriod:        &metav1.Duration{Duration: 60 * time.Second},
									StorageState:         lo.ToPtr(operatorv2beta1.ControlPlaneKonnectLicensingStateDisabled),
								},
								NodeRefreshPeriod:  &metav1.Duration{Duration: 30 * time.Second},
								ConfigUploadPeriod: &metav1.Duration{Duration: 10 * time.Second},
							},
						},
					},
					DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
						Deployment: operatorv2beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv2beta1.DeploymentOptions{
								Replicas: lo.ToPtr(int32(2)),
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  "gateway",
												Image: "kong-gateway:latest",
											},
										},
									},
								},
							},
						},
					},
					Extensions: []commonv1alpha1.ExtensionRef{
						{
							Group: "konnect.konghq.com",
							Kind:  "KonnectExtension",
							NamespacedRef: commonv1alpha1.NamespacedRef{
								Name: "konnect-extension",
							},
						},
						{
							Group: "gateway-operator.konghq.com",
							Kind:  "DataPlaneMetricsExtension",
							NamespacedRef: commonv1alpha1.NamespacedRef{
								Name: "metrics-extension",
							},
						},
					},
				},
				Status: operatorv2beta1.GatewayConfigurationStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "AllGood",
						},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := &operatorv2beta1.GatewayConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
				},
				Spec: tc.src.Spec,
				Status: operatorv2beta1.GatewayConfigurationStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "AllGood",
						},
					},
				},
			}

			intermediate := &operatorv1beta1.GatewayConfiguration{}
			require.NoError(t, intermediate.ConvertFrom(original))

			roundTrip := &operatorv2beta1.GatewayConfiguration{}
			require.NoError(t, intermediate.ConvertTo(roundTrip))

			require.Equal(t, original.ObjectMeta, roundTrip.ObjectMeta)
			require.Equal(t, original.Spec.ControlPlaneOptions.IngressClass, roundTrip.Spec.ControlPlaneOptions.IngressClass)

			if original.Spec.ControlPlaneOptions.WatchNamespaces == nil {
				if roundTrip.Spec.ControlPlaneOptions.WatchNamespaces != nil {
					require.Empty(t, roundTrip.Spec.ControlPlaneOptions.WatchNamespaces.Type)
					require.Nil(t, roundTrip.Spec.ControlPlaneOptions.WatchNamespaces.List)
				}
			} else {
				require.Equal(t, original.Spec.ControlPlaneOptions.WatchNamespaces, roundTrip.Spec.ControlPlaneOptions.WatchNamespaces)
			}

			require.Equal(t, original.Spec.Extensions, roundTrip.Spec.Extensions)
			require.ElementsMatch(t, original.Spec.ControlPlaneOptions.FeatureGates, roundTrip.Spec.ControlPlaneOptions.FeatureGates)
			require.ElementsMatch(t, original.Spec.ControlPlaneOptions.Controllers, roundTrip.Spec.ControlPlaneOptions.Controllers)
			require.Equal(t, original.Status.Conditions, roundTrip.Status.Conditions)

			if original.Spec.ControlPlaneOptions.DataPlaneSync != nil {
				require.NotNil(t, roundTrip.Spec.ControlPlaneOptions.DataPlaneSync)
				require.Equal(t, original.Spec.ControlPlaneOptions.DataPlaneSync.ReverseSync, roundTrip.Spec.ControlPlaneOptions.DataPlaneSync.ReverseSync)
			}
			if original.Spec.ControlPlaneOptions.GatewayDiscovery != nil {
				require.NotNil(t, roundTrip.Spec.ControlPlaneOptions.GatewayDiscovery)
				require.Equal(t, original.Spec.ControlPlaneOptions.GatewayDiscovery.ReadinessCheckInterval, roundTrip.Spec.ControlPlaneOptions.GatewayDiscovery.ReadinessCheckInterval)
				require.Equal(t, original.Spec.ControlPlaneOptions.GatewayDiscovery.ReadinessCheckTimeout, roundTrip.Spec.ControlPlaneOptions.GatewayDiscovery.ReadinessCheckTimeout)
			}
			if original.Spec.ControlPlaneOptions.Cache != nil {
				require.NotNil(t, roundTrip.Spec.ControlPlaneOptions.Cache)
				require.Equal(t, original.Spec.ControlPlaneOptions.Cache.InitSyncDuration, roundTrip.Spec.ControlPlaneOptions.Cache.InitSyncDuration)
			}
			if original.Spec.ControlPlaneOptions.Translation != nil {
				require.NotNil(t, roundTrip.Spec.ControlPlaneOptions.Translation)
				require.Equal(t, original.Spec.ControlPlaneOptions.Translation, roundTrip.Spec.ControlPlaneOptions.Translation)
			}
			if original.Spec.ControlPlaneOptions.ConfigDump != nil {
				require.NotNil(t, roundTrip.Spec.ControlPlaneOptions.ConfigDump)
				require.Equal(t, original.Spec.ControlPlaneOptions.ConfigDump, roundTrip.Spec.ControlPlaneOptions.ConfigDump)
			}
			if original.Spec.ControlPlaneOptions.Konnect != nil {
				require.NotNil(t, roundTrip.Spec.ControlPlaneOptions.Konnect)
				require.Equal(t, original.Spec.ControlPlaneOptions.Konnect, roundTrip.Spec.ControlPlaneOptions.Konnect)
			}
		})
	}
}
