package patch

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/kong-operator/controller/pkg/op"
	"github.com/kong/kong-operator/pkg/consts"
	k8sresources "github.com/kong/kong-operator/pkg/utils/kubernetes/resources"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
)

func TestApplyPatchIfNonEmpty(t *testing.T) {
	testcases := []struct {
		name      string
		dataPlane *operatorv1beta1.DataPlane
		// No easy way to make the function type in struct generic to test update for
		// other types of objects e.g. Deployments generically.
		// There might be a way to achieve this but with serious refactoring of
		// these tests.
		generateHPAFunc func(t *testing.T, dataplane *operatorv1beta1.DataPlane) *autoscalingv2.HorizontalPodAutoscaler
		changeHPAFunc   func(t *testing.T, hpa *autoscalingv2.HorizontalPodAutoscaler)
		assertHPAFunc   func(t *testing.T, hpa *autoscalingv2.HorizontalPodAutoscaler)
		updated         bool
		wantErr         bool
		wantResult      op.Result
	}{
		{
			name: "when no changes are needed no patch is being made",
			dataPlane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: operatorv1beta1.SchemeGroupVersion.Group + "/" + operatorv1beta1.SchemeGroupVersion.Version,
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								Scaling: &operatorv1beta1.Scaling{
									HorizontalScaling: &operatorv1beta1.HorizontalScaling{
										MaxReplicas: 3,
										Metrics: []autoscalingv2.MetricSpec{
											{
												Type: autoscalingv2.ResourceMetricSourceType,
												Resource: &autoscalingv2.ResourceMetricSource{
													Name: "cpu",
													Target: autoscalingv2.MetricTarget{
														Type:               autoscalingv2.UtilizationMetricType,
														AverageUtilization: lo.ToPtr(int32(20)),
													},
												},
											},
										},
									},
								},
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  consts.DataPlaneProxyContainerName,
												Image: "kong:3.4",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			generateHPAFunc: func(t *testing.T, dataplane *operatorv1beta1.DataPlane) *autoscalingv2.HorizontalPodAutoscaler {
				hpa, err := k8sresources.GenerateHPAForDataPlane(dataplane, "test-deployment")
				require.NoError(t, err)
				return hpa
			},
			assertHPAFunc: func(t *testing.T, hpa *autoscalingv2.HorizontalPodAutoscaler) {
				require.Equal(t, "test-deployment", hpa.Spec.ScaleTargetRef.Name)
			},
			updated:    false,
			wantErr:    false,
			wantResult: op.Noop,
		},
		{
			name: "when changes are applied to the generated HPA a patch is being made",
			dataPlane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: operatorv1beta1.SchemeGroupVersion.Group + "/" + operatorv1beta1.SchemeGroupVersion.Version,
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								Scaling: &operatorv1beta1.Scaling{
									HorizontalScaling: &operatorv1beta1.HorizontalScaling{
										MaxReplicas: 3,
										Metrics: []autoscalingv2.MetricSpec{
											{
												Type: autoscalingv2.ResourceMetricSourceType,
												Resource: &autoscalingv2.ResourceMetricSource{
													Name: "cpu",
													Target: autoscalingv2.MetricTarget{
														Type:               autoscalingv2.UtilizationMetricType,
														AverageUtilization: lo.ToPtr(int32(20)),
													},
												},
											},
										},
									},
								},
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  consts.DataPlaneProxyContainerName,
												Image: "kong:3.4",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			generateHPAFunc: func(t *testing.T, dataplane *operatorv1beta1.DataPlane) *autoscalingv2.HorizontalPodAutoscaler {
				hpa, err := k8sresources.GenerateHPAForDataPlane(dataplane, "test-deployment")
				require.NoError(t, err)
				return hpa
			},
			changeHPAFunc: func(t *testing.T, hpa *autoscalingv2.HorizontalPodAutoscaler) {
				hpa.Spec.MinReplicas = lo.ToPtr(int32(2))
			},
			assertHPAFunc: func(t *testing.T, hpa *autoscalingv2.HorizontalPodAutoscaler) {
				require.Equal(t, int32(2), *hpa.Spec.MinReplicas)
				require.Equal(t, "test-deployment", hpa.Spec.ScaleTargetRef.Name)
			},
			updated:    true,
			wantErr:    false,
			wantResult: op.Updated,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()

			require.NoError(t, operatorv1beta1.AddToScheme(scheme))
			require.NoError(t, autoscalingv2.AddToScheme(scheme))

			log := logr.Discard()
			hpa := tc.generateHPAFunc(t, tc.dataPlane)
			old := hpa.DeepCopy()
			if tc.changeHPAFunc != nil {
				tc.changeHPAFunc(t, hpa)
			}

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.dataPlane, hpa).
				Build()

			result, _, err := ApplyPatchIfNotEmpty(t.Context(), fakeClient, log, hpa, old, tc.updated)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantResult, result)
			if tc.assertHPAFunc != nil {
				tc.assertHPAFunc(t, hpa)
			}
		})
	}
}
