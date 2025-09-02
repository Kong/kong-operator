package controlplane

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kcfgcontrolplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/controlplane"
	kcfgdataplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	operatorv2beta1 "github.com/kong/kong-operator/apis/v2beta1"
	"github.com/kong/kong-operator/controller/pkg/op"
	"github.com/kong/kong-operator/ingress-controller/pkg/manager/multiinstance"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/internal/utils/index"
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
		controlplaneSubResources []client.Object
		dataplaneSubResources    []client.Object
		dataplanePods            []client.Object
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
				Spec: gwtypes.ControlPlaneSpec{
					DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
						Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
						Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
							Name: "test-dataplane",
						},
					},
					ControlPlaneOptions: gwtypes.ControlPlaneOptions{
						WatchNamespaces: &operatorv2beta1.WatchNamespaces{
							Type: operatorv2beta1.WatchNamespacesTypeAll,
						},
					},
				},
				Status: gwtypes.ControlPlaneStatus{
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
			dataplanePods: []client.Object{
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
			controlplaneSubResources: []client.Object{
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
			dataplaneSubResources: []client.Object{
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
		{
			name: "secret label selector conflict",
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
						string(ControlPlaneFinalizerCPInstanceTeardown),
					},
				},
				Spec: gwtypes.ControlPlaneSpec{
					DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
						Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
						Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
							Name: "test-dataplane",
						},
					},
					ControlPlaneOptions: gwtypes.ControlPlaneOptions{
						WatchNamespaces: &operatorv2beta1.WatchNamespaces{
							Type: operatorv2beta1.WatchNamespacesTypeAll,
						},
						ObjectFilters: &operatorv2beta1.ControlPlaneObjectFilters{
							Secrets: &operatorv2beta1.ControlPlaneFilterForObjectType{
								MatchLabels: map[string]string{
									"some-key":        "some-value",
									"conflicting-key": "another-value",
								},
							},
						},
					},
				},
				Status: gwtypes.ControlPlaneStatus{
					DataPlane: &operatorv2beta1.ControlPlaneDataPlaneStatus{
						Name: "test-dataplane",
					},
					Conditions: []metav1.Condition{
						{
							Type:   string(kcfgcontrolplane.ConditionTypeProvisioned),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(kcfgcontrolplane.ConditionTypeWatchNamespaceGrantValid),
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
			dataplanePods: []client.Object{
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
			controlplaneSubResources: []client.Object{
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
			dataplaneSubResources: []client.Object{
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

				reconciler.SecretLabelSelector = "conflicting-key"
				// Since the finalizer and `status.dataplane` is filled,
				// the reconcliliation loop should reach the step of validating control plane options.
				_, err := reconciler.Reconcile(ctx, controlplaneReq)
				require.NoError(t, err)
				cp := &operatorv2beta1.ControlPlane{}
				require.NoError(t, reconciler.Get(ctx, controlplaneReq.NamespacedName, cp))
				require.Truef(t, lo.ContainsBy(cp.Status.Conditions, func(c metav1.Condition) bool {
					return c.Type == string(kcfgcontrolplane.ConditionTypeOptionsValid) && c.Status == metav1.ConditionFalse
				}), "OptionsValid condition should be False")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.dataplane != nil {
				k8sutils.SetOwnerForObject(tc.dataplane, tc.controlplane)
			}
			ObjectsToAdd := []client.Object{
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
				WithStatusSubresource(tc.controlplane).
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

func TestReconciler_enforceDataPlaneNameInStatus(t *testing.T) {
	testCases := []struct {
		name              string
		controlplane      *ControlPlane
		dataplanes        *operatorv1beta1.DataPlaneList
		expectedDataPlane string
		expectedResult    op.Result
		expectError       bool
	}{
		{
			name: "ControlPlane with ref type DataPlane should set status",
			controlplane: &ControlPlane{
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
			},
			expectedDataPlane: "test-dataplane",
			expectedResult:    op.Updated,
			expectError:       false,
		},
		{
			name: "ControlPlane with same DataPlane name should not update",
			controlplane: &ControlPlane{
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
					DataPlane: &gwtypes.ControlPlaneDataPlaneStatus{
						Name: "test-dataplane",
					},
				},
			},
			expectedDataPlane: "test-dataplane",
			expectedResult:    op.Noop,
			expectError:       false,
		},
		{
			name: "ControlPlane with same owner as DataPlane and managedByOwner does not get an update if already set",
			controlplane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "gateway.konghq.com/v1",
							Kind:       "Gateway",
							Name:       "test-gw",
							UID:        types.UID("1234"),
						},
					},
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
					DataPlane: &gwtypes.ControlPlaneDataPlaneStatus{
						Name: "test-dataplane",
					},
				},
			},
			dataplanes: &operatorv1beta1.DataPlaneList{
				Items: []operatorv1beta1.DataPlane{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-dataplane",
							Namespace: "test-namespace",
							UID:       types.UID(uuid.NewString()),
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "gateway.konghq.com/v1",
									Kind:       "Gateway",
									Name:       "test-gw",
									UID:        types.UID("1234"),
								},
							},
						},
					},
				},
			},
			expectedDataPlane: "test-dataplane",
			expectedResult:    op.Noop,
			expectError:       false,
		},
		{
			name: "ControlPlane with same owner as DataPlane and managedByOwner does get an update",
			controlplane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "gateway.konghq.com/v1",
							Kind:       "Gateway",
							Name:       "test-gw",
							UID:        types.UID("1234"),
						},
					},
				},
				Spec: gwtypes.ControlPlaneSpec{
					DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
						Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
						Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
							Name: "test-dataplane",
						},
					},
				},
			},
			dataplanes: &operatorv1beta1.DataPlaneList{
				Items: []operatorv1beta1.DataPlane{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-dataplane",
							Namespace: "test-namespace",
							UID:       types.UID(uuid.NewString()),
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "gateway.konghq.com/v1",
									Kind:       "Gateway",
									Name:       "test-gw",
									UID:        types.UID("1234"),
								},
							},
						},
					},
				},
			},
			expectedDataPlane: "test-dataplane",
			expectedResult:    op.Updated,
			expectError:       false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			builder := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tc.controlplane).
				WithStatusSubresource(tc.controlplane)
			if tc.dataplanes != nil {
				builder.
					WithLists(tc.dataplanes).
					WithIndex(
						&operatorv1beta1.DataPlane{},
						index.DataPlaneOnOwnerGatewayIndex,
						index.OwnerGatewayOnDataPlane,
					)
			}

			fakeClient := builder.Build()

			r := Reconciler{
				Client: fakeClient,
			}

			dataplaneName, result, err := r.enforceDataPlaneNameInStatus(t.Context(), tc.controlplane)

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tc.expectedDataPlane, dataplaneName)
			require.Equal(t, tc.expectedResult, result)

			if result == op.Updated {
				// Verify the status was actually updated
				updatedCP := &ControlPlane{}
				require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(tc.controlplane), updatedCP))

				if tc.expectedDataPlane == "" {
					require.Nil(t, updatedCP.Status.DataPlane)
				} else {
					require.NotNil(t, updatedCP.Status.DataPlane)
					require.Equal(t, tc.expectedDataPlane, updatedCP.Status.DataPlane.Name)
				}
			}
		})
	}
}
