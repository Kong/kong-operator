package controllers

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/test/helpers"
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

func TestDataPlaneReconciler_Reconcile(t *testing.T) {
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
					Kind:       "DataPlane",
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
						Name:      "svc-proxy-to-keep",
						Namespace: "test-namespace",
						Labels: map[string]string{
							consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneProxyServiceLabelValue),
						},
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
						Name:      "test-admin-service",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app":                            "test-dataplane",
							consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneAdminServiceLabelValue),
						},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: corev1.ClusterIPNone,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-proxy-to-delete",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app":                            "test-dataplane",
							consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneProxyServiceLabelValue),
						},
					},
				},
			},
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataplaneReq reconcile.Request) {
				ctx := context.Background()

				// first reconcile loop to allow the reconciler to set the dataplane defaults
				_, err := reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.EqualError(t, err, "number of dataplane proxy services reduced")

				svcToBeDeleted, svcToBeKept := &corev1.Service{}, &corev1.Service{}
				err = reconciler.Client.Get(ctx, types.NamespacedName{Namespace: "test-namespace", Name: "svc-proxy-to-delete"}, svcToBeDeleted)
				require.True(t, k8serrors.IsNotFound(err))
				err = reconciler.Client.Get(ctx, types.NamespacedName{Namespace: "test-namespace", Name: "svc-proxy-to-keep"}, svcToBeKept)
				require.NoError(t, err)
			},
		},
		{
			name: "valid DataPlane image",
			dataplaneReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test-namespace",
					Name:      "test-dataplane",
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
							ContainerImage: pointer.String("kong"),
							Version:        pointer.String("3.0"),
						},
					},
				},
				Status: operatorv1alpha1.DataPlaneStatus{
					Service: "svc-proxy-to-delete",
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
						Name:      "test-admin-service",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app":                            "test-dataplane",
							consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneAdminServiceLabelValue),
						},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: corev1.ClusterIPNone,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-proxy-to-delete",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app":                            "test-dataplane",
							consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneProxyServiceLabelValue),
						},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tls-secret",
						Namespace: "test-namespace",
					},
				},
			},
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataplaneReq reconcile.Request) {
				ctx := context.Background()

				// first reconcile loop to allow the reconciler to set the dataplane defaults
				_, err := reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
			},
		},
		{
			name: "invalid DataPlane image",
			dataplaneReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dataplane",
					Namespace: "default",
				},
			},
			dataplane: &operatorv1alpha1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "default",
					UID:       types.UID(uuid.NewString()),
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneDeploymentOptions: operatorv1alpha1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1alpha1.DeploymentOptions{
							ContainerImage: pointer.String("kong"),
							Version:        pointer.String("1.0"),
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
			dataplaneSubResources: []controllerruntimeclient.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-admin-service",
						Namespace: "default",
						Labels: map[string]string{
							"app":                            "test-dataplane",
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
						Namespace: "default",
						Labels: map[string]string{
							"app":                            "test-dataplane",
							consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneProxyServiceLabelValue),
						},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP:  "1.1.1.1",
						ClusterIPs: []string{"1.1.1.1"},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dataplane-tls-secret",
						Namespace: "default",
						Labels: map[string]string{
							"konghq.com/gateway-operator": "dataplane",
						},
					},
					Data: helpers.TLSSecretData(t, ca,
						helpers.CreateCert(t, "*.test-admin-service.default.svc", ca.Cert, ca.Key),
					),
				},
			},
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataplaneReq reconcile.Request) {
				ctx := context.Background()

				// first reconcile loop to allow the reconciler to set the dataplane defaults
				_, err := reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				// second reconcile loop to allow the reconciler to set the service name in the dataplane status
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.EqualError(t, err, "unsupported DataPlane image kong:1.0")
			},
		},
		{
			name: "dataplane status is populated with backing service and its addresses",
			dataplaneReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "dataplane-kong",
					Namespace: "default",
				},
			},
			dataplane: &operatorv1alpha1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dataplane-kong",
					Namespace: "default",
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
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dataplane-deployment",
						Namespace: "default",
					},
					Status: appsv1.DeploymentStatus{
						ReadyReplicas: 1,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-admin-service",
						Namespace: "default",
						Labels: map[string]string{
							"app":                            "test-dataplane",
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
						Namespace: "default",
						Labels: map[string]string{
							"app":                            "test-dataplane",
							consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneProxyServiceLabelValue),
						},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP:  "10.0.0.1",
						ClusterIPs: []string{"10.0.0.1"},
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "6.7.8.9",
								},
								{
									Hostname: "mycustomhostname.com",
								},
							},
						},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dataplane-tls-secret-2",
						Namespace: "default",
					},
					Data: helpers.TLSSecretData(t, ca,
						helpers.CreateCert(t, "*.test-admin-service.default.svc", ca.Cert, ca.Key),
					),
				},
			},
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataplaneReq reconcile.Request) {
				ctx := context.Background()

				_, err := reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				// The second reconcile is needed because the first one would only get to marking
				// the DataPlane as Scheduled.
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				// The third reconcile is needed because the second one will only ensure
				// the service is deployed for the DataPlane.
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				// The fourth reconcile is needed to ensure the service name in the dataplane status
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				dp := &operatorv1alpha1.DataPlane{}
				err = reconciler.Client.Get(ctx, types.NamespacedName{Namespace: "default", Name: "dataplane-kong"}, dp)
				require.NoError(t, err)
				require.Equal(t, "test-proxy-service", dp.Status.Service)
				require.Equal(t, []operatorv1alpha1.Address{
					// This currently assumes that we sort the addresses in a way
					// such that LoadBalancer IPs, then LoadBalancer hostnames are added
					// and then ClusterIPs follow.
					// If this ends up being the desired logic and aligns with what
					// has been agreed in https://github.com/Kong/gateway-operator/issues/281
					// then no action has to be taken. Otherwise this might need to be changed.
					{
						Type:  addressOf(operatorv1alpha1.IPAddressType),
						Value: "6.7.8.9",
					},
					{
						Type:  addressOf(operatorv1alpha1.HostnameAddressType),
						Value: "mycustomhostname.com",
					},
					{
						Type:  addressOf(operatorv1alpha1.IPAddressType),
						Value: "10.0.0.1",
					},
				}, dp.Status.Addresses)
			},
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			ObjectsToAdd := []controllerruntimeclient.Object{
				tc.dataplane,
				mtlsSecret,
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
				Client:                   fakeClient,
				ClusterCASecretName:      mtlsSecret.Name,
				ClusterCASecretNamespace: mtlsSecret.Namespace,
			}

			tc.testBody(t, reconciler, tc.dataplaneReq)
		})
	}
}
