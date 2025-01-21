package controlplane

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	controllerruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	"github.com/kong/gateway-operator/test/helpers"
)

func init() {
	if err := gatewayv1.Install(scheme.Scheme); err != nil {
		fmt.Println("error while adding gatewayv1 scheme")
		os.Exit(1)
	}
	if err := operatorv1beta1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding operatorv1beta1 scheme")
		os.Exit(1)
	}
}

func TestReconciler_Reconcile(t *testing.T) {
	ca := helpers.CreateCA(t)
	mtlsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mtls-secret",
			Namespace: "test-namespace",
		},
		Data: map[string][]byte{
			"tls.crt": ca.CertPEM.Bytes(),
			"tls.key": ca.KeyPEM.Bytes(),
		},
	}

	testCases := []struct {
		name                     string
		controlplaneReq          reconcile.Request
		controlplane             *operatorv1beta1.ControlPlane
		dataplane                *operatorv1beta1.DataPlane
		controlplaneSubResources []controllerruntimeclient.Object
		dataplaneSubResources    []controllerruntimeclient.Object
		dataplanePods            []controllerruntimeclient.Object
		testBody                 func(t *testing.T, reconciler Reconciler, controlplane *operatorv1beta1.ControlPlane)
	}{
		{
			name: "valid ControlPlane image",
			controlplaneReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test-namespace",
					Name:      "test-controlplane",
				},
			},
			controlplane: &operatorv1beta1.ControlPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "ControlPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
					Finalizers: []string{
						string(ControlPlaneFinalizerCleanupClusterRole),
						string(ControlPlaneFinalizerCleanupClusterRoleBinding),
						string(ControlPlaneFinalizerCleanupValidatingWebhookConfiguration),
					},
				},
				Spec: operatorv1beta1.ControlPlaneSpec{
					ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
						Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
							PodTemplateSpec: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Name:  consts.ControlPlaneControllerContainerName,
											Image: "kong/kubernetes-ingress-controller:3.1.2",
										},
									},
								},
							},
						},
						DataPlane: lo.ToPtr("test-dataplane"),
					},
				},
				Status: operatorv1beta1.ControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(ConditionTypeProvisioned),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
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
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  consts.DataPlaneProxyContainerName,
												Image: "kong:3.0",
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
						{
							Type:   string(consts.ReadyType),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			dataplanePods: []controllerruntimeclient.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dataplane-pod",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app": "test-dataplane",
						},
						CreationTimestamp: metav1.Now(),
					},
					Status: corev1.PodStatus{
						PodIP: "1.2.3.4",
					},
				},
			},
			controlplaneSubResources: []controllerruntimeclient.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tls-secret",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app":                                "test-controlplane",
							consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
						},
					},
				},
				&rbacv1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-serviceAccount",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app":                                "test-controlplane",
							consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
						},
					},
				},
				&rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-clusterRole",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app":                                "test-controlplane",
							consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
						},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-clusterRoleBin",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app":                                "test-controlplane",
							consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
						},
					},
				},
			},
			dataplaneSubResources: []controllerruntimeclient.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-proxy-service",
						Namespace: "test-namespace",
						Labels: map[string]string{
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
							consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
						},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: corev1.ClusterIPNone,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-admin-service",
						Namespace: "test-namespace",
						Labels: map[string]string{
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
							consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
						},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: corev1.ClusterIPNone,
					},
				},
			},
			testBody: func(t *testing.T, reconciler Reconciler, controlplane *operatorv1beta1.ControlPlane) {
				ctx := context.Background()

				// first reconcile loop to allow the reconciler to set the controlplane defaults
				_, err := reconciler.Reconcile(ctx, controlplane)
				require.NoError(t, err)

				_, err = reconciler.Reconcile(ctx, controlplane)
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
			controlplane: &operatorv1beta1.ControlPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "ControlPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
					Finalizers: []string{
						string(ControlPlaneFinalizerCleanupClusterRole),
						string(ControlPlaneFinalizerCleanupClusterRoleBinding),
						string(ControlPlaneFinalizerCleanupValidatingWebhookConfiguration),
					},
				},
				Spec: operatorv1beta1.ControlPlaneSpec{
					ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
						Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
							PodTemplateSpec: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Name:  consts.ControlPlaneControllerContainerName,
											Image: "kong/kubernetes-ingress-controller:1.0",
										},
									},
								},
							},
						},
						DataPlane: lo.ToPtr("test-dataplane"),
					},
				},
				Status: operatorv1beta1.ControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(ConditionTypeProvisioned),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
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
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  consts.DataPlaneProxyContainerName,
												Image: "kong:1.0",
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
						{
							Type:   string(consts.ReadyType),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			dataplanePods: []controllerruntimeclient.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dataplane-pod",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app": "test-dataplane",
						},
						CreationTimestamp: metav1.Now(),
					},
					Status: corev1.PodStatus{
						PodIP: "1.2.3.4",
					},
				},
			},
			controlplaneSubResources: []controllerruntimeclient.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tls-secret",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app":                                "test-controlplane",
							consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
						},
					},
				},
				&rbacv1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-serviceAccount",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app":                                "test-controlplane",
							consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
						},
					},
				},
				&rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-clusterRole",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app":                                "test-controlplane",
							consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
						},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-clusterRoleBin",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app":                                "test-controlplane",
							consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
						},
					},
				},
			},
			dataplaneSubResources: []controllerruntimeclient.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-proxy-service",
						Namespace: "test-namespace",
						Labels: map[string]string{
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
							consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
						},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: corev1.ClusterIPNone,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-admin-service",
						Namespace: "test-namespace",
						Labels: map[string]string{
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
							consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
						},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: corev1.ClusterIPNone,
					},
				},
			},
			testBody: func(t *testing.T, reconciler Reconciler, controlplane *operatorv1beta1.ControlPlane) {
				ctx := context.Background()

				_, err := reconciler.Reconcile(ctx, controlplane)
				require.EqualError(t, err, "unsupported ControlPlane image kong/kubernetes-ingress-controller:1.0")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.dataplane != nil {
				k8sutils.SetOwnerForObject(tc.dataplane, tc.controlplane)
			}
			ObjectsToAdd := []controllerruntimeclient.Object{
				tc.controlplane,
				tc.dataplane,
				mtlsSecret,
			}

			ObjectsToAdd = append(ObjectsToAdd, tc.dataplanePods...)

			for _, controlplaneSubresource := range tc.controlplaneSubResources {
				k8sutils.SetOwnerForObject(controlplaneSubresource, tc.controlplane)
				ObjectsToAdd = append(ObjectsToAdd, controlplaneSubresource)
			}

			for _, dataplaneSubresource := range tc.dataplaneSubResources {
				k8sutils.SetOwnerForObject(dataplaneSubresource, tc.dataplane)
				ObjectsToAdd = append(ObjectsToAdd, dataplaneSubresource)
			}

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(ObjectsToAdd...).
				Build()

			reconciler := Reconciler{
				Client:                   fakeClient,
				Scheme:                   scheme.Scheme,
				ClusterCASecretName:      mtlsSecret.Name,
				ClusterCASecretNamespace: mtlsSecret.Namespace,
			}

			tc.testBody(t, reconciler, tc.controlplane)
		})
	}
}
