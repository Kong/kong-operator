package gateway

import (
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	controllerruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kcfgconsts "github.com/kong/kubernetes-configuration/v2/api/common/consts"
	kcfgdataplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/dataplane"
	kcfggateway "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/gateway"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	gatewayutils "github.com/kong/kong-operator/v2/pkg/utils/gateway"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
	"github.com/kong/kong-operator/v2/pkg/vars"
)

func TestGatewayReconciler_Reconcile(t *testing.T) {
	testCases := []struct {
		name                     string
		gatewayReq               reconcile.Request
		gatewayClass             *gatewayv1.GatewayClass
		gateway                  *gwtypes.Gateway
		gatewaySubResources      []controllerruntimeclient.Object
		dataplaneSubResources    []controllerruntimeclient.Object
		controlplaneSubResources []controllerruntimeclient.Object
		testBody                 func(t *testing.T, reconciler Reconciler, gatewayReq reconcile.Request)
	}{
		{
			name: "gateway class not found - reconciliation should fail",
			gatewayReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test-namespace",
					Name:      "test-gateway",
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway.networking.k8s.io/v1beta1",
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "not-existing-gatewayclass",
				},
			},
			testBody: func(t *testing.T, r Reconciler, gatewayReq reconcile.Request) {
				ctx := t.Context()
				_, err := r.Reconcile(ctx, gatewayReq)
				require.Error(t, err)
			},
		},
		{
			name: "gateway class found, but controller name is not matching - gateway is ignored",
			gatewayReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test-namespace",
					Name:      "test-gateway",
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway.networking.k8s.io/v1beta1",
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "test-gatewayclass",
				},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gatewayclass",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: gatewayv1.GatewayController("not-existing-controller"),
				},
				Status: gatewayv1.GatewayClassStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 0,
							LastTransitionTime: metav1.Now(),
							Reason:             string(gatewayv1.GatewayClassReasonAccepted),
							Message:            "the gatewayclass has been accepted by the controller",
						},
					},
				},
			},
			testBody: func(t *testing.T, r Reconciler, gatewayReq reconcile.Request) {
				ctx := t.Context()
				res, err := r.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation should not return an error")
				require.Equal(t, reconcile.Result{}, res, "reconciliation should not return a requeue")

				var gw gwtypes.Gateway
				require.NoError(t, r.Get(ctx, gatewayReq.NamespacedName, &gw))

				require.Empty(t, gw.GetFinalizers(), "gateway should not have any finalizers as it's ignored")
			},
		},
		{
			name: "service connectivity",
			gatewayReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test-namespace",
					Name:      "test-gateway",
				},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gatewayclass",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
				},
				Status: gatewayv1.GatewayClassStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 0,
							LastTransitionTime: metav1.Now(),
							Reason:             string(gatewayv1.GatewayClassReasonAccepted),
							Message:            "the gatewayclass has been accepted by the controller",
						},
					},
				},
			},
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway.networking.k8s.io/v1beta1",
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "test-gatewayclass",
				},
			},
			gatewaySubResources: []controllerruntimeclient.Object{
				&operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dataplane",
						Namespace: "test-namespace",
						UID:       types.UID(uuid.NewString()),
					},
					Status: operatorv1beta1.DataPlaneStatus{
						Conditions: []metav1.Condition{
							k8sutils.NewCondition(kcfgdataplane.ReadyType, metav1.ConditionTrue, kcfgdataplane.ResourceReadyReason, ""),
						},
					},
				},
				&gwtypes.ControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-controlplane",
						Namespace: "test-namespace",
						UID:       types.UID(uuid.NewString()),
					},
					Spec: gwtypes.ControlPlaneSpec{
						DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
							Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
							Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
								Name: "test-dataplane",
							},
						},
					},
					Status: gwtypes.ControlPlaneStatus{
						Conditions: []metav1.Condition{
							k8sutils.NewCondition(kcfgdataplane.ReadyType, metav1.ConditionTrue, kcfgdataplane.ResourceReadyReason, ""),
						},
					},
				},
				&networkingv1.NetworkPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-namespace",
						Name:      "test-networkpolicy",
					},
				},
			},
			dataplaneSubResources: []controllerruntimeclient.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-admin-service",
						Namespace: "test-namespace",
						Labels: map[string]string{
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
						},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: corev1.ClusterIPNone,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-ingress-service",
						Namespace: "test-namespace",
						Labels: map[string]string{
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
						},
					},
				},
			},
			testBody: func(t *testing.T, reconciler Reconciler, gatewayReq reconcile.Request) {
				ctx := t.Context()

				// These addresses are just placeholders, their value doesn't matter. No check is performed in the Gateway-controller,
				// apart from the existence of an address.
				clusterIP := "10.96.1.50"
				loadBalancerIP := "172.18.1.18"
				otherBalancerIP := "172.18.1.19"
				exampleHostname := "host.example.com"

				t.Log("first reconciliation, the dataplane has no IP assigned")
				// the dataplane service starts with no IP assigned, the gateway must be not ready
				_, err := reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")

				t.Log("verifying the gateway gets finalizers assigned")
				var gateway gwtypes.Gateway
				require.NoError(t, reconciler.Get(ctx, gatewayReq.NamespacedName, &gateway))
				require.ElementsMatch(t, gateway.GetFinalizers(), []string{
					string(GatewayFinalizerCleanupControlPlanes),
					string(GatewayFinalizerCleanupDataPlanes),
					string(GatewayFinalizerCleanupNetworkPolicies),
				})

				// need to trigger the Reconcile again because the first one only updated the finalizers
				_, err = reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")
				// need to trigger the Reconcile again because the previous updated the Gateway Status
				_, err = reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")
				// need to trigger the Reconcile again because the previous updated the NetworkPolicy
				_, err = reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")

				var currentGateway gwtypes.Gateway
				require.NoError(t, reconciler.Get(ctx, gatewayReq.NamespacedName, &currentGateway))
				require.False(t, k8sutils.IsProgrammed(gatewayConditionsAndListenersAware(&currentGateway)))
				condition, found := k8sutils.GetCondition(kcfggateway.GatewayServiceType, gatewayConditionsAndListenersAware(&currentGateway))
				require.True(t, found)
				require.Equal(t, metav1.ConditionFalse, condition.Status)
				require.Equal(t, kcfggateway.GatewayReasonServiceError, kcfgconsts.ConditionReason(condition.Reason))
				require.Empty(t, currentGateway.Status.Addresses)

				t.Log("adding a ClusterIP to the dataplane service")
				dataplaneService := &corev1.Service{}
				require.NoError(t, reconciler.Get(ctx, types.NamespacedName{Namespace: "test-namespace", Name: "test-ingress-service"}, dataplaneService))
				dataplaneService.Spec = corev1.ServiceSpec{
					ClusterIP: clusterIP,
					Type:      corev1.ServiceTypeClusterIP,
				}
				require.NoError(t, reconciler.Update(ctx, dataplaneService))
				_, err = reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")
				// the dataplane service now has a clusterIP assigned, the gateway must be ready
				require.NoError(t, reconciler.Get(ctx, gatewayReq.NamespacedName, &currentGateway))
				require.True(t, k8sutils.IsProgrammed(gatewayConditionsAndListenersAware(&currentGateway)))
				condition, found = k8sutils.GetCondition(kcfggateway.GatewayServiceType, gatewayConditionsAndListenersAware(&currentGateway))
				require.True(t, found)
				require.Equal(t, metav1.ConditionTrue, condition.Status)
				require.Equal(t, kcfgdataplane.ResourceReadyReason, kcfgconsts.ConditionReason(condition.Reason))
				require.Equal(t,
					[]gwtypes.GatewayStatusAddress{
						{
							Type:  lo.ToPtr(gatewayv1.IPAddressType),
							Value: clusterIP,
						},
					},
					currentGateway.Status.Addresses,
				)

				t.Log("adding a LoadBalancer IP to the dataplane service")
				dataplaneService.Spec.Type = corev1.ServiceTypeLoadBalancer
				require.NoError(t, reconciler.Update(ctx, dataplaneService))
				dataplaneService.Status = corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: loadBalancerIP,
							},
							{
								IP: otherBalancerIP,
							},
						},
					},
				}
				require.NoError(t, reconciler.Client.Status().Update(ctx, dataplaneService))
				_, err = reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")
				require.NoError(t, reconciler.Get(ctx, gatewayReq.NamespacedName, &currentGateway))
				require.True(t, k8sutils.IsProgrammed(gatewayConditionsAndListenersAware(&currentGateway)))
				condition, found = k8sutils.GetCondition(kcfggateway.GatewayServiceType, gatewayConditionsAndListenersAware(&currentGateway))
				require.True(t, found)
				require.Equal(t, metav1.ConditionTrue, condition.Status)
				require.Equal(t, kcfgdataplane.ResourceReadyReason, kcfgconsts.ConditionReason(condition.Reason))
				require.Equal(t,
					[]gwtypes.GatewayStatusAddress{
						{
							Type:  lo.ToPtr(gatewayv1.IPAddressType),
							Value: loadBalancerIP,
						},
						{
							Type:  lo.ToPtr(gatewayv1.IPAddressType),
							Value: otherBalancerIP,
						},
					},
					currentGateway.Status.Addresses,
				)

				t.Log("replacing LoadBalancer IP with hostname")
				dataplaneService.Status = corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								Hostname: exampleHostname,
							},
						},
					},
				}
				require.NoError(t, reconciler.Client.Status().Update(ctx, dataplaneService))
				_, err = reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")
				require.NoError(t, reconciler.Get(ctx, gatewayReq.NamespacedName, &currentGateway))
				require.True(t, k8sutils.IsProgrammed(gatewayConditionsAndListenersAware(&currentGateway)))
				condition, found = k8sutils.GetCondition(kcfggateway.GatewayServiceType, gatewayConditionsAndListenersAware(&currentGateway))
				require.True(t, found)
				require.Equal(t, metav1.ConditionTrue, condition.Status)
				require.Equal(t, kcfgdataplane.ResourceReadyReason, kcfgconsts.ConditionReason(condition.Reason))
				require.Equal(t, []gwtypes.GatewayStatusAddress{
					{
						Type:  lo.ToPtr(gatewayv1.HostnameAddressType),
						Value: exampleHostname,
					},
				}, currentGateway.Status.Addresses)

				t.Log("removing the ClusterIP from the dataplane service")
				dataplaneService.Spec = corev1.ServiceSpec{
					ClusterIP: "",
				}
				require.NoError(t, reconciler.Update(ctx, dataplaneService))
				_, err = reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")
				require.NoError(t, reconciler.Get(ctx, gatewayReq.NamespacedName, &currentGateway))
				// the dataplane service has no clusterIP assigned, the gateway must be not ready
				// and no addresses must be assigned
				require.False(t, k8sutils.IsProgrammed(gatewayConditionsAndListenersAware(&currentGateway)))
				condition, found = k8sutils.GetCondition(kcfggateway.GatewayServiceType, gatewayConditionsAndListenersAware(&currentGateway))
				require.True(t, found)
				require.Equal(t, metav1.ConditionFalse, condition.Status)
				require.Equal(t, kcfggateway.GatewayReasonServiceError, kcfgconsts.ConditionReason(condition.Reason))
				require.Empty(t, currentGateway.Status.Addresses)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			objectsToAdd := []controllerruntimeclient.Object{
				tc.gateway,
			}
			if tc.gatewayClass != nil {
				objectsToAdd = append(objectsToAdd, tc.gatewayClass)
			}
			for _, gatewaySubResource := range tc.gatewaySubResources {
				k8sutils.SetOwnerForObject(gatewaySubResource, tc.gateway)
				gatewayutils.LabelObjectAsGatewayManaged(gatewaySubResource)
				if gatewaySubResource.GetName() == "test-dataplane" {
					for _, dataplaneSubresource := range tc.dataplaneSubResources {
						k8sutils.SetOwnerForObject(dataplaneSubresource, gatewaySubResource)
						objectsToAdd = append(objectsToAdd, dataplaneSubresource)
					}
				}
				if gatewaySubResource.GetName() == "test-controlplane" {
					for _, controlplaneSubResource := range tc.controlplaneSubResources {
						k8sutils.SetOwnerForObject(controlplaneSubResource, gatewaySubResource)
						objectsToAdd = append(objectsToAdd, controlplaneSubResource)
					}
				}
				objectsToAdd = append(objectsToAdd, gatewaySubResource)
			}

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(objectsToAdd...).
				WithStatusSubresource(objectsToAdd...).
				Build()

			reconciler := Reconciler{
				Client: fakeClient,
			}

			tc.testBody(t, reconciler, tc.gatewayReq)
		})
	}
}

func Test_setDataPlaneOptionsDefaults(t *testing.T) {
	testcases := []struct {
		name     string
		input    operatorv1beta1.DataPlaneOptions
		expected operatorv1beta1.DataPlaneOptions
	}{
		{
			name:  "no providing any options",
			input: operatorv1beta1.DataPlaneOptions{},
			expected: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(1)),
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:           consts.DataPlaneProxyContainerName,
										Image:          consts.DefaultDataPlaneImage,
										ReadinessProbe: k8sresources.GenerateDataPlaneReadinessProbe(consts.DataPlaneStatusReadyEndpoint),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "providing only replicas",
			input: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(10)),
					},
				},
			},
			expected: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(10)),
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:           consts.DataPlaneProxyContainerName,
										Image:          consts.DefaultDataPlaneImage,
										ReadinessProbe: k8sresources.GenerateDataPlaneReadinessProbe(consts.DataPlaneStatusReadyEndpoint),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "providing only replicas that are equal to default",
			input: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(1)),
					},
				},
			},
			expected: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(1)),
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:           consts.DataPlaneProxyContainerName,
										Image:          consts.DefaultDataPlaneImage,
										ReadinessProbe: k8sresources.GenerateDataPlaneReadinessProbe(consts.DataPlaneStatusReadyEndpoint),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "providing more options",
			input: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(10)),
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: "image:v1",
									},
								},
							},
						},
					},
				},
			},
			expected: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(10)),
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:           consts.DataPlaneProxyContainerName,
										Image:          "image:v1",
										ReadinessProbe: k8sresources.GenerateDataPlaneReadinessProbe(consts.DataPlaneStatusReadyEndpoint),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "providing more options",
			input: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(10)),
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: "image:v1",
										ReadinessProbe: &corev1.Probe{
											InitialDelaySeconds: 1,
											TimeoutSeconds:      1,
										},
									},
								},
							},
						},
					},
				},
			},
			expected: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(10)),
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: "image:v1",
										ReadinessProbe: &corev1.Probe{
											InitialDelaySeconds: 1,
											TimeoutSeconds:      1,
											ProbeHandler: corev1.ProbeHandler{
												HTTPGet: &corev1.HTTPGetAction{
													Path:   consts.DataPlaneStatusReadyEndpoint,
													Port:   intstr.FromInt(consts.DataPlaneMetricsPort),
													Scheme: corev1.URISchemeHTTP,
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
		{
			name: "defining scaling strategy should not set default replicas",
			input: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Scaling: &operatorv1beta1.Scaling{
							HorizontalScaling: &operatorv1beta1.HorizontalScaling{
								MaxReplicas: 10,
							},
						},
					},
				},
			},
			expected: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Scaling: &operatorv1beta1.Scaling{
							HorizontalScaling: &operatorv1beta1.HorizontalScaling{
								MaxReplicas: 10,
							},
						},
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:           consts.DataPlaneProxyContainerName,
										Image:          consts.DefaultDataPlaneImage,
										ReadinessProbe: k8sresources.GenerateDataPlaneReadinessProbe(consts.DataPlaneStatusReadyEndpoint),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "not providing the readiness probe sets it to default",
			input: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{},
			},
			expected: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(1)),
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:           consts.DataPlaneProxyContainerName,
										Image:          consts.DefaultDataPlaneImage,
										ReadinessProbe: k8sresources.GenerateDataPlaneReadinessProbe(consts.DataPlaneStatusReadyEndpoint),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "providing the readiness probe sets it as expected",
			input: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: consts.DefaultDataPlaneImage,
										ReadinessProbe: &corev1.Probe{
											FailureThreshold:    6,
											InitialDelaySeconds: 7,
											PeriodSeconds:       8,
											SuccessThreshold:    8,
											TimeoutSeconds:      9,
											ProbeHandler: corev1.ProbeHandler{
												HTTPGet: &corev1.HTTPGetAction{
													Path:   "/endpoint",
													Port:   intstr.FromInt(4567),
													Scheme: corev1.URISchemeHTTP,
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
			expected: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(1)),
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: consts.DefaultDataPlaneImage,
										ReadinessProbe: &corev1.Probe{
											FailureThreshold:    6,
											InitialDelaySeconds: 7,
											PeriodSeconds:       8,
											SuccessThreshold:    8,
											TimeoutSeconds:      9,
											ProbeHandler: corev1.ProbeHandler{
												HTTPGet: &corev1.HTTPGetAction{
													Path:   "/endpoint",
													Port:   intstr.FromInt(4567),
													Scheme: corev1.URISchemeHTTP,
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
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			setDataPlaneOptionsDefaults(&tc.input, consts.DefaultDataPlaneImage)
			require.Equal(t, tc.expected, tc.input)
		})
	}
}

func BenchmarkGatewayReconciler_Reconcile(b *testing.B) {
	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gatewayclass",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
		},
		Status: gatewayv1.GatewayClassStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 0,
					LastTransitionTime: metav1.Now(),
					Reason:             string(gatewayv1.GatewayClassReasonAccepted),
					Message:            "the gatewayclass has been accepted by the controller",
				},
			},
		},
	}
	gateway := &gwtypes.Gateway{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1beta1",
			Kind:       "Gateway",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "test-namespace",
			UID:       types.UID(uuid.NewString()),
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "test-gatewayclass",
		},
	}

	fakeClient := fakectrlruntimeclient.
		NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(gateway, gatewayClass).
		Build()

	reconciler := Reconciler{
		Client: fakeClient,
	}

	gatewayReq := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "test-namespace",
			Name:      "test-gateway",
		},
	}

	b.ResetTimer()

	for b.Loop() {
		_, err := reconciler.Reconcile(b.Context(), gatewayReq)
		if err != nil {
			b.Error(err)
		}
	}
}
