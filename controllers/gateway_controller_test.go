package controllers

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
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
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	dataplaneutils "github.com/kong/gateway-operator/internal/utils/dataplane"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/pkg/vars"
)

func init() {
	if err := gatewayv1alpha2.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding gatewayv1alpha2 scheme")
		os.Exit(1)
	}
	if err := operatorv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding operatorv1alpha1 scheme")
		os.Exit(1)
	}
}

func TestGatewayReconciler_Reconcile(t *testing.T) {
	var testCases = []struct {
		name                     string
		gatewayReq               reconcile.Request
		gatewayClass             *gatewayv1alpha2.GatewayClass
		gateway                  *gatewayv1alpha2.Gateway
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
			gatewayClass: &gatewayv1alpha2.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gatewayclass",
				},
				Spec: gatewayv1alpha2.GatewayClassSpec{
					ControllerName: gatewayv1alpha2.GatewayController(vars.ControllerName),
				},
			},
			gateway: &gatewayv1alpha2.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway.networking.k8s.io/v1alpha2",
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec: gatewayv1alpha2.GatewaySpec{
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
						ControlPlaneDeploymentOptions: operatorv1alpha1.ControlPlaneDeploymentOptions{
							DataPlane: pointer.StringPtr("test-dataplane"),
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
						Name:      "svc-test-dataplane",
						Namespace: "test-namespace",
					},
				},
			},
			testBody: func(t *testing.T, reconciler GatewayReconciler, gatewayReq reconcile.Request) {
				ctx := context.Background()

				// These addresses are just placeholders, their value doesn't matter. No check is performed in the Gateway-controller,
				// apart from the existence of an address.
				clusterIP := "10.96.1.50"
				loadBalancerIP := "172.18.1.18"

				IPAddressTypePointer := (*gatewayv1alpha2.AddressType)(pointer.StringPtr(string(gatewayv1alpha2.IPAddressType)))

				t.Log("first reconciliation, the dataplane has no IP assigned")
				currentGateway := newGateway()
				// the dataplane service starts with no IP assigned, the gateway must be not ready
				_, err := reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")
				require.NoError(t, reconciler.Client.Get(ctx, gatewayReq.NamespacedName, currentGateway.Gateway))
				require.False(t, k8sutils.IsReady(currentGateway))
				condition, found := k8sutils.GetCondition(GatewayServiceType, currentGateway)
				require.True(t, found)
				require.Equal(t, condition.Status, metav1.ConditionFalse)
				require.Equal(t, k8sutils.ConditionReason(condition.Reason), GatewayServiceErrorReason)
				require.Len(t, currentGateway.Status.Addresses, 0)

				t.Log("adding a ClusterIP to the dataplane service")
				dataplaneService := &corev1.Service{}
				require.NoError(t, reconciler.Client.Get(ctx, types.NamespacedName{Namespace: "test-namespace", Name: "svc-test-dataplane"}, dataplaneService))
				dataplaneService.Spec = corev1.ServiceSpec{
					ClusterIP: clusterIP,
				}
				require.NoError(t, reconciler.Client.Update(ctx, dataplaneService))
				_, err = reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")
				// the dataplane service now has a clusterIP assigned, the gateway must be ready
				require.NoError(t, reconciler.Client.Get(ctx, gatewayReq.NamespacedName, currentGateway.Gateway))
				require.True(t, k8sutils.IsReady(currentGateway))
				condition, found = k8sutils.GetCondition(GatewayServiceType, currentGateway)
				require.True(t, found)
				require.Equal(t, condition.Status, metav1.ConditionTrue)
				require.Equal(t, k8sutils.ConditionReason(condition.Reason), k8sutils.ResourceReadyReason)
				require.Equal(t, currentGateway.Status.Addresses, []gatewayv1alpha2.GatewayAddress{
					{
						Type:  IPAddressTypePointer,
						Value: clusterIP,
					},
				})

				t.Log("adding a LoadBalancer IP to the dataplane service")
				dataplaneService.Status = corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: loadBalancerIP,
							},
						},
					},
				}
				require.NoError(t, reconciler.Client.Status().Update(ctx, dataplaneService))
				_, err = reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")
				require.NoError(t, reconciler.Client.Get(ctx, gatewayReq.NamespacedName, currentGateway.Gateway))
				require.True(t, k8sutils.IsReady(currentGateway))
				condition, found = k8sutils.GetCondition(GatewayServiceType, currentGateway)
				require.True(t, found)
				require.Equal(t, condition.Status, metav1.ConditionTrue)
				require.Equal(t, k8sutils.ConditionReason(condition.Reason), k8sutils.ResourceReadyReason)
				require.Equal(t, currentGateway.Status.Addresses, []gatewayv1alpha2.GatewayAddress{
					{
						Type:  IPAddressTypePointer,
						Value: loadBalancerIP,
					},
					{
						Type:  IPAddressTypePointer,
						Value: clusterIP,
					},
				})

				t.Log("removing the ClusterIP from the dataplane service")
				dataplaneService.Spec = corev1.ServiceSpec{
					ClusterIP: "",
				}
				require.NoError(t, reconciler.Client.Update(ctx, dataplaneService))
				_, err = reconciler.Reconcile(ctx, gatewayReq)
				require.NoError(t, err, "reconciliation returned an error")
				require.NoError(t, reconciler.Client.Get(ctx, gatewayReq.NamespacedName, currentGateway.Gateway))
				// the dataplane service has no clusterIP assigned, the gateway must be not ready
				// and no addresses must be assigned
				require.False(t, k8sutils.IsReady(currentGateway))
				condition, found = k8sutils.GetCondition(GatewayServiceType, currentGateway)
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
					dataplaneutils.SetDataPlaneDefaults(&dataplane.Spec.DataPlaneDeploymentOptions)
					for _, dataplaneSubresource := range tc.dataplaneSubResources {
						k8sutils.SetOwnerForObject(dataplaneSubresource, gatewaySubResource)
						addLabelForDataplane(dataplaneSubresource)
						ObjectsToAdd = append(ObjectsToAdd, dataplaneSubresource)
					}
				}
				if gatewaySubResource.GetName() == "test-controlplane" {
					controlplane := gatewaySubResource.(*operatorv1alpha1.ControlPlane)
					setControlPlaneDefaults(&controlplane.Spec.ControlPlaneDeploymentOptions, "test-namespace", "svc-test-dataplane", map[string]struct{}{})
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
				Build()

			reconciler := GatewayReconciler{
				Client: fakeClient,
			}

			tc.testBody(t, reconciler, tc.gatewayReq)
		})
	}
}
