package envtest

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgdataplane "github.com/kong/kong-operator/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/controller/dataplane"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

func TestDataPlane(t *testing.T) {
	t.Parallel()

	t.Run("service reduction", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cl, ns := setupDataPlaneTest(t, ctx, "cluster-ca-service-reduction")

		dp := &operatorv1beta1.DataPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dp-service-reduction",
				Namespace: ns.Name,
			},
			Spec: operatorv1beta1.DataPlaneSpec{
				DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
					Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1beta1.DeploymentOptions{
							PodTemplateSpec: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:  consts.DataPlaneProxyContainerName,
										Image: consts.DefaultDataPlaneImage,
									}},
								},
							},
						},
					},
				},
			},
		}
		require.NoError(t, cl.Create(ctx, dp))

		var primaryIngressService corev1.Service
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var serviceList corev1.ServiceList
			require.NoError(ct, cl.List(ctx, &serviceList,
				client.InNamespace(ns.Name),
				client.MatchingLabels{
					"app":                             dp.Name,
					consts.DataPlaneServiceTypeLabel:  string(consts.DataPlaneIngressServiceLabelValue),
					consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValueLive,
				},
			))
			require.Len(ct, serviceList.Items, 1)
			primaryIngressService = serviceList.Items[0]
		}, waitTime, tickTime)

		dpWithUID := &operatorv1beta1.DataPlane{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(dp), dpWithUID))

		extraIngressService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "extra-ingress-service",
				Namespace: ns.Name,
				Labels: map[string]string{
					"app":                                dp.Name,
					consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
					consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValueLive,
					consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: operatorv1beta1.SchemeGroupVersion.String(),
						Kind:       "DataPlane",
						Name:       dpWithUID.Name,
						UID:        dpWithUID.UID,
						Controller: new(true),
					},
				},
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Selector: map[string]string{
					"app": dp.Name,
				},
				Ports: []corev1.ServicePort{
					{
						Name:     "proxy",
						Protocol: corev1.ProtocolTCP,
						Port:     80,
					},
				},
			},
		}
		require.NoError(t, cl.Create(ctx, extraIngressService))

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var serviceList corev1.ServiceList
			require.NoError(ct, cl.List(ctx, &serviceList,
				client.InNamespace(ns.Name),
				client.MatchingLabels{
					"app":                             dp.Name,
					consts.DataPlaneServiceTypeLabel:  string(consts.DataPlaneIngressServiceLabelValue),
					consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValueLive,
				},
			))
			require.Len(ct, serviceList.Items, 1)
			assert.Equal(ct, primaryIngressService.Name, serviceList.Items[0].Name)
		}, waitTime, tickTime)
	})

	t.Run("valid and invalid DataPlane images", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cl, ns := setupDataPlaneTest(t, ctx, "cluster-ca-image-validation")

		validDP := &operatorv1beta1.DataPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dp-valid-image",
				Namespace: ns.Name,
			},
			Spec: operatorv1beta1.DataPlaneSpec{
				DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
					Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1beta1.DeploymentOptions{
							PodTemplateSpec: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:  consts.DataPlaneProxyContainerName,
										Image: "kong:3.0",
									}},
								},
							},
						},
					},
				},
			},
		}
		require.NoError(t, cl.Create(ctx, validDP))

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var deploymentList appsv1.DeploymentList
			require.NoError(ct, cl.List(ctx, &deploymentList,
				client.InNamespace(ns.Name),
				client.MatchingLabels{"app": validDP.Name},
			))
			require.NotEmpty(ct, deploymentList.Items)
		}, waitTime, tickTime)

		invalidDP := &operatorv1beta1.DataPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dp-invalid-image",
				Namespace: ns.Name,
			},
			Spec: operatorv1beta1.DataPlaneSpec{
				DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
					Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1beta1.DeploymentOptions{
							PodTemplateSpec: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:  consts.DataPlaneProxyContainerName,
										Image: "kong:1.0",
									}},
								},
							},
						},
					},
				},
			},
		}
		require.NoError(t, cl.Create(ctx, invalidDP))

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var deploymentList appsv1.DeploymentList
			require.NoError(ct, cl.List(ctx, &deploymentList,
				client.InNamespace(ns.Name),
				client.MatchingLabels{"app": invalidDP.Name},
			))
			assert.Empty(ct, deploymentList.Items)
		}, waitTime, tickTime)
	})

	t.Run("status addresses and ready are eventually set", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		cl, ns := setupDataPlaneTest(t, ctx, "cluster-ca-status")

		dp := &operatorv1beta1.DataPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dp-status",
				Namespace: ns.Name,
			},
			Spec: operatorv1beta1.DataPlaneSpec{
				DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
					Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1beta1.DeploymentOptions{
							PodTemplateSpec: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:  consts.DataPlaneProxyContainerName,
										Image: consts.DefaultDataPlaneBaseImage + ":3.2",
										LivenessProbe: &corev1.Probe{
											InitialDelaySeconds: 1,
											PeriodSeconds:       1,
											ProbeHandler: corev1.ProbeHandler{
												HTTPGet: &corev1.HTTPGetAction{
													Path: "/healthz",
													Port: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
												},
											},
										},
									}},
								},
							},
						},
					},
				},
			},
		}
		require.NoError(t, cl.Create(ctx, dp))

		var ingressService corev1.Service
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var services corev1.ServiceList
			require.NoError(ct, cl.List(ctx, &services,
				client.InNamespace(ns.Name),
				client.MatchingLabels{
					"app":                             dp.Name,
					consts.DataPlaneServiceTypeLabel:  string(consts.DataPlaneIngressServiceLabelValue),
					consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValueLive,
				},
			))
			require.Len(ct, services.Items, 1)
			ingressService = services.Items[0]
		}, waitTime, tickTime)

		ingressService.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
			{IP: "6.7.8.9"},
			{Hostname: "mycustomhostname.com"},
		}
		require.NoError(t, cl.Status().Update(ctx, &ingressService))

		var deploymentList appsv1.DeploymentList
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			require.NoError(ct, cl.List(ctx, &deploymentList,
				client.InNamespace(ns.Name),
				client.MatchingLabels{
					"app":                                dp.Name,
					consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
					consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
				},
			))
			require.Len(ct, deploymentList.Items, 1)
		}, waitTime, tickTime)

		deployment := deploymentList.Items[0]
		deployment.Status = appsv1.DeploymentStatus{
			AvailableReplicas: 1,
			ReadyReplicas:     1,
			Replicas:          1,
		}
		require.NoError(t, cl.Status().Update(ctx, &deployment))

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			current := &operatorv1beta1.DataPlane{}
			require.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(dp), current))

			assert.Equal(ct, ingressService.Name, current.Status.Service)
			assert.Equal(ct, []operatorv1beta1.Address{
				{
					Type:       new(operatorv1beta1.IPAddressType),
					Value:      "6.7.8.9",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.HostnameAddressType),
					Value:      "mycustomhostname.com",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.IPAddressType),
					Value:      ingressService.Spec.ClusterIP,
					SourceType: operatorv1beta1.PrivateIPAddressSourceType,
				},
			}, current.Status.Addresses)

			condition, ok := k8sutils.GetCondition(kcfgdataplane.ReadyType, current)
			require.True(ct, ok)
			assert.Equal(ct, metav1.ConditionTrue, condition.Status)
			assert.EqualValues(ct, 1, current.Status.ReadyReplicas)
			assert.EqualValues(ct, 1, current.Status.Replicas)
		}, waitTime, tickTime)

		updated := &operatorv1beta1.DataPlane{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(dp), updated))
		updated.Spec.Deployment.PodTemplateSpec.Spec.Containers[0].LivenessProbe.PeriodSeconds = 2
		require.NoError(t, cl.Update(ctx, updated))

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			current := &operatorv1beta1.DataPlane{}
			require.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(dp), current))

			condition, ok := k8sutils.GetCondition(kcfgdataplane.ReadyType, current)
			require.True(ct, ok)
			assert.Equal(ct, metav1.ConditionTrue, condition.Status)
			assert.Equal(ct, current.Generation, condition.ObservedGeneration)
			assert.EqualValues(ct, 1, current.Status.ReadyReplicas)
			assert.EqualValues(ct, 1, current.Status.Replicas)
		}, waitTime, tickTime)
	})
}

func setupDataPlaneTest(t *testing.T, ctx context.Context, caSecretName string) (client.Client, *corev1.Namespace) {
	t.Helper()

	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	clusterCA := createClusterCASecret(t, ctx, mgr.GetClient(), ns.Name, caSecretName)
	dpReconciler := &dataplane.Reconciler{
		Client:                   mgr.GetClient(),
		ClusterCASecretName:      clusterCA.Name,
		ClusterCASecretNamespace: clusterCA.Namespace,
		DefaultImage:             consts.DefaultDataPlaneImage,
		ValidateDataPlaneImage:   true,
	}
	StartReconcilers(ctx, t, mgr, logs, dpReconciler)

	return mgr.GetClient(), ns
}

func createClusterCASecret(t *testing.T, ctx context.Context, cl client.Client, namespace, name string) *corev1.Secret {
	t.Helper()

	cert, key := certificate.MustGenerateCertPEMFormat(
		certificate.WithCommonName(fmt.Sprintf("%s-ca", name)),
		certificate.WithCATrue(),
	)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				"konghq.com/secret": "internal",
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": cert,
			"tls.key": key,
		},
	}
	require.NoError(t, cl.Create(ctx, secret))

	return secret
}
