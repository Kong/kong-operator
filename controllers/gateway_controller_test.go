package controllers

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	controllerruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	dataplaneutils "github.com/kong/gateway-operator/internal/utils/dataplane"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/pkg/vars"
)

func init() {
	if err := gatewayv1beta1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding gatewayv1beta1 scheme")
		os.Exit(1)
	}
	if err := operatorv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding operatorv1alpha1 scheme")
		os.Exit(1)
	}
}

func TestGatewayReconciler_Reconcile(t *testing.T) {
	testCases := []struct {
		name                     string
		gatewayReq               reconcile.Request
		gatewayClass             *gatewayv1beta1.GatewayClass
		gateway                  *gwtypes.Gateway
		gatewaySubResources      []controllerruntimeclient.Object
		dataplaneSubResources    []controllerruntimeclient.Object
		controlplaneSubResources []controllerruntimeclient.Object
		testBody                 func(t *testing.T, reconciler GatewayReconciler, gatewayReq reconcile.Request)
	}{
		{
			name: "service connectivity",
			gatewayReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test-namespace",
					Name:      "test-gateway",
				},
			},
			gatewayClass: &gatewayv1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gatewayclass",
				},
				Spec: gatewayv1beta1.GatewayClassSpec{
					ControllerName: gatewayv1beta1.GatewayController(vars.ControllerName()),
				},
				Status: gatewayv1beta1.GatewayClassStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1beta1.GatewayClassConditionStatusAccepted),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 0,
							LastTransitionTime: metav1.Now(),
							Reason:             string(gatewayv1beta1.GatewayClassReasonAccepted),
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
				Spec: gatewayv1beta1.GatewaySpec{
					GatewayClassName: "test-gatewayclass",
				},
			},
			gatewaySubResources: []controllerruntimeclient.Object{
				&operatorv1alpha1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dataplane",
						Namespace: "test-namespace",
						UID:       types.UID(uuid.NewString()),
					},
					Status: operatorv1alpha1.DataPlaneStatus{
						Conditions: []metav1.Condition{
							k8sutils.NewCondition(k8sutils.ReadyType, metav1.ConditionTrue, k8sutils.ResourceReadyReason, ""),
						},
					},
				},
				&operatorv1alpha1.ControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-controlplane",
						Namespace: "test-namespace",
						UID:       types.UID(uuid.NewString()),
					},
					Spec: operatorv1alpha1.ControlPlaneSpec{
						ControlPlaneOptions: operatorv1alpha1.ControlPlaneOptions{
							DataPlane: pointer.String("test-dataplane"),
						},
					},
					Status: operatorv1alpha1.ControlPlaneStatus{
						Conditions: []metav1.Condition{
							k8sutils.NewCondition(k8sutils.ReadyType, metav1.ConditionTrue, k8sutils.ResourceReadyReason, ""),
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
							consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneAdminServiceLabelValue),
						},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: corev1.ClusterIPNone,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-proxy-service",
						Namespace: "test-namespace",
						Labels: map[string]string{
							consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneProxyServiceLabelValue),
						},
					},
				},
			},
			testBody: func(t *testing.T, reconciler GatewayReconciler, gatewayReq reconcile.Request) {
				ctx := context.Background()

				// These addresses are just placeholders, their value doesn't matter. No check is performed in the Gateway-controller,
				// apart from the existence of an address.
				clusterIP := "10.96.1.50"
				loadBalancerIP := "172.18.1.18"
				otherBalancerIP := "172.18.1.19"
				exampleHostname := "host.example.com"

				IPAddressTypePointer := (*gatewayv1beta1.AddressType)(pointer.String(string(gatewayv1beta1.IPAddressType)))
				HostnameAddressTypePointer := (*gatewayv1beta1.AddressType)(pointer.String(string(gatewayv1beta1.HostnameAddressType)))

				t.Log("first reconciliation, the dataplane has no IP assigned")
				// the dataplane service starts with no IP assigned, the gateway must be not ready
				_, err := reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")
				// need to trigger the Reconcile again because the first one only updated the finalizers
				_, err = reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")
				// need to trigger the Reconcile again because the previous updated the NetworkPolicy
				_, err = reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")

				var currentGateway gwtypes.Gateway
				require.NoError(t, reconciler.Client.Get(ctx, gatewayReq.NamespacedName, &currentGateway))
				require.False(t, k8sutils.IsReady(gatewayConditionsAware(&currentGateway)))
				condition, found := k8sutils.GetCondition(GatewayServiceType, gatewayConditionsAware(&currentGateway))
				require.True(t, found)
				require.Equal(t, condition.Status, metav1.ConditionFalse)
				require.Equal(t, k8sutils.ConditionReason(condition.Reason), GatewayServiceErrorReason)
				require.Len(t, currentGateway.Status.Addresses, 0)

				t.Log("adding a ClusterIP to the dataplane service")
				dataplaneService := &corev1.Service{}
				require.NoError(t, reconciler.Client.Get(ctx, types.NamespacedName{Namespace: "test-namespace", Name: "test-proxy-service"}, dataplaneService))
				dataplaneService.Spec = corev1.ServiceSpec{
					ClusterIP: clusterIP,
					Type:      corev1.ServiceTypeClusterIP,
				}
				require.NoError(t, reconciler.Client.Update(ctx, dataplaneService))
				_, err = reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")
				// the dataplane service now has a clusterIP assigned, the gateway must be ready
				require.NoError(t, reconciler.Client.Get(ctx, gatewayReq.NamespacedName, &currentGateway))
				require.True(t, k8sutils.IsReady(gatewayConditionsAware(&currentGateway)))
				condition, found = k8sutils.GetCondition(GatewayServiceType, gatewayConditionsAware(&currentGateway))
				require.True(t, found)
				require.Equal(t, condition.Status, metav1.ConditionTrue)
				require.Equal(t, k8sutils.ConditionReason(condition.Reason), k8sutils.ResourceReadyReason)
				require.Equal(t,
					[]gwtypes.GatewayAddress{
						{
							Type:  IPAddressTypePointer,
							Value: clusterIP,
						},
					},
					currentGateway.Status.Addresses,
				)

				t.Log("adding a LoadBalancer IP to the dataplane service")
				dataplaneService.Spec.Type = corev1.ServiceTypeLoadBalancer
				require.NoError(t, reconciler.Client.Update(ctx, dataplaneService))
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
				require.NoError(t, reconciler.Client.Get(ctx, gatewayReq.NamespacedName, &currentGateway))
				require.True(t, k8sutils.IsReady(gatewayConditionsAware(&currentGateway)))
				condition, found = k8sutils.GetCondition(GatewayServiceType, gatewayConditionsAware(&currentGateway))
				require.True(t, found)
				require.Equal(t, condition.Status, metav1.ConditionTrue)
				require.Equal(t, k8sutils.ConditionReason(condition.Reason), k8sutils.ResourceReadyReason)
				require.Equal(t,
					[]gwtypes.GatewayAddress{
						{
							Type:  IPAddressTypePointer,
							Value: loadBalancerIP,
						},
						{
							Type:  IPAddressTypePointer,
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
				require.NoError(t, reconciler.Client.Get(ctx, gatewayReq.NamespacedName, &currentGateway))
				require.True(t, k8sutils.IsReady(gatewayConditionsAware(&currentGateway)))
				condition, found = k8sutils.GetCondition(GatewayServiceType, gatewayConditionsAware(&currentGateway))
				require.True(t, found)
				require.Equal(t, condition.Status, metav1.ConditionTrue)
				require.Equal(t, k8sutils.ConditionReason(condition.Reason), k8sutils.ResourceReadyReason)
				require.Equal(t, currentGateway.Status.Addresses, []gwtypes.GatewayAddress{
					{
						Type:  HostnameAddressTypePointer,
						Value: exampleHostname,
					},
				})

				t.Log("removing the ClusterIP from the dataplane service")
				dataplaneService.Spec = corev1.ServiceSpec{
					ClusterIP: "",
				}
				require.NoError(t, reconciler.Client.Update(ctx, dataplaneService))
				_, err = reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")
				require.NoError(t, reconciler.Client.Get(ctx, gatewayReq.NamespacedName, &currentGateway))
				// the dataplane service has no clusterIP assigned, the gateway must be not ready
				// and no addresses must be assigned
				require.False(t, k8sutils.IsReady(gatewayConditionsAware(&currentGateway)))
				condition, found = k8sutils.GetCondition(GatewayServiceType, gatewayConditionsAware(&currentGateway))
				require.True(t, found)
				require.Equal(t, condition.Status, metav1.ConditionFalse)
				require.Equal(t, k8sutils.ConditionReason(condition.Reason), GatewayServiceErrorReason)
				require.Len(t, currentGateway.Status.Addresses, 0)
			},
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			ObjectsToAdd := []controllerruntimeclient.Object{
				tc.gateway,
				tc.gatewayClass,
			}
			for _, gatewaySubResource := range tc.gatewaySubResources {
				k8sutils.SetOwnerForObject(gatewaySubResource, tc.gateway)
				gatewayutils.LabelObjectAsGatewayManaged(gatewaySubResource)
				if gatewaySubResource.GetName() == "test-dataplane" {
					dataplane := gatewaySubResource.(*operatorv1alpha1.DataPlane)
					dataplaneutils.SetDataPlaneDefaults(&dataplane.Spec.DataPlaneOptions)
					for _, dataplaneSubresource := range tc.dataplaneSubResources {
						k8sutils.SetOwnerForObject(dataplaneSubresource, gatewaySubResource)
						addLabelForDataplane(dataplaneSubresource)
						ObjectsToAdd = append(ObjectsToAdd, dataplaneSubresource)
					}
				}
				if gatewaySubResource.GetName() == "test-controlplane" {
					controlplane := gatewaySubResource.(*operatorv1alpha1.ControlPlane)
					_ = setControlPlaneDefaults(&controlplane.Spec.ControlPlaneOptions, map[string]struct{}{}, controlPlaneDefaultsArgs{
						namespace:                 "test-namespace",
						dataplaneProxyServiceName: "test-proxy-service",
					})
					for _, controlplaneSubResource := range tc.controlplaneSubResources {
						k8sutils.SetOwnerForObject(controlplaneSubResource, gatewaySubResource)
						addLabelForControlPlane(controlplaneSubResource)
						ObjectsToAdd = append(ObjectsToAdd, controlplaneSubResource)
					}
				}
				ObjectsToAdd = append(ObjectsToAdd, gatewaySubResource)
			}

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(ObjectsToAdd...).
				WithStatusSubresource(ObjectsToAdd...).
				Build()

			reconciler := GatewayReconciler{
				Client: fakeClient,
			}

			tc.testBody(t, reconciler, tc.gatewayReq)
		})
	}
}

func Test_setControlPlaneOptionsDefaults(t *testing.T) {
	testcases := []struct {
		name     string
		input    operatorv1alpha1.ControlPlaneOptions
		expected operatorv1alpha1.ControlPlaneOptions
	}{
		{
			name:  "no providing any options",
			input: operatorv1alpha1.ControlPlaneOptions{},
			expected: operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Replicas: lo.ToPtr(int32(1)),
					Pods: operatorv1alpha1.PodsOptions{
						ContainerImage: lo.ToPtr(consts.DefaultControlPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultControlPlaneTag),
					},
				},
			},
		},
		{
			name: "providing only replicas",
			input: operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Replicas: lo.ToPtr(int32(10)),
				},
			},
			expected: operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Replicas: lo.ToPtr(int32(10)),
					Pods: operatorv1alpha1.PodsOptions{
						ContainerImage: lo.ToPtr(consts.DefaultControlPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultControlPlaneTag),
					},
				},
			},
		},
		{
			name: "providing only replicas that are equal to default",
			input: operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Replicas: lo.ToPtr(int32(1)),
				},
			},
			expected: operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Replicas: lo.ToPtr(int32(1)),
					Pods: operatorv1alpha1.PodsOptions{
						ContainerImage: lo.ToPtr(consts.DefaultControlPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultControlPlaneTag),
					},
				},
			},
		},
		{
			name: "providing more options",
			input: operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Replicas: lo.ToPtr(int32(10)),
					Pods: operatorv1alpha1.PodsOptions{
						ContainerImage: lo.ToPtr("image"),
						Version:        lo.ToPtr("version"),
					},
				},
			},
			expected: operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Replicas: lo.ToPtr(int32(10)),
					Pods: operatorv1alpha1.PodsOptions{
						ContainerImage: lo.ToPtr("image"),
						Version:        lo.ToPtr("version"),
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			setControlPlaneOptionsDefaults(&tc.input)
			require.Equal(t, tc.expected, tc.input)
		})
	}
}

func Test_setDataPlaneOptionsDefaults(t *testing.T) {
	testcases := []struct {
		name     string
		input    operatorv1alpha1.DataPlaneOptions
		expected operatorv1alpha1.DataPlaneOptions
	}{
		{
			name:  "no providing any options",
			input: operatorv1alpha1.DataPlaneOptions{},
			expected: operatorv1alpha1.DataPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Replicas: lo.ToPtr(int32(1)),
					Pods: operatorv1alpha1.PodsOptions{
						ContainerImage: lo.ToPtr(consts.DefaultDataPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultDataPlaneTag),
					},
				},
			},
		},
		{
			name: "providing only replicas",
			input: operatorv1alpha1.DataPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Replicas: lo.ToPtr(int32(10)),
				},
			},
			expected: operatorv1alpha1.DataPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Replicas: lo.ToPtr(int32(10)),
					Pods: operatorv1alpha1.PodsOptions{
						ContainerImage: lo.ToPtr(consts.DefaultDataPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultDataPlaneTag),
					},
				},
			},
		},
		{
			name: "providing only replicas that are equal to default",
			input: operatorv1alpha1.DataPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Replicas: lo.ToPtr(int32(1)),
				},
			},
			expected: operatorv1alpha1.DataPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Replicas: lo.ToPtr(int32(1)),
					Pods: operatorv1alpha1.PodsOptions{
						ContainerImage: lo.ToPtr(consts.DefaultDataPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultDataPlaneTag),
					},
				},
			},
		},
		{
			name: "providing more options",
			input: operatorv1alpha1.DataPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Replicas: lo.ToPtr(int32(10)),
					Pods: operatorv1alpha1.PodsOptions{
						ContainerImage: lo.ToPtr("image"),
						Version:        lo.ToPtr("version"),
					},
				},
			},
			expected: operatorv1alpha1.DataPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Replicas: lo.ToPtr(int32(10)),
					Pods: operatorv1alpha1.PodsOptions{
						ContainerImage: lo.ToPtr("image"),
						Version:        lo.ToPtr("version"),
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			setDataPlaneOptionsDefaults(&tc.input)
			require.Equal(t, tc.expected, tc.input)
		})
	}
}

func BenchmarkGatewayReconciler_Reconcile(b *testing.B) {
	gatewayClass := &gatewayv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gatewayclass",
		},
		Spec: gatewayv1beta1.GatewayClassSpec{
			ControllerName: gatewayv1beta1.GatewayController(vars.ControllerName()),
		},
		Status: gatewayv1beta1.GatewayClassStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(gatewayv1beta1.GatewayClassConditionStatusAccepted),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 0,
					LastTransitionTime: metav1.Now(),
					Reason:             string(gatewayv1beta1.GatewayClassReasonAccepted),
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
		Spec: gatewayv1beta1.GatewaySpec{
			GatewayClassName: "test-gatewayclass",
		},
	}

	fakeClient := fakectrlruntimeclient.
		NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(gateway, gatewayClass).
		Build()

	reconciler := GatewayReconciler{
		Client: fakeClient,
	}

	gatewayReq := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "test-namespace",
			Name:      "test-gateway",
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := reconciler.Reconcile(context.Background(), gatewayReq)
		if err != nil {
			b.Error(err)
		}
	}
}
