package controlplane

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	controllerruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kcfgcontrolplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/controlplane"
	kcfgdataplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	operatorv2alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2alpha1"

	"github.com/kong/kong-operator/ingress-controller/pkg/manager/multiinstance"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/test/helpers"
)

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
		controlplane             *ControlPlane
		dataplane                *operatorv1beta1.DataPlane
		controlplaneSubResources []controllerruntimeclient.Object
		dataplaneSubResources    []controllerruntimeclient.Object
		dataplanePods            []controllerruntimeclient.Object
		testBody                 func(t *testing.T, reconciler Reconciler, controlplane reconcile.Request)
	}{
		{
			name: "base",
			controlplaneReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test-namespace",
					Name:      "test-controlplane",
				},
			},
			controlplane: &ControlPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "ControlPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
					Finalizers: []string{
						string(ControlPlaneFinalizerCleanupValidatingWebhookConfiguration),
					},
				},
				Spec: operatorv2alpha1.ControlPlaneSpec{
					DataPlane: operatorv2alpha1.ControlPlaneDataPlaneTarget{
						Type: operatorv2alpha1.ControlPlaneDataPlaneTargetRefType,
						Ref: &operatorv2alpha1.ControlPlaneDataPlaneTargetRef{
							Name: "test-dataplane",
						},
					},
					ControlPlaneOptions: operatorv2alpha1.ControlPlaneOptions{
						WatchNamespaces: &operatorv1beta1.WatchNamespaces{
							Type: operatorv1beta1.WatchNamespacesTypeAll,
						},
					},
				},
				Status: operatorv2alpha1.ControlPlaneStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kcfgcontrolplane.ConditionTypeProvisioned),
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
							Type:   string(kcfgdataplane.ReadyType),
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
			testBody: func(t *testing.T, reconciler Reconciler, controlplaneReq reconcile.Request) {
				ctx := t.Context()

				// first reconcile loop to allow the reconciler to set the controlplane defaults
				_, err := reconciler.Reconcile(ctx, controlplaneReq)
				require.NoError(t, err)
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
				WithScheme(scheme.Get()).
				WithObjects(ObjectsToAdd...).
				Build()

			reconciler := Reconciler{
				Client:                   fakeClient,
				ClusterCASecretName:      mtlsSecret.Name,
				ClusterCASecretNamespace: mtlsSecret.Namespace,
				InstancesManager:         multiinstance.NewManager(logr.Discard()),
			}

			tc.testBody(t, reconciler, tc.controlplaneReq)
		})
	}
}
