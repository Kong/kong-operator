package controllers

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	controllerruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
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

func TestDataplaneReconciler_Reconcile(t *testing.T) {
	testCases := []struct {
		name                  string
		dataplaneReq          reconcile.Request
		dataplane             *operatorv1alpha1.DataPlane
		dataplaneSubResources []controllerruntimeclient.Object
		testBody              func(t *testing.T, reconciler DataPlaneReconciler, dataplaneReq reconcile.Request)
	}{
		{
			name: "service reduction",
			dataplaneReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test-namespace",
					Name:      "test-dataplane",
				},
			},
			dataplane: &operatorv1alpha1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "Dataplane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Status: operatorv1alpha1.DataPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(DataPlaneConditionTypeProvisioned),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			dataplaneSubResources: []controllerruntimeclient.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-to-keep",
						Namespace: "test-namespace",
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "1.2.3.4",
								},
							},
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-to-delete",
						Namespace: "test-namespace",
					},
				},
			},
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataplaneReq reconcile.Request) {
				ctx := context.Background()

				_, err := reconciler.Reconcile(ctx, dataplaneReq)
				require.EqualError(t, err, "number of services reduced")

				svcToBeDeleted, svcToBeKept := &corev1.Service{}, &corev1.Service{}
				err = reconciler.Client.Get(ctx, types.NamespacedName{Namespace: "test-namespace", Name: "svc-to-delete"}, svcToBeDeleted)
				require.True(t, k8serrors.IsNotFound(err))
				err = reconciler.Client.Get(ctx, types.NamespacedName{Namespace: "test-namespace", Name: "svc-to-keep"}, svcToBeKept)
				require.NoError(t, err)
			},
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			ObjectsToAdd := []controllerruntimeclient.Object{
				tc.dataplane,
			}

			for _, dataplaneSubresource := range tc.dataplaneSubResources {
				k8sutils.SetOwnerForObject(dataplaneSubresource, tc.dataplane)
				addLabelForDataplane(dataplaneSubresource)
				ObjectsToAdd = append(ObjectsToAdd, dataplaneSubresource)
			}

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(ObjectsToAdd...).
				Build()

			reconciler := DataPlaneReconciler{
				Client: fakeClient,
			}

			tc.testBody(t, reconciler, tc.dataplaneReq)
		})
	}
}
