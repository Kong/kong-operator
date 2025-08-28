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

func TestControlPlane_ConvertTo(t *testing.T) {
	t.Run("error: wrong hub type", func(t *testing.T) {
		obj := &operatorv1beta1.ControlPlane{}
		testConversionError(t, func() error {
			return obj.ConvertTo(&dummyHub{})
		}, "ControlPlane ConvertTo: expected *operatorv2beta1.ControlPlane, got %T")
	})

	cases := []struct {
		name                 string
		spec                 operatorv1beta1.ControlPlaneSpec
		expectsDataPlane     bool
		expectedIngressClass *string
		expectedFeatureGates []operatorv2beta1.ControlPlaneFeatureGate
		expectedControllers  []operatorv2beta1.ControlPlaneController
		expectedError        error
	}{
		{
			name: "With DataPlane ref",
			spec: operatorv1beta1.ControlPlaneSpec{
				ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
					DataPlane: lo.ToPtr("test-dataplane"),
					WatchNamespaces: &operatorv1beta1.WatchNamespaces{
						Type: operatorv1beta1.WatchNamespacesTypeAll,
					},
					Extensions: []commonv1alpha1.ExtensionRef{
						{
							Group: "konnect.konghq.com",
							Kind:  "KonnectExtension",
							NamespacedRef: commonv1alpha1.NamespacedRef{
								Name: "test-extension",
							},
						},
					},
					Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
						Replicas: lo.ToPtr(int32(2)),
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
			expectedIngressClass: lo.ToPtr("kong"),
			expectsDataPlane:     true,
			expectedFeatureGates: []operatorv2beta1.ControlPlaneFeatureGate{
				{Name: "GatewayAlpha", State: operatorv2beta1.FeatureGateStateEnabled},
				{Name: "ExperimentalFeature", State: operatorv2beta1.FeatureGateStateDisabled},
			},
			expectedControllers: []operatorv2beta1.ControlPlaneController{
				{Name: "INGRESS_CLASS_NETWORKINGV1", State: operatorv2beta1.ControllerStateEnabled},
				{Name: "KONG_PLUGIN", State: operatorv2beta1.ControllerStateDisabled},
			},
		},
		{
			name: "With EnvFrom and ValueFrom returns error",
			spec: operatorv1beta1.ControlPlaneSpec{
				ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
					DataPlane: lo.ToPtr("test-dataplane"),
					Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "controller",
										Image: "kong/controller:latest",
										Env: []corev1.EnvVar{
											{
												Name: "POD_NAME",
												ValueFrom: &corev1.EnvVarSource{
													FieldRef: &corev1.ObjectFieldSelector{
														FieldPath: "metadata.name",
													},
												},
											},
											{
												Name: "POD_NAMESPACE",
												ValueFrom: &corev1.EnvVarSource{
													FieldRef: &corev1.ObjectFieldSelector{
														FieldPath: "metadata.namespace",
													},
												},
											},
											{
												Name: "CONTROLLER_FEATURE_GATES",
												ValueFrom: &corev1.EnvVarSource{
													SecretKeyRef: &corev1.SecretKeySelector{
														LocalObjectReference: corev1.LocalObjectReference{
															Name: "secret-with-fg",
														},
														Key: "fg-key",
													},
												},
											},
											{
												Name: "CONTROLLER_ENABLE_CONTROLLER_KONG_PLUGIN",
												ValueFrom: &corev1.EnvVarSource{
													SecretKeyRef: &corev1.SecretKeySelector{
														LocalObjectReference: corev1.LocalObjectReference{
															Name: "configmap-controllers",
														},
														Key: "kp-key",
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				IngressClass: lo.ToPtr("kong"),
			},
			expectedError: errors.New("ControlPlane v1beta1 can't be converted, because environment variable: CONTROLLER_FEATURE_GATES is populated with EnvFrom, manual adjustment is needed"),
		},
		{
			name: "With EnvFrom on container level",
			spec: operatorv1beta1.ControlPlaneSpec{
				ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
					DataPlane: lo.ToPtr("test-dataplane"),
					Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "controller",
										Image: "kong/controller:latest",
										EnvFrom: []corev1.EnvFromSource{
											{
												ConfigMapRef: &corev1.ConfigMapEnvSource{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "configmap-controllers",
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				IngressClass: lo.ToPtr("kong"),
			},
			expectedError: errors.New("ControlPlane v1beta1 can't be converted, because EnvFrom is used on container level (converter can't reason about values), manual adjustment is needed"),
		},
		{
			name: "Without DataPlane ref (managed by owner)",
			spec: operatorv1beta1.ControlPlaneSpec{
				ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
					WatchNamespaces: &operatorv1beta1.WatchNamespaces{
						Type: operatorv1beta1.WatchNamespacesTypeList,
						List: []string{"namespace1", "namespace2"},
					},
				},
				IngressClass: lo.ToPtr("test"),
			},
			expectedIngressClass: lo.ToPtr("test"),
			expectsDataPlane:     false,
		},
		{
			name: "With own namespace watching",
			spec: operatorv1beta1.ControlPlaneSpec{
				ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
					WatchNamespaces: &operatorv1beta1.WatchNamespaces{
						Type: operatorv1beta1.WatchNamespacesTypeOwn,
					},
				},
			},
			expectedIngressClass: lo.ToPtr("kong"),
			expectsDataPlane:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj := &operatorv1beta1.ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
				},
				Spec: tc.spec,
				Status: operatorv1beta1.ControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "AllGood",
						},
					},
				},
			}

			dst := &operatorv2beta1.ControlPlane{}
			err := obj.ConvertTo(dst)
			if tc.expectedError != nil {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedError.Error())
				return
			}
			require.NoError(t, err)

			require.Equal(t, obj.ObjectMeta, dst.ObjectMeta)

			if tc.expectsDataPlane {
				require.Equal(t, operatorv2beta1.ControlPlaneDataPlaneTargetRefType, dst.Spec.DataPlane.Type)
				require.NotNil(t, dst.Spec.DataPlane.Ref)
				require.Equal(t, lo.FromPtr(tc.spec.DataPlane), dst.Spec.DataPlane.Ref.Name)
			} else {
				require.Equal(t, operatorv2beta1.ControlPlaneDataPlaneTargetManagedByType, dst.Spec.DataPlane.Type)
				require.Nil(t, dst.Spec.DataPlane.Ref)
			}

			require.Equal(t, tc.expectedIngressClass, dst.Spec.IngressClass)

			if tc.spec.WatchNamespaces != nil {
				require.NotNil(t, dst.Spec.WatchNamespaces)
				require.Equal(t, operatorv2beta1.WatchNamespacesType(tc.spec.WatchNamespaces.Type), dst.Spec.WatchNamespaces.Type)
				require.Equal(t, tc.spec.WatchNamespaces.List, dst.Spec.WatchNamespaces.List)
			} else {
				require.Empty(t, dst.Spec.WatchNamespaces)
			}

			require.Equal(t, tc.spec.Extensions, dst.Spec.Extensions)

			if tc.expectedFeatureGates != nil {
				require.ElementsMatch(t, tc.expectedFeatureGates, dst.Spec.FeatureGates)
			}

			if tc.expectedControllers != nil {
				require.ElementsMatch(t, tc.expectedControllers, dst.Spec.Controllers)
			}

			require.Equal(t, obj.Status.Conditions, dst.Status.Conditions)
		})
	}
}

func TestControlPlane_ConvertFrom(t *testing.T) {
	t.Run("error: wrong hub type", func(t *testing.T) {
		obj := &operatorv1beta1.ControlPlane{}
		testConversionError(t, func() error {
			return obj.ConvertFrom(&dummyHub{})
		}, "ControlPlane ConvertFrom: expected *operatorv2beta1.ControlPlane, got %T")
	})

	cases := []struct {
		name             string
		src              operatorv2beta1.ControlPlaneSpec
		expectsDataPlane bool
	}{
		{
			name: "With DataPlane ref",
			src: operatorv2beta1.ControlPlaneSpec{
				DataPlane: operatorv2beta1.ControlPlaneDataPlaneTarget{
					Type: operatorv2beta1.ControlPlaneDataPlaneTargetRefType,
					Ref: &operatorv2beta1.ControlPlaneDataPlaneTargetRef{
						Name: "test-dataplane",
					},
				},
				ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
					IngressClass: lo.ToPtr("kong"),
					WatchNamespaces: &operatorv2beta1.WatchNamespaces{
						Type: operatorv2beta1.WatchNamespacesTypeAll,
					},
					FeatureGates: []operatorv2beta1.ControlPlaneFeatureGate{
						{Name: "GatewayAlpha", State: operatorv2beta1.FeatureGateStateEnabled},
						{Name: "ExperimentalFeature", State: operatorv2beta1.FeatureGateStateDisabled},
					},
				},
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						Group: "konnect.konghq.com",
						Kind:  "KonnectExtension",
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: "test-extension",
						},
					},
				},
			},
			expectsDataPlane: true,
		},
		{
			name: "With managedByOwner DataPlane",
			src: operatorv2beta1.ControlPlaneSpec{
				DataPlane: operatorv2beta1.ControlPlaneDataPlaneTarget{
					Type: operatorv2beta1.ControlPlaneDataPlaneTargetManagedByType,
				},
				ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
					IngressClass: lo.ToPtr("kong"),
					WatchNamespaces: &operatorv2beta1.WatchNamespaces{
						Type: operatorv2beta1.WatchNamespacesTypeList,
						List: []string{"namespace1", "namespace2"},
					},
				},
			},
			expectsDataPlane: false,
		},
		{
			name: "With own namespace watching",
			src: operatorv2beta1.ControlPlaneSpec{
				DataPlane: operatorv2beta1.ControlPlaneDataPlaneTarget{
					Type: operatorv2beta1.ControlPlaneDataPlaneTargetManagedByType,
				},
				ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
					WatchNamespaces: &operatorv2beta1.WatchNamespaces{
						Type: operatorv2beta1.WatchNamespacesTypeOwn,
					},
				},
			},
			expectsDataPlane: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj := &operatorv1beta1.ControlPlane{}

			src := &operatorv2beta1.ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
				},
				Spec: tc.src,
				Status: operatorv2beta1.ControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "AllGood",
						},
					},
					FeatureGates: []operatorv2beta1.ControlPlaneFeatureGate{
						{Name: "StatusFeatureGate", State: operatorv2beta1.FeatureGateStateEnabled},
					},
					Controllers: []operatorv2beta1.ControlPlaneController{
						{Name: "StatusController", State: operatorv2beta1.ControllerStateEnabled},
					},
				},
			}

			require.NoError(t, obj.ConvertFrom(src))

			require.Equal(t, src.ObjectMeta, obj.ObjectMeta)

			if tc.expectsDataPlane {
				require.NotNil(t, obj.Spec.DataPlane)
				require.Equal(t, tc.src.DataPlane.Ref.Name, *obj.Spec.DataPlane)
			} else {
				require.Nil(t, obj.Spec.DataPlane)
			}

			require.Equal(t, tc.src.IngressClass, obj.Spec.IngressClass)

			if tc.src.WatchNamespaces != nil {
				require.NotNil(t, obj.Spec.WatchNamespaces)
				require.Equal(t, operatorv1beta1.WatchNamespacesType(tc.src.WatchNamespaces.Type), obj.Spec.WatchNamespaces.Type)
				require.Equal(t, tc.src.WatchNamespaces.List, obj.Spec.WatchNamespaces.List)
			} else {
				require.Nil(t, obj.Spec.WatchNamespaces)
			}

			require.Equal(t, tc.src.Extensions, obj.Spec.Extensions)

			require.Equal(t, src.Status.Conditions, obj.Status.Conditions)
		})
	}
}

func TestControlPlane_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		src  operatorv2beta1.ControlPlaneSpec
	}{
		{
			name: "Complete configuration with all options",
			src: operatorv2beta1.ControlPlaneSpec{
				DataPlane: operatorv2beta1.ControlPlaneDataPlaneTarget{
					Type: operatorv2beta1.ControlPlaneDataPlaneTargetRefType,
					Ref: &operatorv2beta1.ControlPlaneDataPlaneTargetRef{
						Name: "test-dataplane",
					},
				},
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
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						Group: "konnect.konghq.com",
						Kind:  "KonnectExtension",
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: "test-extension",
						},
					},
				},
			},
		},
		{
			name: "Minimal configuration with managed dataplane",
			src: operatorv2beta1.ControlPlaneSpec{
				DataPlane: operatorv2beta1.ControlPlaneDataPlaneTarget{
					Type: operatorv2beta1.ControlPlaneDataPlaneTargetManagedByType,
				},
				ControlPlaneOptions: operatorv2beta1.ControlPlaneOptions{
					IngressClass: lo.ToPtr("kong"),
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := &operatorv2beta1.ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
				},
				Spec: tc.src,
				Status: operatorv2beta1.ControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "AllGood",
						},
					},
				},
			}

			intermediate := &operatorv1beta1.ControlPlane{}
			err := intermediate.ConvertFrom(original)
			require.NoError(t, err)

			roundTrip := &operatorv2beta1.ControlPlane{}
			err = intermediate.ConvertTo(roundTrip)
			require.NoError(t, err)

			require.Equal(t, original.ObjectMeta, roundTrip.ObjectMeta)
			require.Equal(t, original.Spec.DataPlane, roundTrip.Spec.DataPlane)
			require.Equal(t, original.Spec.IngressClass, roundTrip.Spec.IngressClass)

			if original.Spec.WatchNamespaces == nil {
				if roundTrip.Spec.WatchNamespaces != nil {
					require.Empty(t, roundTrip.Spec.WatchNamespaces.Type)
					require.Nil(t, roundTrip.Spec.WatchNamespaces.List)
				}
			} else {
				require.Equal(t, original.Spec.WatchNamespaces, roundTrip.Spec.WatchNamespaces)
			}

			require.Equal(t, original.Spec.Extensions, roundTrip.Spec.Extensions)
			require.ElementsMatch(t, original.Spec.FeatureGates, roundTrip.Spec.FeatureGates)
			require.ElementsMatch(t, original.Spec.Controllers, roundTrip.Spec.Controllers)
			require.Equal(t, original.Status.Conditions, roundTrip.Status.Conditions)

			if original.Spec.DataPlaneSync != nil {
				require.NotNil(t, roundTrip.Spec.DataPlaneSync)
				require.Equal(t, original.Spec.DataPlaneSync.ReverseSync, roundTrip.Spec.DataPlaneSync.ReverseSync)
			}
			if original.Spec.GatewayDiscovery != nil {
				require.NotNil(t, roundTrip.Spec.GatewayDiscovery)
				require.Equal(t, original.Spec.GatewayDiscovery.ReadinessCheckInterval, roundTrip.Spec.GatewayDiscovery.ReadinessCheckInterval)
				require.Equal(t, original.Spec.GatewayDiscovery.ReadinessCheckTimeout, roundTrip.Spec.GatewayDiscovery.ReadinessCheckTimeout)
			}
			if original.Spec.Cache != nil {
				require.NotNil(t, roundTrip.Spec.Cache)
				require.Equal(t, original.Spec.Cache.InitSyncDuration, roundTrip.Spec.Cache.InitSyncDuration)
			}
			if original.Spec.Translation != nil {
				require.NotNil(t, roundTrip.Spec.Translation)
				require.Equal(t, original.Spec.Translation, roundTrip.Spec.Translation)
			}
			if original.Spec.ConfigDump != nil {
				require.NotNil(t, roundTrip.Spec.ConfigDump)
				require.Equal(t, original.Spec.ConfigDump, roundTrip.Spec.ConfigDump)
			}
			if original.Spec.Konnect != nil {
				require.NotNil(t, roundTrip.Spec.Konnect)
				require.Equal(t, original.Spec.Konnect, roundTrip.Spec.Konnect)
			}
		})
	}
}
