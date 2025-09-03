package dataplane

import (
	"crypto/x509"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	controllerruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kcfgdataplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/dataplane"

	operatorv1alpha1 "github.com/kong/kong-operator/apis/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/apis/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/controller/pkg/secrets"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/test/helpers"
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

// TODO: This test requires a rewrite to get rid of the mystical .Reconcile()
// calls which tests writers each time have to guess how many of those will be
// necessary.
// There's an open issue to rewrite that into e.g. envtest based test(s) so that
// test writers will be able to rely on the reconciler running against an apiserver
// and just asserting on the actual desired effect.
//
// Ref: https://github.com/kong/kong-operator/issues/172
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
		dataplane             *operatorv1beta1.DataPlane
		dataplaneSubResources []controllerruntimeclient.Object
		testBody              func(t *testing.T, reconciler Reconciler, dataplaneReq reconcile.Request)
	}{
		{
			name: "service reduction",
			dataplaneReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test-namespace",
					Name:      "test-dataplane",
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
												Image: consts.DefaultDataPlaneImage,
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
			dataplaneSubResources: []controllerruntimeclient.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-proxy-to-keep",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app":                                "test-dataplane",
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
							consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValueLive,
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
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
							"app":                                "test-dataplane",
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
							consts.DataPlaneServiceStateLabel:    string(consts.DataPlaneStateLabelValueLive),
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
						},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: corev1.ClusterIPNone,
						Type:      corev1.ServiceTypeClusterIP,
						Selector:  map[string]string{"app": "test-dataplane"},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-proxy-to-delete",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app":                                "test-dataplane",
							consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValueLive,
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
						},
					},
				},
			},
			testBody: func(t *testing.T, reconciler Reconciler, dataplaneReq reconcile.Request) {
				ctx := t.Context()

				// first reconcile loop to allow the reconciler to set the dataplane defaults
				_, err := reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.EqualError(t, err, "number of DataPlane ingress services reduced")

				svcToBeDeleted, svcToBeKept := &corev1.Service{}, &corev1.Service{}
				err = reconciler.Get(ctx, types.NamespacedName{Namespace: "test-namespace", Name: "svc-proxy-to-delete"}, svcToBeDeleted)
				require.True(t, k8serrors.IsNotFound(err))
				err = reconciler.Get(ctx, types.NamespacedName{Namespace: "test-namespace", Name: "svc-proxy-to-keep"}, svcToBeKept)
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
					Service: "svc-proxy-to-delete",
					Conditions: []metav1.Condition{
						{
							Type:   string(kcfgdataplane.ReadyType),
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
						Type:      corev1.ServiceTypeClusterIP,
						Selector:  map[string]string{"app": "test-dataplane"},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-proxy-to-delete",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app":                             "test-dataplane",
							consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValueLive,
							consts.DataPlaneServiceTypeLabel:  string(consts.DataPlaneIngressServiceLabelValue),
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
			testBody: func(t *testing.T, reconciler Reconciler, dataplaneReq reconcile.Request) {
				ctx := t.Context()

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
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "default",
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
							Type:   string(kcfgdataplane.ReadyType),
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
							"app":                                "test-dataplane",
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
							consts.DataPlaneServiceStateLabel:    string(consts.DataPlaneStateLabelValueLive),
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
						},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: corev1.ClusterIPNone,
						Type:      corev1.ServiceTypeClusterIP,
						Selector:  map[string]string{"app": "test-dataplane"},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-proxy-service",
						Namespace: "default",
						Labels: map[string]string{
							"app":                                "test-dataplane",
							consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValueLive,
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
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
							consts.GatewayOperatorManagedByLabel: "dataplane",
						},
					},
					Data: helpers.TLSSecretData(t, ca,
						helpers.CreateCert(t, "*.test-admin-service.default.svc", ca.Cert, ca.Key),
					),
				},
			},
			testBody: func(t *testing.T, reconciler Reconciler, dataplaneReq reconcile.Request) {
				ctx := t.Context()

				// first reconcile loop to allow the reconciler to set the dataplane defaults
				_, err := reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				// second reconcile loop to allow the reconciler to set the service name in the dataplane status
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.EqualError(t, err, "could not build Deployment for DataPlane default/test-dataplane: could not generate Deployment: unsupported DataPlane image kong:1.0")
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
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dataplane-kong",
					Namespace: "default",
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
												Image: consts.DefaultDataPlaneImage,
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
							"app":                                "test-dataplane",
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
							consts.DataPlaneServiceStateLabel:    string(consts.DataPlaneStateLabelValueLive),
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
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
							"app":                                "test-dataplane",
							consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValueLive,
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
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
			testBody: func(t *testing.T, reconciler Reconciler, dataplaneReq reconcile.Request) {
				ctx := t.Context()

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
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				dp := &operatorv1beta1.DataPlane{}
				err = reconciler.Get(ctx, types.NamespacedName{Namespace: "default", Name: "dataplane-kong"}, dp)
				require.NoError(t, err)
				require.Equal(t, "test-proxy-service", dp.Status.Service)
				require.Equal(t, []operatorv1beta1.Address{
					// This currently assumes that we sort the addresses in a way
					// such that LoadBalancer IPs, then LoadBalancer hostnames are added
					// and then ClusterIPs follow.
					// If this ends up being the desired logic and aligns with what
					// has been agreed in https://github.com/kong/kong-operator/issues/281
					// then no action has to be taken. Otherwise this might need to be changed.
					{
						Type:       lo.ToPtr(operatorv1beta1.IPAddressType),
						Value:      "6.7.8.9",
						SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
					},
					{
						Type:       lo.ToPtr(operatorv1beta1.HostnameAddressType),
						Value:      "mycustomhostname.com",
						SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
					},
					{
						Type:       lo.ToPtr(operatorv1beta1.IPAddressType),
						Value:      "10.0.0.1",
						SourceType: operatorv1beta1.PrivateIPAddressSourceType,
					},
				}, dp.Status.Addresses)
			},
		},
		{
			name: "dataplane status has its ready field set",
			dataplaneReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "dataplane-kong",
					Namespace: "default",
				},
			},
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dataplane-kong",
					Namespace: "default",
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
												Image: consts.DefaultDataPlaneBaseImage + ":3.2",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			dataplaneSubResources: []controllerruntimeclient.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dataplane-deployment",
						Namespace: "default",
						Labels: map[string]string{
							consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
						ReadyReplicas:     1,
						Replicas:          1,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-admin-service",
						Namespace: "default",
						Labels: map[string]string{
							"app":                                "test-dataplane",
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
							consts.DataPlaneServiceStateLabel:    string(consts.DataPlaneStateLabelValueLive),
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
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
							"app":                                "test-dataplane",
							consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValueLive,
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
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
			testBody: func(t *testing.T, reconciler Reconciler, dataplaneReq reconcile.Request) {
				ctx := t.Context()

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

				dp := &operatorv1beta1.DataPlane{}
				nn := types.NamespacedName{Namespace: "default", Name: "dataplane-kong"}
				err = reconciler.Get(ctx, nn, dp)
				require.NoError(t, err)
				c, ok := k8sutils.GetCondition(kcfgdataplane.ReadyType, dp)
				require.True(t, ok, "DataPlane should have a Ready condition set")
				assert.Equal(t, metav1.ConditionFalse, c.Status, "DataPlane shouldn't be ready just yet")
				assert.EqualValues(t, 0, dp.Status.ReadyReplicas)
				assert.EqualValues(t, 0, dp.Status.Replicas)

				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				err = reconciler.Get(ctx, nn, dp)
				require.NoError(t, err)
				c, ok = k8sutils.GetCondition(kcfgdataplane.ReadyType, dp)
				require.True(t, ok, "DataPlane should have a Ready condition set")
				assert.Equal(t, metav1.ConditionTrue, c.Status, "DataPlane should be ready at this point")
				assert.EqualValues(t, 1, dp.Status.ReadyReplicas)
				assert.EqualValues(t, 1, dp.Status.Replicas)
			},
		},
		{
			name: "dataplane gets updated with status conditions when it's updated with a field that has non zero defaults",
			dataplaneReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "dataplane-kong",
					Namespace: "default",
				},
			},
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dataplane-kong",
					Namespace: "default",
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
												Image: consts.DefaultDataPlaneBaseImage + ":3.2",
												LivenessProbe: &corev1.Probe{
													InitialDelaySeconds: 1,
													PeriodSeconds:       1,
													ProbeHandler: corev1.ProbeHandler{
														HTTPGet: &corev1.HTTPGetAction{
															Path: "/healthz",
															Port: intstr.IntOrString{
																Type:   intstr.Int,
																IntVal: 8080,
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			dataplaneSubResources: []controllerruntimeclient.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dataplane-deployment",
						Namespace: "default",
						Labels: map[string]string{
							consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
						ReadyReplicas:     1,
						Replicas:          1,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-admin-service",
						Namespace: "default",
						Labels: map[string]string{
							"app":                                "test-dataplane",
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
							consts.DataPlaneServiceStateLabel:    string(consts.DataPlaneStateLabelValueLive),
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
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
							"app":                                "test-dataplane",
							consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValueLive,
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
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
			testBody: func(t *testing.T, reconciler Reconciler, dataplaneReq reconcile.Request) {
				ctx := t.Context()

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

				dp := &operatorv1beta1.DataPlane{}
				nn := types.NamespacedName{Namespace: "default", Name: "dataplane-kong"}
				err = reconciler.Get(ctx, nn, dp)
				require.NoError(t, err)
				c, ok := k8sutils.GetCondition(kcfgdataplane.ReadyType, dp)
				require.True(t, ok, "DataPlane should have a Ready condition set")
				assert.Equal(t, metav1.ConditionFalse, c.Status, "DataPlane shouldn't be ready just yet")
				assert.EqualValues(t, 0, dp.Status.ReadyReplicas)
				assert.EqualValues(t, 0, dp.Status.Replicas)

				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				err = reconciler.Get(ctx, nn, dp)
				require.NoError(t, err)
				c, ok = k8sutils.GetCondition(kcfgdataplane.ReadyType, dp)
				require.True(t, ok, "DataPlane should have a Ready condition set")
				assert.Equal(t, metav1.ConditionTrue, c.Status, "DataPlane should be ready at this point")
				assert.Equal(t, c.ObservedGeneration, dp.Generation, "DataPlane Ready condition should have the same generation as the DataPlane")
				assert.EqualValues(t, 1, dp.Status.ReadyReplicas)
				assert.EqualValues(t, 1, dp.Status.Replicas)

				dp.Spec.Deployment.PodTemplateSpec.Spec.Containers[0].LivenessProbe.PeriodSeconds = 2
				require.NoError(t, reconciler.Update(ctx, dp))

				// Below code checks if the dataplane gets properly updated status conditions when
				// the dataplane spec changes with a field that has non zero defaults.
				// See: https://github.com/kong/kong-operator/issues/904
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				require.NoError(t, reconciler.Get(ctx, nn, dp))
				c, ok = k8sutils.GetCondition(kcfgdataplane.ReadyType, dp)
				require.True(t, ok, "DataPlane should have a Ready condition set")
				assert.Equal(t, metav1.ConditionTrue, c.Status, "DataPlane should be ready at this point")
				assert.Equal(t, c.ObservedGeneration, dp.Generation, "DataPlane Ready condition should have the same generation as the DataPlane")
				assert.EqualValues(t, 1, dp.Status.ReadyReplicas)
				assert.EqualValues(t, 1, dp.Status.Replicas)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ObjectsToAdd := []controllerruntimeclient.Object{
				tc.dataplane,
				mtlsSecret,
			}

			for _, dataplaneSubresource := range tc.dataplaneSubResources {
				k8sutils.SetOwnerForObject(dataplaneSubresource, tc.dataplane)
				ObjectsToAdd = append(ObjectsToAdd, dataplaneSubresource)
			}

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(ObjectsToAdd...).
				WithStatusSubresource(tc.dataplane).
				Build()

			reconciler := Reconciler{
				Client:                   fakeClient,
				ClusterCASecretName:      mtlsSecret.Name,
				ClusterCASecretNamespace: mtlsSecret.Namespace,
				ValidateDataPlaneImage:   true,
				ClusterCAKeyConfig: secrets.KeyConfig{
					Type: x509.ECDSA,
				},
			}

			tc.testBody(t, reconciler, tc.dataplaneReq)
		})
	}
}
