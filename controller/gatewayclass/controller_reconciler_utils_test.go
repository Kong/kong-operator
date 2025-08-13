package gatewayclass

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/gateway-operator/pkg/consts"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

func TestGetAcceptedCondition(t *testing.T) {
	scheme := runtime.NewScheme()
	assert.NoError(t, gatewayv1.Install(scheme))
	assert.NoError(t, operatorv1beta1.AddToScheme(scheme))

	tests := []struct {
		name           string
		gwc            *gatewayv1.GatewayClass
		existingObjs   []runtime.Object
		expectedStatus metav1.ConditionStatus
		expectedReason string
		expectedMsg    string
	}{
		{
			name: "ParametersRef is nil",
			gwc: &gatewayv1.GatewayClass{
				Spec: gatewayv1.GatewayClassSpec{
					ParametersRef: nil,
				},
			},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: string(gatewayv1.GatewayClassReasonAccepted),
			expectedMsg:    "GatewayClass is accepted",
		},
		{
			name: "Invalid ParametersRef Group and kind",
			gwc: &gatewayv1.GatewayClass{
				Spec: gatewayv1.GatewayClassSpec{
					ParametersRef: &gatewayv1.ParametersReference{
						Group:     "invalid.group",
						Kind:      "InvalidKind",
						Namespace: lo.ToPtr(gatewayv1.Namespace("default")),
						Name:      "invalid",
					},
				},
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: string(gatewayv1.GatewayClassReasonInvalidParameters),
			expectedMsg:    "ParametersRef must reference a gateway-operator.konghq.com/GatewayConfiguration",
		},
		{
			name: "ParametersRef Namespace is nil",
			gwc: &gatewayv1.GatewayClass{
				Spec: gatewayv1.GatewayClassSpec{
					ParametersRef: &gatewayv1.ParametersReference{
						Group: gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
						Kind:  "GatewayConfiguration",
						Name:  "no-namespace",
					},
				},
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: string(gatewayv1.GatewayClassReasonInvalidParameters),
			expectedMsg:    "ParametersRef must reference a namespaced resource",
		},
		{
			name: "GatewayConfiguration does not exist",
			gwc: &gatewayv1.GatewayClass{
				Spec: gatewayv1.GatewayClassSpec{
					ParametersRef: &gatewayv1.ParametersReference{
						Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
						Kind:      "GatewayConfiguration",
						Name:      "nonexistent",
						Namespace: lo.ToPtr(gatewayv1.Namespace("default")),
					},
				},
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: string(gatewayv1.GatewayClassReasonInvalidParameters),
			expectedMsg:    "The referenced GatewayConfiguration does not exist",
		},
		{
			name: "Valid ParametersRef",
			gwc: &gatewayv1.GatewayClass{
				Spec: gatewayv1.GatewayClassSpec{
					ParametersRef: &gatewayv1.ParametersReference{
						Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
						Kind:      "GatewayConfiguration",
						Name:      "valid-config",
						Namespace: lo.ToPtr(gatewayv1.Namespace("default")),
					},
				},
			},
			existingObjs: []runtime.Object{
				&operatorv1beta1.GatewayConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "valid-config",
						Namespace: "default",
					},
				},
			},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: string(gatewayv1.GatewayClassReasonAccepted),
			expectedMsg:    "GatewayClass is accepted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tt.existingObjs...).
				Build()

			condition, err := getAcceptedCondition(ctx, cl, tt.gwc)
			require.NoError(t, err)
			assert.NotNil(t, condition)
			assert.Equal(t, tt.expectedStatus, condition.Status)
			assert.Equal(t, tt.expectedReason, condition.Reason)
			assert.Equal(t, tt.expectedMsg, condition.Message)
		})
	}
}

func TestGetRouterFlavor(t *testing.T) {
	scheme := runtime.NewScheme()
	assert.NoError(t, gatewayv1.Install(scheme))
	assert.NoError(t, operatorv1beta1.AddToScheme(scheme))

	tests := []struct {
		name           string
		gatewayConfig  *operatorv1beta1.GatewayConfiguration
		existingObjs   []runtime.Object
		expectedFlavor consts.RouterFlavor
	}{
		{
			name:           "GatewayConfiguration is nil",
			gatewayConfig:  nil,
			expectedFlavor: consts.RouterFlavorTraditionalCompatible,
		},
		{
			name: "DataPlaneOptions is nil",
			gatewayConfig: &operatorv1beta1.GatewayConfiguration{
				Spec: operatorv1beta1.GatewayConfigurationSpec{
					DataPlaneOptions: nil,
				},
			},
			expectedFlavor: consts.RouterFlavorTraditionalCompatible,
		},
		{
			name: "PodTemplateSpec is nil",
			gatewayConfig: &operatorv1beta1.GatewayConfiguration{
				Spec: operatorv1beta1.GatewayConfigurationSpec{
					DataPlaneOptions: &operatorv1beta1.GatewayConfigDataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: nil,
							},
						},
					},
				},
			},
			expectedFlavor: consts.RouterFlavorTraditionalCompatible,
		},
		{
			name: "Container not found",
			gatewayConfig: &operatorv1beta1.GatewayConfiguration{
				Spec: operatorv1beta1.GatewayConfigurationSpec{
					DataPlaneOptions: &operatorv1beta1.GatewayConfigDataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{},
									},
								},
							},
						},
					},
				},
			},
			expectedFlavor: consts.RouterFlavorTraditionalCompatible,
		},
		{
			name: "KONG_ROUTER_FLAVOR not found",
			gatewayConfig: &operatorv1beta1.GatewayConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: operatorv1beta1.GatewayConfigurationSpec{
					DataPlaneOptions: &operatorv1beta1.GatewayConfigDataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name: consts.DataPlaneProxyContainerName,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedFlavor: consts.RouterFlavorTraditionalCompatible,
		},
		{
			name: "KONG_ROUTER_FLAVOR found",
			gatewayConfig: &operatorv1beta1.GatewayConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: operatorv1beta1.GatewayConfigurationSpec{
					DataPlaneOptions: &operatorv1beta1.GatewayConfigDataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name: consts.DataPlaneProxyContainerName,
												Env: []corev1.EnvVar{
													{
														Name:  consts.RouterFlavorEnvKey,
														Value: string(consts.RouterFlavorTraditionalCompatible),
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
			},
			expectedFlavor: consts.RouterFlavorTraditionalCompatible,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tt.existingObjs...).
				Build()

			flavor, err := getRouterFlavor(ctx, cl, tt.gatewayConfig)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedFlavor, flavor)
		})
	}
}
