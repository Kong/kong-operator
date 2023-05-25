package controllers

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	controllerruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
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

func TestGatewayClassReconciler_Reconcile(t *testing.T) {
	testCases := []struct {
		name            string
		gatewayClassReq reconcile.Request
		gatewayClass    *gatewayv1beta1.GatewayClass
		testBody        func(t *testing.T, reconciler GatewayClassReconciler, gatewayClassReq reconcile.Request, gatewayClass *gatewayv1beta1.GatewayClass)
	}{
		{
			name: "gatewayclass not accepted",
			gatewayClassReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "test-gatewayclass",
				},
			},
			gatewayClass: &gatewayv1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gatewayclass",
				},
				Spec: gatewayv1beta1.GatewayClassSpec{
					ControllerName: gatewayv1beta1.GatewayController("mismatch-controller-name"),
				},
			},
			testBody: func(t *testing.T, reconciler GatewayClassReconciler, gatewayClassReq reconcile.Request, gatewayClass *gatewayv1beta1.GatewayClass) {
				ctx := context.Background()
				_, err := reconciler.Reconcile(ctx, gatewayClassReq)
				require.NoError(t, err)
				gwc := newGatewayClass()
				err = reconciler.Client.Get(ctx, gatewayClassReq.NamespacedName, gwc.GatewayClass)
				require.NoError(t, err)
				require.False(t, gwc.isAccepted())
			},
		},
		{
			name: "gatewayclass accepted",
			gatewayClassReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "test-gatewayclass",
				},
			},
			gatewayClass: &gatewayv1beta1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gatewayclass",
				},
				Spec: gatewayv1beta1.GatewayClassSpec{
					ControllerName: gatewayv1beta1.GatewayController(vars.ControllerName()),
				},
			},
			testBody: func(t *testing.T, reconciler GatewayClassReconciler, gatewayClassReq reconcile.Request, gatewayClass *gatewayv1beta1.GatewayClass) {
				ctx := context.Background()
				_, err := reconciler.Reconcile(ctx, gatewayClassReq)
				require.NoError(t, err)
				gwc := newGatewayClass()
				err = reconciler.Client.Get(ctx, gatewayClassReq.NamespacedName, gwc.GatewayClass)
				require.NoError(t, err)
				require.True(t, gwc.isAccepted())
			},
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			ObjectsToAdd := []controllerruntimeclient.Object{
				tc.gatewayClass,
			}

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(ObjectsToAdd...).
				WithStatusSubresource(tc.gatewayClass).
				Build()

			reconciler := GatewayClassReconciler{
				Client: fakeClient,
			}

			tc.testBody(t, reconciler, tc.gatewayClassReq, tc.gatewayClass)
		})
	}
}
