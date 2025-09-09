package dataplane

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kcfgdataplane "github.com/kong/kong-operator/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

func TestEnsureDataPlaneReadyStatus(t *testing.T) {
	testCases := []struct {
		name                    string
		objectLists             []client.ObjectList
		expectedError           bool
		expectedResult          reconcile.Result
		expectedDataPlaneStatus operatorv1beta1.DataPlaneStatus
		dataPlane               *operatorv1beta1.DataPlane
	}{
		{
			name: "not all replicas are ready",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					UID:        "test-uid",
					Name:       "test",
					Namespace:  "default",
					Generation: 102,
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  consts.DataPlaneProxyContainerName,
												Image: consts.DefaultDataPlaneImage,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			objectLists: []client.ObjectList{
				&appsv1.DeploymentList{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DeploymentList",
						APIVersion: "apps/v1",
					},
					Items: []appsv1.Deployment{
						{
							TypeMeta: metav1.TypeMeta{
								Kind:       "Deployment",
								APIVersion: "apps/v1",
							},
							ObjectMeta: metav1.ObjectMeta{
								Name:      "dataplane-deployment-1",
								Namespace: "default",
								Labels: map[string]string{
									"app":                                "test",
									consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
								},
								OwnerReferences: []metav1.OwnerReference{
									{
										APIVersion: "gateway-operator.konghq.com/v1beta1",
										Kind:       "DataPlane",
										UID:        "test-uid",
									},
								},
							},
							Spec: appsv1.DeploymentSpec{},
							Status: appsv1.DeploymentStatus{
								Replicas:          2,
								ReadyReplicas:     1,
								AvailableReplicas: 1,
							},
						},
					},
				},
			},
			expectedError:  false,
			expectedResult: ctrl.Result{},
			expectedDataPlaneStatus: operatorv1beta1.DataPlaneStatus{
				Conditions: []metav1.Condition{
					k8sutils.NewConditionWithGeneration(
						kcfgdataplane.ReadyType,
						metav1.ConditionFalse,
						kcfgdataplane.WaitingToBecomeReadyReason,
						fmt.Sprintf("%s: Deployment %s is not ready yet", kcfgdataplane.WaitingToBecomeReadyMessage, "dataplane-deployment-1"),
						102,
					),
				},
				Replicas:      2,
				ReadyReplicas: 1,
			},
		},
		{
			name: "all replicas are ready but ingress service of type LoadBalancer doesn't have an IP",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					UID:        "test-uid",
					Name:       "test",
					Namespace:  "default",
					Generation: 102,
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							Services: &operatorv1beta1.DataPlaneServices{
								Ingress: &operatorv1beta1.DataPlaneServiceOptions{
									ServiceOptions: operatorv1beta1.ServiceOptions{
										Type: corev1.ServiceTypeLoadBalancer,
									},
								},
							},
						},
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  consts.DataPlaneProxyContainerName,
												Image: consts.DefaultDataPlaneImage,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			objectLists: []client.ObjectList{
				&appsv1.DeploymentList{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DeploymentList",
						APIVersion: "apps/v1",
					},
					Items: []appsv1.Deployment{
						{
							TypeMeta: metav1.TypeMeta{
								Kind:       "Deployment",
								APIVersion: "apps/v1",
							},
							ObjectMeta: metav1.ObjectMeta{
								Name:      "dataplane-deployment-1",
								Namespace: "default",
								Labels: map[string]string{
									"app":                                "test",
									consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
								},
								OwnerReferences: []metav1.OwnerReference{
									{
										APIVersion: "gateway-operator.konghq.com/v1beta1",
										Kind:       "DataPlane",
										UID:        "test-uid",
									},
								},
							},
							Spec: appsv1.DeploymentSpec{},
							Status: appsv1.DeploymentStatus{
								Replicas:          1,
								ReadyReplicas:     1,
								AvailableReplicas: 1,
							},
						},
					},
				},
				&corev1.ServiceList{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ServiceList",
						APIVersion: "apps/v1",
					},
					Items: []corev1.Service{
						{
							TypeMeta: metav1.TypeMeta{
								Kind:       "Service",
								APIVersion: "v1",
							},
							ObjectMeta: metav1.ObjectMeta{
								Name:      "dataplane-service-1",
								Namespace: "default",
								Labels: map[string]string{
									"app":                             "test",
									consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValueLive,
									consts.DataPlaneServiceTypeLabel:  string(consts.DataPlaneIngressServiceLabelValue),
								},
								OwnerReferences: []metav1.OwnerReference{
									{
										APIVersion: "gateway-operator.konghq.com/v1beta1",
										Kind:       "DataPlane",
										UID:        "test-uid",
									},
								},
							},
							Spec: corev1.ServiceSpec{
								Type: corev1.ServiceTypeLoadBalancer,
							},
							Status: corev1.ServiceStatus{
								// Empty to cause Ready condition False
							},
						},
					},
				},
			},
			expectedError:  false,
			expectedResult: ctrl.Result{},
			expectedDataPlaneStatus: operatorv1beta1.DataPlaneStatus{
				Conditions: []metav1.Condition{
					k8sutils.NewConditionWithGeneration(
						kcfgdataplane.ReadyType,
						metav1.ConditionFalse,
						kcfgdataplane.WaitingToBecomeReadyReason,
						fmt.Sprintf("%s: ingress Service %s is not ready yet", kcfgdataplane.WaitingToBecomeReadyMessage, "dataplane-service-1"),
						102,
					),
				},
				Replicas:      1,
				ReadyReplicas: 1,
			},
		},
		{
			name: "all replicas are ready and ingress service of type load balancer has an IP",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					UID:        "test-uid",
					Name:       "test",
					Namespace:  "default",
					Generation: 102,
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							Services: &operatorv1beta1.DataPlaneServices{
								Ingress: &operatorv1beta1.DataPlaneServiceOptions{
									ServiceOptions: operatorv1beta1.ServiceOptions{
										Type: corev1.ServiceTypeLoadBalancer,
									},
								},
							},
						},
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  consts.DataPlaneProxyContainerName,
												Image: consts.DefaultDataPlaneImage,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			objectLists: []client.ObjectList{
				&appsv1.DeploymentList{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DeploymentList",
						APIVersion: "apps/v1",
					},
					Items: []appsv1.Deployment{
						{
							TypeMeta: metav1.TypeMeta{
								Kind:       "Deployment",
								APIVersion: "apps/v1",
							},
							ObjectMeta: metav1.ObjectMeta{
								Name:      "dataplane-deployment-1",
								Namespace: "default",
								Labels: map[string]string{
									"app":                                "test",
									consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
								},
								OwnerReferences: []metav1.OwnerReference{
									{
										APIVersion: "gateway-operator.konghq.com/v1beta1",
										Kind:       "DataPlane",
										UID:        "test-uid",
									},
								},
							},
							Spec: appsv1.DeploymentSpec{},
							Status: appsv1.DeploymentStatus{
								Replicas:          1,
								ReadyReplicas:     1,
								AvailableReplicas: 1,
							},
						},
					},
				},
				&corev1.ServiceList{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DeploymentList",
						APIVersion: "apps/v1",
					},
					Items: []corev1.Service{
						{
							TypeMeta: metav1.TypeMeta{
								Kind:       "Service",
								APIVersion: "v1",
							},
							ObjectMeta: metav1.ObjectMeta{
								Name:      "dataplane-service-1",
								Namespace: "default",
								Labels: map[string]string{
									"app":                             "test",
									consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValueLive,
									consts.DataPlaneServiceTypeLabel:  string(consts.DataPlaneIngressServiceLabelValue),
								},
								OwnerReferences: []metav1.OwnerReference{
									{
										APIVersion: "gateway-operator.konghq.com/v1beta1",
										Kind:       "DataPlane",
										UID:        "test-uid",
									},
								},
							},
							Spec: corev1.ServiceSpec{},
							Status: corev1.ServiceStatus{
								LoadBalancer: corev1.LoadBalancerStatus{
									Ingress: []corev1.LoadBalancerIngress{
										{
											IP: "3.3.3.3",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedError:  false,
			expectedResult: ctrl.Result{},
			expectedDataPlaneStatus: operatorv1beta1.DataPlaneStatus{
				Conditions: []metav1.Condition{
					k8sutils.NewConditionWithGeneration(
						kcfgdataplane.ReadyType,
						metav1.ConditionTrue,
						"Ready",
						"",
						102,
					),
				},
				Replicas:      1,
				ReadyReplicas: 1,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()

			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, appsv1.AddToScheme(scheme))
			require.NoError(t, operatorv1beta1.AddToScheme(scheme))
			require.NoError(t, gatewayv1.Install(scheme))

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithStatusSubresource(tc.dataPlane).
				WithScheme(scheme).
				WithObjects(tc.dataPlane).
				WithLists(tc.objectLists...).
				Build()

			res, err := ensureDataPlaneReadyStatus(t.Context(), fakeClient, logr.Discard(), tc.dataPlane, tc.dataPlane.Generation)
			if tc.expectedError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expectedResult, res)
			opts := []cmp.Option{
				cmp.FilterPath(
					func(p cmp.Path) bool { return p.String() == "Conditions.LastTransitionTime" },
					cmp.Ignore(),
				),
			}
			if !cmp.Equal(tc.expectedDataPlaneStatus, tc.dataPlane.Status, opts...) {
				d := cmp.Diff(tc.expectedDataPlaneStatus, tc.dataPlane.Status, opts...)
				assert.FailNowf(t, "unexpected DataPlane status", "got :\n%#v\ndiff:\n%s\n", tc.dataPlane.Status, d)
			}
		})
	}
}
