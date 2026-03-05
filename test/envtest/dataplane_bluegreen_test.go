package envtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgdataplane "github.com/kong/kong-operator/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/controller/dataplane"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
)

func TestDataPlaneBlueGreen(t *testing.T) {
	ctx := t.Context()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	clusterCA := createClusterCASecret(t, ctx, mgr.GetClient(), ns.Name, "cluster-ca-bluegreen-reconcile")
	bgReconciler := &dataplane.BlueGreenReconciler{
		Client:                   mgr.GetClient(),
		ClusterCASecretName:      clusterCA.Name,
		ClusterCASecretNamespace: clusterCA.Namespace,
		DefaultImage:             consts.DefaultDataPlaneImage,
		ValidateDataPlaneImage:   true,
		DataPlaneController: &dataplane.Reconciler{
			Client:                   mgr.GetClient(),
			ClusterCASecretName:      clusterCA.Name,
			ClusterCASecretNamespace: clusterCA.Namespace,
			DefaultImage:             consts.DefaultDataPlaneImage,
			ValidateDataPlaneImage:   true,
		},
	}
	StartReconcilers(ctx, t, mgr, logs, bgReconciler)

	dp := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dp-bluegreen-reconcile",
			Namespace: ns.Name,
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					Rollout: &operatorv1beta1.Rollout{
						Strategy: operatorv1beta1.RolloutStrategy{
							BlueGreen: &operatorv1beta1.BlueGreenStrategy{
								Promotion: operatorv1beta1.Promotion{
									Strategy: operatorv1beta1.BreakBeforePromotion,
								},
							},
						},
					},
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:  consts.DataPlaneProxyContainerName,
									Image: consts.DefaultDataPlaneBaseImage + ":3.2",
								}},
							},
						},
					},
				},
			},
		},
	}
	require.NoError(t, mgr.GetClient().Create(ctx, dp))

	var ingressService corev1.Service
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var services corev1.ServiceList
		require.NoError(ct, mgr.GetClient().List(ctx, &services,
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

	ingressService.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "6.7.8.9"}}
	require.NoError(t, mgr.GetClient().Status().Update(ctx, &ingressService))

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var deploymentList appsv1.DeploymentList
		require.NoError(ct, mgr.GetClient().List(ctx, &deploymentList,
			client.InNamespace(ns.Name),
			client.MatchingLabels{
				"app":                                dp.Name,
				consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
				consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
			},
		))
		require.Len(ct, deploymentList.Items, 1)

		liveDeployment := deploymentList.Items[0]
		liveDeployment.Status = appsv1.DeploymentStatus{
			AvailableReplicas: 1,
			ReadyReplicas:     1,
			Replicas:          1,
		}
		require.NoError(ct, mgr.GetClient().Status().Update(ctx, &liveDeployment))
	}, waitTime, tickTime)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		current := &operatorv1beta1.DataPlane{}
		require.NoError(ct, mgr.GetClient().Get(ctx, client.ObjectKeyFromObject(dp), current))

		condition, ok := k8sutils.GetCondition(kcfgdataplane.DataPlaneConditionTypeRolledOut, current.Status.RolloutStatus)
		require.True(ct, ok)
		assert.Equal(ct, metav1.ConditionFalse, condition.Status)
	}, waitTime, tickTime)

	dataplaneName := client.ObjectKeyFromObject(dp)
	require.Eventually(t,
		testutils.DataPlaneUpdateEventually(t, ctx, dataplaneName, mgr.GetClient(), func(dp *operatorv1beta1.DataPlane) {
			dp.Spec.Deployment.PodTemplateSpec.Spec.Containers = append(
				dp.Spec.Deployment.PodTemplateSpec.Spec.Containers,
				corev1.Container{Name: "proxy-rollout-trigger", Image: consts.DefaultDataPlaneBaseImage + ":3.3"},
			)
		}),
		waitTime, tickTime)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var previewDeployments appsv1.DeploymentList
		require.NoError(ct, mgr.GetClient().List(ctx, &previewDeployments,
			client.InNamespace(ns.Name),
			client.MatchingLabels{
				"app":                                dp.Name,
				consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValuePreview,
				consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
			},
		))
		require.Len(ct, previewDeployments.Items, 1)

		previewDeployment := previewDeployments.Items[0]
		previewDeployment.Status = appsv1.DeploymentStatus{
			AvailableReplicas: 1,
			ReadyReplicas:     1,
			Replicas:          1,
		}
		require.NoError(ct, mgr.GetClient().Status().Update(ctx, &previewDeployment))
	}, waitTime, tickTime)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var liveDeployments appsv1.DeploymentList
		require.NoError(ct, mgr.GetClient().List(ctx, &liveDeployments,
			client.InNamespace(ns.Name),
			client.MatchingLabels{
				"app":                                dp.Name,
				consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
				consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
			},
		))
		require.Len(ct, liveDeployments.Items, 1)

		liveDeployment := liveDeployments.Items[0]
		liveDeployment.Status = appsv1.DeploymentStatus{
			AvailableReplicas: 0,
			ReadyReplicas:     0,
			Replicas:          0,
		}
		require.NoError(ct, mgr.GetClient().Status().Update(ctx, &liveDeployment))
	}, waitTime, tickTime)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		current := &operatorv1beta1.DataPlane{}
		require.NoError(ct, mgr.GetClient().Get(ctx, client.ObjectKeyFromObject(dp), current))

		readyCondition, ok := k8sutils.GetCondition(kcfgdataplane.ReadyType, current)
		require.True(ct, ok)
		assert.Equal(ct, metav1.ConditionFalse, readyCondition.Status)
		assert.EqualValues(ct, kcfgdataplane.WaitingToBecomeReadyReason, readyCondition.Reason)

		rolledOutCondition, ok := k8sutils.GetCondition(kcfgdataplane.DataPlaneConditionTypeRolledOut, current.Status.RolloutStatus)
		require.True(ct, ok)
		assert.Equal(ct, metav1.ConditionFalse, rolledOutCondition.Status)
		assert.EqualValues(ct, kcfgdataplane.DataPlaneConditionReasonRolloutAwaitingPromotion, rolledOutCondition.Reason)
	}, waitTime, tickTime)
}
