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

	kcfgdataplane "github.com/kong/kong-operator/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
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
			name: "not all replicas are ready (.spec.replicas is set)",
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
							Spec: appsv1.DeploymentSpec{
								Replicas: new(int32(2)),
							},
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
					k8sutils.NewConditionWithGeneration(
						kcfgdataplane.DeploymentRolledOutType,
						metav1.ConditionFalse,
						kcfgdataplane.DeploymentRolloutProgressingReason,
						"Waiting for the Deployment to roll out",
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
					k8sutils.NewConditionWithGeneration(
						kcfgdataplane.DeploymentRolledOutType,
						metav1.ConditionFalse,
						kcfgdataplane.DeploymentRolloutProgressingReason,
						"Waiting for the Deployment to roll out",
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
					k8sutils.NewConditionWithGeneration(
						kcfgdataplane.DeploymentRolledOutType,
						metav1.ConditionFalse,
						kcfgdataplane.DeploymentRolloutProgressingReason,
						"Waiting for the Deployment to roll out",
						102,
					),
				},
				Replicas:      1,
				ReadyReplicas: 1,
			},
		},
		{
			// The Deployment is available (old replica still serving) but has not
			// finished rolling out generation 102 yet: Ready still reports True at
			// generation 102 (the DataPlane is serving traffic), while the separate
			// DeploymentRolledOut condition reports that generation 102 itself has
			// not rolled out yet.
			name: "deployment available but not fully rolled out reports DeploymentRolledOut=False at the current generation",
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
				Status: operatorv1beta1.DataPlaneStatus{
					Conditions: []metav1.Condition{
						k8sutils.NewConditionWithGeneration(
							kcfgdataplane.ReadyType,
							metav1.ConditionTrue,
							kcfgdataplane.ResourceReadyReason,
							"",
							101,
						),
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
								Name:       "dataplane-deployment-1",
								Namespace:  "default",
								Generation: 102,
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
							Spec: appsv1.DeploymentSpec{
								Replicas: new(int32(1)),
							},
							Status: appsv1.DeploymentStatus{
								ObservedGeneration: 102,
								Replicas:           2, // old + surging new replica.
								UpdatedReplicas:    1, // new replica not available yet.
								AvailableReplicas:  1, // old replica still serving.
								ReadyReplicas:      1,
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
						kcfgdataplane.ResourceReadyReason,
						"",
						102,
					),
					k8sutils.NewConditionWithGeneration(
						kcfgdataplane.DeploymentRolledOutType,
						metav1.ConditionFalse,
						kcfgdataplane.DeploymentRolloutProgressingReason,
						"Waiting for the Deployment to roll out",
						102,
					),
				},
				Replicas:      2,
				ReadyReplicas: 1,
			},
		},
		{
			// Regression guard: a transient dip below spec.Replicas during a rollout
			// previously wrote {Ready: False, observedGeneration: <current>}. The next
			// reconcile, seeing the stored condition was not True, used to skip a
			// generation fallback and report {Ready: True, observedGeneration: <current>}
			// while most pods still ran the old template. Ready.observedGeneration must
			// simply track the generation it was computed for; DeploymentRolledOut is
			// the one that reports the rollout is still in progress.
			name: "Ready recovers to the current generation after a transient dip, DeploymentRolledOut still reports the rollout in progress",
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
				Status: operatorv1beta1.DataPlaneStatus{
					Conditions: []metav1.Condition{
						// Left behind by the transient dip.
						k8sutils.NewConditionWithGeneration(
							kcfgdataplane.ReadyType,
							metav1.ConditionFalse,
							kcfgdataplane.WaitingToBecomeReadyReason,
							"",
							102,
						),
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
								Name:       "dataplane-deployment-1",
								Namespace:  "default",
								Generation: 102,
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
							Spec: appsv1.DeploymentSpec{
								Replicas: new(int32(1)),
							},
							Status: appsv1.DeploymentStatus{
								ObservedGeneration: 102,
								Replicas:           2, // old + surging new replica.
								UpdatedReplicas:    1, // new replica not available yet.
								AvailableReplicas:  1, // old replica still serving.
								ReadyReplicas:      1,
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
						kcfgdataplane.ResourceReadyReason,
						"",
						102,
					),
					k8sutils.NewConditionWithGeneration(
						kcfgdataplane.DeploymentRolledOutType,
						metav1.ConditionFalse,
						kcfgdataplane.DeploymentRolloutProgressingReason,
						"Waiting for the Deployment to roll out",
						102,
					),
				},
				Replicas:      2,
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

func TestExtractDataPlaneIngressServiceLabels(t *testing.T) {
	testCases := []struct {
		name           string
		dataplane      operatorv1beta1.DataPlane
		expectedLabels map[string]string
	}{
		{
			name:           "nil Services",
			dataplane:      operatorv1beta1.DataPlane{},
			expectedLabels: nil,
		},
		{
			name: "nil Ingress",
			dataplane: operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							Services: &operatorv1beta1.DataPlaneServices{},
						},
					},
				},
			},
			expectedLabels: nil,
		},
		{
			name: "nil Labels",
			dataplane: operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							Services: &operatorv1beta1.DataPlaneServices{
								Ingress: &operatorv1beta1.DataPlaneServiceOptions{},
							},
						},
					},
				},
			},
			expectedLabels: nil,
		},
		{
			name: "labels present",
			dataplane: operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							Services: &operatorv1beta1.DataPlaneServices{
								Ingress: &operatorv1beta1.DataPlaneServiceOptions{
									ServiceOptions: operatorv1beta1.ServiceOptions{
										Labels: map[operatorv1beta1.LabelName]operatorv1beta1.LabelValue{
											"my-label": "my-value",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedLabels: map[string]string{"my-label": "my-value"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := extractDataPlaneIngressServiceLabels(&tc.dataplane)
			require.Equal(t, tc.expectedLabels, result)
		})
	}
}

func TestAddAnnotationsForDataPlaneIngressService(t *testing.T) {
	testCases := []struct {
		name                string
		existingAnnotations map[string]string
		dataplane           operatorv1beta1.DataPlane
		expectedAnnotations map[string]string
	}{
		{
			name:                "no-op when DataPlane has no ingress service annotations",
			existingAnnotations: map[string]string{"existing": "val"},
			dataplane:           operatorv1beta1.DataPlane{},
			expectedAnnotations: map[string]string{"existing": "val"},
		},
		{
			name:                "annotations merged onto object",
			existingAnnotations: map[string]string{"existing": "val"},
			dataplane: operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							Services: &operatorv1beta1.DataPlaneServices{
								Ingress: &operatorv1beta1.DataPlaneServiceOptions{
									ServiceOptions: operatorv1beta1.ServiceOptions{
										Annotations: map[string]string{"new-annotation": "new-val"},
									},
								},
							},
						},
					},
				},
			},
			expectedAnnotations: map[string]string{
				"existing":                              "val",
				"new-annotation":                        "new-val",
				consts.AnnotationLastAppliedAnnotations: `{"new-annotation":"new-val"}`,
			},
		},
		{
			name:                "spec annotation wins over existing on conflict",
			existingAnnotations: map[string]string{"conflict": "old"},
			dataplane: operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							Services: &operatorv1beta1.DataPlaneServices{
								Ingress: &operatorv1beta1.DataPlaneServiceOptions{
									ServiceOptions: operatorv1beta1.ServiceOptions{
										Annotations: map[string]string{"conflict": "new"},
									},
								},
							},
						},
					},
				},
			},
			expectedAnnotations: map[string]string{
				"conflict":                              "new",
				consts.AnnotationLastAppliedAnnotations: `{"conflict":"new"}`,
			},
		},
		{
			name:                "nil existing annotations initialized correctly",
			existingAnnotations: nil,
			dataplane: operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							Services: &operatorv1beta1.DataPlaneServices{
								Ingress: &operatorv1beta1.DataPlaneServiceOptions{
									ServiceOptions: operatorv1beta1.ServiceOptions{
										Annotations: map[string]string{"k": "v"},
									},
								},
							},
						},
					},
				},
			},
			expectedAnnotations: map[string]string{
				"k":                                     "v",
				consts.AnnotationLastAppliedAnnotations: `{"k":"v"}`,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &corev1.Service{}
			svc.Annotations = tc.existingAnnotations
			addAnnotationsForDataPlaneIngressService(svc, tc.dataplane)
			require.Equal(t, tc.expectedAnnotations, svc.Annotations)
		})
	}
}

func TestAddLabelsForDataPlaneIngressService(t *testing.T) {
	testCases := []struct {
		name           string
		existingLabels map[string]string
		dataplane      operatorv1beta1.DataPlane
		expectedLabels map[string]string
	}{
		{
			name:           "no-op when DataPlane has no ingress service labels",
			existingLabels: map[string]string{"existing": "val"},
			dataplane:      operatorv1beta1.DataPlane{},
			expectedLabels: map[string]string{"existing": "val"},
		},
		{
			name:           "labels merged onto object",
			existingLabels: map[string]string{"existing": "val"},
			dataplane: operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							Services: &operatorv1beta1.DataPlaneServices{
								Ingress: &operatorv1beta1.DataPlaneServiceOptions{
									ServiceOptions: operatorv1beta1.ServiceOptions{
										Labels: map[operatorv1beta1.LabelName]operatorv1beta1.LabelValue{"new-label": "new-val"},
									},
								},
							},
						},
					},
				},
			},
			expectedLabels: map[string]string{
				"existing":  "val",
				"new-label": "new-val",
			},
		},
		{
			name:           "spec label wins over existing on conflict",
			existingLabels: map[string]string{"conflict": "old"},
			dataplane: operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							Services: &operatorv1beta1.DataPlaneServices{
								Ingress: &operatorv1beta1.DataPlaneServiceOptions{
									ServiceOptions: operatorv1beta1.ServiceOptions{
										Labels: map[operatorv1beta1.LabelName]operatorv1beta1.LabelValue{"conflict": "new"},
									},
								},
							},
						},
					},
				},
			},
			expectedLabels: map[string]string{"conflict": "new"},
		},
		{
			name:           "nil existing labels initialized correctly",
			existingLabels: nil,
			dataplane: operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							Services: &operatorv1beta1.DataPlaneServices{
								Ingress: &operatorv1beta1.DataPlaneServiceOptions{
									ServiceOptions: operatorv1beta1.ServiceOptions{
										Labels: map[operatorv1beta1.LabelName]operatorv1beta1.LabelValue{"k": "v"},
									},
								},
							},
						},
					},
				},
			},
			expectedLabels: map[string]string{"k": "v"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &corev1.Service{}
			svc.Labels = tc.existingLabels
			addLabelsForDataPlaneIngressService(svc, tc.dataplane)
			require.Equal(t, tc.expectedLabels, svc.Labels)
		})
	}
}

func TestAddAnnotationsForDataPlaneDeployment(t *testing.T) {
	testCases := []struct {
		name                string
		existingAnnotations map[string]string
		specAnnotations     map[string]string
		expectedAnnotations map[string]string
		expectedInfoCount   int
	}{
		{
			name:                "no-op when DataPlane has no deployment annotations",
			existingAnnotations: map[string]string{"existing": "val"},
			expectedAnnotations: map[string]string{"existing": "val"},
		},
		{
			name:                "new keys merged, conflicting keys overridden, last-applied tracked",
			existingAnnotations: map[string]string{"existing": "val", "conflict": "old"},
			specAnnotations:     map[string]string{"new": "val", "conflict": "new"},
			expectedAnnotations: map[string]string{
				"existing":                              "val",
				"new":                                   "val",
				"conflict":                              "new",
				consts.AnnotationLastAppliedAnnotations: `{"conflict":"new","new":"val"}`,
			},
		},
		{
			name:            "nil existing annotations initialized correctly",
			specAnnotations: map[string]string{"k": "v"},
			expectedAnnotations: map[string]string{
				"k":                                     "v",
				consts.AnnotationLastAppliedAnnotations: `{"k":"v"}`,
			},
		},
		{
			name: "reserved keys are dropped and a warning is logged",
			specAnnotations: map[string]string{
				"safe":                           "val",
				consts.OperatorLabelPrefix + "x": "val",
			},
			expectedAnnotations: map[string]string{
				"safe":                                  "val",
				consts.AnnotationLastAppliedAnnotations: `{"safe":"val"}`,
			},
			expectedInfoCount: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dataplane := operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{Annotations: tc.specAnnotations},
						},
					},
				},
			}
			deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Annotations: tc.existingAnnotations}}
			var infoCount int
			addAnnotationsForDataPlaneDeployment(logr.New(infoCountSink{count: &infoCount}), deployment, dataplane)
			require.Equal(t, tc.expectedAnnotations, deployment.Annotations)
			assert.Equal(t, tc.expectedInfoCount, infoCount)
		})
	}
}

func TestAddLabelsForDataPlaneDeployment(t *testing.T) {
	testCases := []struct {
		name              string
		existingLabels    map[string]string
		specLabels        map[string]string
		expectedLabels    map[string]string
		expectedInfoCount int
	}{
		{
			name:           "no-op when DataPlane has no deployment labels",
			existingLabels: map[string]string{"existing": "val"},
			expectedLabels: map[string]string{"existing": "val"},
		},
		{
			name:           "new keys merged, conflicting keys overridden",
			existingLabels: map[string]string{"existing": "val", "conflict": "old"},
			specLabels:     map[string]string{"new": "val", "conflict": "new"},
			expectedLabels: map[string]string{"existing": "val", "new": "val", "conflict": "new"},
		},
		{
			name:           "nil existing labels initialized correctly",
			specLabels:     map[string]string{"k": "v"},
			expectedLabels: map[string]string{"k": "v"},
		},
		{
			name:              "reserved keys are dropped and a warning is logged",
			specLabels:        map[string]string{"safe": "val", "app": "should-not-override-selector"},
			expectedLabels:    map[string]string{"safe": "val"},
			expectedInfoCount: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dataplane := operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{Labels: tc.specLabels},
						},
					},
				},
			}
			deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: tc.existingLabels}}
			var infoCount int
			addLabelsForDataPlaneDeployment(logr.New(infoCountSink{count: &infoCount}), deployment, dataplane)
			require.Equal(t, tc.expectedLabels, deployment.Labels)
			assert.Equal(t, tc.expectedInfoCount, infoCount)
		})
	}
}

func TestIsDeploymentReady(t *testing.T) {
	testCases := []struct {
		name           string
		deployment     *appsv1.Deployment
		expectedStatus metav1.ConditionStatus
		expectedReady  bool
	}{
		{
			name: "zero replicas in status",
			deployment: &appsv1.Deployment{
				Status: appsv1.DeploymentStatus{
					Replicas: 0,
				},
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReady:  false,
		},
		{
			name: "available replicas less than spec replicas",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Replicas: new(int32(3)),
				},
				Status: appsv1.DeploymentStatus{
					Replicas:          3,
					AvailableReplicas: 1,
				},
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReady:  false,
		},
		{
			name: "available replicas equal to spec replicas",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Replicas: new(int32(3)),
				},
				Status: appsv1.DeploymentStatus{
					Replicas:          3,
					AvailableReplicas: 3,
				},
			},
			expectedStatus: metav1.ConditionTrue,
			expectedReady:  true,
		},
		{
			name: "available replicas greater than spec replicas",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Replicas: new(int32(2)),
				},
				Status: appsv1.DeploymentStatus{
					Replicas:          3,
					AvailableReplicas: 3,
				},
			},
			expectedStatus: metav1.ConditionTrue,
			expectedReady:  true,
		},
		{
			name: "spec replicas nil with non-zero status replicas",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Replicas: nil,
				},
				Status: appsv1.DeploymentStatus{
					Replicas:          1,
					AvailableReplicas: 1,
				},
			},
			expectedStatus: metav1.ConditionTrue,
			expectedReady:  true,
		},
		{
			name: "zero available replicas with non-zero spec",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Replicas: new(int32(1)),
				},
				Status: appsv1.DeploymentStatus{
					Replicas:          1,
					AvailableReplicas: 0,
				},
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReady:  false,
		},
		{
			name: "rolling update: status replicas exceed spec but available meets spec",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Replicas: new(int32(3)),
				},
				Status: appsv1.DeploymentStatus{
					Replicas:          5, // old + new pods during rollout
					AvailableReplicas: 3,
				},
			},
			expectedStatus: metav1.ConditionTrue,
			expectedReady:  true,
		},
		{
			// Old replicas keep the Deployment "available" during a rollout, but
			// once Kubernetes gives up on the rollout (ProgressDeadlineExceeded)
			// this must not be masked by the anti-flap replica check above.
			name: "available replicas meet spec but rollout has stalled",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Replicas: new(int32(1)),
				},
				Status: appsv1.DeploymentStatus{
					Replicas:          2,
					AvailableReplicas: 1,
					Conditions: []appsv1.DeploymentCondition{
						{
							Type:   appsv1.DeploymentProgressing,
							Status: corev1.ConditionFalse,
							Reason: "ProgressDeadlineExceeded",
						},
					},
				},
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReady:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			status, ready := isDeploymentReady(tc.deployment)
			assert.Equal(t, tc.expectedStatus, status)
			assert.Equal(t, tc.expectedReady, ready)
		})
	}
}

func TestSetDeploymentRolledOutCondition(t *testing.T) {
	testCases := []struct {
		name       string
		deployment *appsv1.Deployment
		generation int64
		expected   metav1.Condition
	}{
		{
			name:       "no Deployment yet",
			deployment: nil,
			generation: 102,
			expected: k8sutils.NewConditionWithGeneration(
				kcfgdataplane.DeploymentRolledOutType,
				metav1.ConditionFalse,
				kcfgdataplane.DeploymentRolloutProgressingReason,
				"Deployment not present yet",
				102,
			),
		},
		{
			name: "fully rolled out",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 102},
				Spec:       appsv1.DeploymentSpec{Replicas: new(int32(1))},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 102,
					Replicas:           1,
					UpdatedReplicas:    1,
					AvailableReplicas:  1,
				},
			},
			generation: 102,
			expected: k8sutils.NewConditionWithGeneration(
				kcfgdataplane.DeploymentRolledOutType,
				metav1.ConditionTrue,
				kcfgdataplane.DeploymentRolloutCompleteReason,
				"All replicas run the current generation",
				102,
			),
		},
		{
			name: "mid-rollout",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 102},
				Spec:       appsv1.DeploymentSpec{Replicas: new(int32(1))},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 102,
					Replicas:           2,
					UpdatedReplicas:    1,
					AvailableReplicas:  1,
				},
			},
			generation: 102,
			expected: k8sutils.NewConditionWithGeneration(
				kcfgdataplane.DeploymentRolledOutType,
				metav1.ConditionFalse,
				kcfgdataplane.DeploymentRolloutProgressingReason,
				"Waiting for the Deployment to roll out",
				102,
			),
		},
		{
			name: "rollout stalled",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 102},
				Spec:       appsv1.DeploymentSpec{Replicas: new(int32(1))},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 102,
					Replicas:           2,
					UpdatedReplicas:    1,
					AvailableReplicas:  1,
					Conditions: []appsv1.DeploymentCondition{
						{
							Type:    appsv1.DeploymentProgressing,
							Status:  corev1.ConditionFalse,
							Reason:  "ProgressDeadlineExceeded",
							Message: "ReplicaSet has timed out progressing",
						},
					},
				},
			},
			generation: 102,
			expected: k8sutils.NewConditionWithGeneration(
				kcfgdataplane.DeploymentRolledOutType,
				metav1.ConditionFalse,
				kcfgdataplane.DeploymentRolloutStalledReason,
				"ReplicaSet has timed out progressing",
				102,
			),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dataplane := &operatorv1beta1.DataPlane{}
			setDeploymentRolledOutCondition(dataplane, tc.deployment, tc.generation)
			got, ok := k8sutils.GetCondition(kcfgdataplane.DeploymentRolledOutType, dataplane)
			require.True(t, ok)
			got.LastTransitionTime = tc.expected.LastTransitionTime
			assert.Equal(t, tc.expected, got)
		})
	}
}
