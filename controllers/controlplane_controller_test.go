package controllers

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
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

func TestControlPlaneReconciler_Reconcile(t *testing.T) {
	testCases := []struct {
		name                     string
		controlplaneReq          reconcile.Request
		controlplane             *operatorv1alpha1.ControlPlane
		dataplane                *operatorv1alpha1.DataPlane
		controlplaneSubResources []controllerruntimeclient.Object
		dataplaneSubResources    []controllerruntimeclient.Object
		testBody                 func(t *testing.T, reconciler ControlPlaneReconciler, controlplane reconcile.Request)
	}{
		{
			name: "valid ControlPlane image",
			controlplaneReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test-namespace",
					Name:      "test-controlplane",
				},
			},
			controlplane: &operatorv1alpha1.ControlPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "ControlPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
					Finalizers: []string{
						string(ControlPlaneFinalizerCleanupClusterRole),
						string(ControlPlaneFinalizerCleanupClusterRoleBinding),
					},
				},
				Spec: operatorv1alpha1.ControlPlaneSpec{
					ControlPlaneDeploymentOptions: operatorv1alpha1.ControlPlaneDeploymentOptions{
						DeploymentOptions: operatorv1alpha1.DeploymentOptions{
							ContainerImage: pointer.StringPtr("kong/kubernetes-ingress-controller"),
							Version:        pointer.StringPtr("2.7"),
						},
						DataPlane: pointer.StringPtr("test-dataplane"),
					},
				},
				Status: operatorv1alpha1.ControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(ControlPlaneConditionTypeProvisioned),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			dataplane: &operatorv1alpha1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneDeploymentOptions: operatorv1alpha1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1alpha1.DeploymentOptions{
							ContainerImage: pointer.StringPtr("kong"),
							Version:        pointer.StringPtr("3.0"),
						},
					},
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
			controlplaneSubResources: []controllerruntimeclient.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tls-secret",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app": "test-controlplane",
						},
					},
				},
				&rbacv1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-serviceAccount",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app": "test-controlplane",
						},
					},
				},
				&rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-clusterRole",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app": "test-controlplane",
						},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-clusterRoleBin",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app": "test-controlplane",
						},
					},
				},
			},
			dataplaneSubResources: []controllerruntimeclient.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service",
						Namespace: "test-namespace",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "1.1.1.1",
					},
				},
			},
			testBody: func(t *testing.T, reconciler ControlPlaneReconciler, controlplaneReq reconcile.Request) {
				ctx := context.Background()

				// first reconcile loop to allow the reconciler to set the controlplane defaults
				_, err := reconciler.Reconcile(ctx, controlplaneReq)
				require.NoError(t, err)

				_, err = reconciler.Reconcile(ctx, controlplaneReq)
				require.NoError(t, err)
			},
		},
		{
			name: "invalid ControlPlane image",
			controlplaneReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test-namespace",
					Name:      "test-controlplane",
				},
			},
			controlplane: &operatorv1alpha1.ControlPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "ControlPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
					Finalizers: []string{
						string(ControlPlaneFinalizerCleanupClusterRole),
						string(ControlPlaneFinalizerCleanupClusterRoleBinding),
					},
				},
				Spec: operatorv1alpha1.ControlPlaneSpec{
					ControlPlaneDeploymentOptions: operatorv1alpha1.ControlPlaneDeploymentOptions{
						DeploymentOptions: operatorv1alpha1.DeploymentOptions{
							ContainerImage: pointer.StringPtr("kong/kubernetes-ingress-controller"),
							Version:        pointer.StringPtr("1.0"),
						},
						DataPlane: pointer.StringPtr("test-dataplane"),
					},
				},
				Status: operatorv1alpha1.ControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(ControlPlaneConditionTypeProvisioned),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			dataplane: &operatorv1alpha1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneDeploymentOptions: operatorv1alpha1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1alpha1.DeploymentOptions{
							ContainerImage: pointer.StringPtr("kong"),
							Version:        pointer.StringPtr("3.0"),
						},
					},
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
			controlplaneSubResources: []controllerruntimeclient.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tls-secret",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app": "test-controlplane",
						},
					},
				},
				&rbacv1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-serviceAccount",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app": "test-controlplane",
						},
					},
				},
				&rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-clusterRole",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app": "test-controlplane",
						},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-clusterRoleBin",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app": "test-controlplane",
						},
					},
				},
			},
			dataplaneSubResources: []controllerruntimeclient.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service",
						Namespace: "test-namespace",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "1.1.1.1",
					},
				},
			},
			testBody: func(t *testing.T, reconciler ControlPlaneReconciler, controlplaneReq reconcile.Request) {
				ctx := context.Background()

				_, err := reconciler.Reconcile(ctx, controlplaneReq)
				require.EqualError(t, err, "unsupported ControlPlane image kong/kubernetes-ingress-controller:1.0")
			},
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			if tc.dataplane != nil {
				k8sutils.SetOwnerForObject(tc.dataplane, tc.controlplane)
			}
			ObjectsToAdd := []controllerruntimeclient.Object{
				tc.controlplane,
				tc.dataplane,
			}

			for _, controlplaneSubresource := range tc.controlplaneSubResources {
				k8sutils.SetOwnerForObject(controlplaneSubresource, tc.controlplane)
				addLabelForControlPlane(controlplaneSubresource)
				ObjectsToAdd = append(ObjectsToAdd, controlplaneSubresource)
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

			reconciler := ControlPlaneReconciler{
				Client: fakeClient,
				Scheme: scheme.Scheme,
			}

			tc.testBody(t, reconciler, tc.controlplaneReq)
		})
	}
}
