package gatewayclass

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
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/internal/utils/gatewayclass"
	"github.com/kong/gateway-operator/pkg/vars"
)

func init() {
	if err := gatewayv1.Install(scheme.Scheme); err != nil {
		fmt.Println("error while adding gatewayv1 scheme")
		os.Exit(1)
	}
	if err := operatorv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding operatorv1alpha1 scheme")
		os.Exit(1)
	}
	if err := operatorv1beta1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding operatorv1beta1 scheme")
		os.Exit(1)
	}
}

func TestGatewayClassReconciler_Reconcile(t *testing.T) {
	testCases := []struct {
		name            string
		gatewayClassReq reconcile.Request
		gatewayClass    *gatewayv1.GatewayClass
		testBody        func(t *testing.T, reconciler Reconciler, gatewayClassReq reconcile.Request, gatewayClass *gatewayv1.GatewayClass)
	}{
		{
			name: "gatewayclass not accepted",
			gatewayClassReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "test-gatewayclass",
				},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gatewayclass",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: gatewayv1.GatewayController("mismatch-controller-name"),
				},
			},
			testBody: func(t *testing.T, reconciler Reconciler, gatewayClassReq reconcile.Request, gatewayClass *gatewayv1.GatewayClass) {
				ctx := context.Background()
				_, err := reconciler.Reconcile(ctx, gatewayClass)
				require.NoError(t, err)
				gwc := gatewayclass.NewDecorator()
				err = reconciler.Client.Get(ctx, gatewayClassReq.NamespacedName, gwc.GatewayClass)
				require.NoError(t, err)
				require.False(t, gwc.IsAccepted())
			},
		},
		{
			name: "gatewayclass accepted",
			gatewayClassReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "test-gatewayclass",
				},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gatewayclass",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
				},
			},
			testBody: func(t *testing.T, reconciler Reconciler, gatewayClassReq reconcile.Request, gatewayClass *gatewayv1.GatewayClass) {
				ctx := context.Background()
				_, err := reconciler.Reconcile(ctx, gatewayClass)
				require.NoError(t, err)
				gwc := gatewayclass.NewDecorator()
				err = reconciler.Client.Get(ctx, gatewayClassReq.NamespacedName, gwc.GatewayClass)
				require.NoError(t, err)
				require.True(t, gwc.IsAccepted())
			},
		},
	}

	for _, tc := range testCases {
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

			reconciler := Reconciler{
				Client: fakeClient,
			}

			tc.testBody(t, reconciler, tc.gatewayClassReq, tc.gatewayClass)
		})
	}
}
