package dataplane

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	kcfgdataplane "github.com/kong/kong-operator/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/dataplane"
	"github.com/kong/kong-operator/v2/controller/konnect"
	secretcert "github.com/kong/kong-operator/v2/controller/secret_cert"
	"github.com/kong/kong-operator/v2/internal/versions"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/pkg/ipfamily"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/create"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestDataPlaneBlueGreen(t *testing.T) {
	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())

	// Uncached client: the Secret is seeded before StartReconcilers starts the
	// manager, and going through the manager's client that early deadlocks under
	// read-your-own-writes consistency.
	clusterCA := create.ClusterCASecret(t, ctx, envtest.NewControllerClient(t, scheme.Get(), cfg), ns.Name, "cluster-ca-bluegreen-reconcile")
	bgReconciler := &dataplane.BlueGreenReconciler{
		DataPlaneIPFamily:        ipfamily.IPv4,
		Client:                   mgr.GetClient(),
		ClusterCASecretName:      clusterCA.Name,
		ClusterCASecretNamespace: clusterCA.Namespace,
		DefaultImage:             consts.DefaultDataPlaneImage,
		ValidateDataPlaneImage:   true,
		CertTTL:                  consts.DefaultCertTTL,
		DataPlaneController: &dataplane.Reconciler{
			DataPlaneIPFamily:        ipfamily.IPv4,
			Client:                   mgr.GetClient(),
			ClusterCASecretName:      clusterCA.Name,
			ClusterCASecretNamespace: clusterCA.Namespace,
			DefaultImage:             consts.DefaultDataPlaneImage,
			ValidateDataPlaneImage:   true,
			CertTTL:                  consts.DefaultCertTTL,
		},
	}
	secretCertReconciler := &secretcert.Reconciler{
		Client:               mgr.GetClient(),
		CertExpirationMargin: consts.DefaultCertExpirationMargin,
	}
	envtest.StartReconcilers(ctx, t, mgr, logs,
		bgReconciler,
		secretCertReconciler,
	)

	var (
		originalImage = consts.DefaultDataPlaneBaseImage + ":" + versions.MinimumDataPlaneVersion.String()
		upgradedImage = consts.DefaultDataPlaneBaseImage + ":" + consts.DefaultDataPlaneTag
	)

	dp := &operatorv1beta1.DataPlane{
		Name:      "dp-bluegreen-reconcile",
		Namespace: ns.Name,
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
									Image: originalImage,
								}},
							},
						},
					},
				},
			},
		},
	}
	require.NoError(t, mgr.GetClient().Create(ctx, dp))

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

		ingressService := services.Items[0]
		ingressService.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "6.7.8.9"}}
		require.NoError(ct, mgr.GetClient().Status().Update(ctx, &ingressService))
	}, waitTime, tickTime)

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
		liveProxy := k8sutils.GetPodContainerByName(&liveDeployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(ct, liveProxy)
		require.Equal(ct, originalImage, liveProxy.Image)

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
			proxy := k8sutils.GetPodContainerByName(&dp.Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
			if !assert.NotNil(t, proxy) {
				return
			}
			proxy.Image = upgradedImage
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
		previewProxy := k8sutils.GetPodContainerByName(&previewDeployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(ct, previewProxy)
		require.Equal(ct, upgradedImage, previewProxy.Image)

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

		// With BreakBeforePromotion the live Deployment keeps the original image until promoted.
		liveDeployment := liveDeployments.Items[0]
		liveProxy := k8sutils.GetPodContainerByName(&liveDeployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(ct, liveProxy)
		require.Equal(ct, originalImage, liveProxy.Image)

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

func TestDataPlaneBlueGreenKonnectHybrid(t *testing.T) {
	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	cl := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	factory := sdkmocks.NewMockSDKFactory(t)
	factory.SDK.DataPlaneCertificatesSDK.EXPECT().CreateDataplaneCertificate(mock.Anything, mock.Anything, mock.Anything).
		Return(&sdkkonnectops.CreateDataplaneCertificateResponse{
			DataPlaneClientCertificateResponse: &sdkkonnectcomp.DataPlaneClientCertificateResponse{
				Item: &sdkkonnectcomp.DataPlaneClientCertificate{
					ID:   new("111111111111111111111111111111111-4"),
					Cert: new(deploy.TestValidCACertPEM),
				},
			},
		}, nil)

	// Uncached client: see the note in TestDataPlaneBlueGreen.
	clusterCA := create.ClusterCASecret(t, ctx, envtest.NewControllerClient(t, scheme.Get(), cfg), ns.Name, "cluster-ca-bluegreen-hybrid")
	bgReconciler := &dataplane.BlueGreenReconciler{
		DataPlaneIPFamily:        ipfamily.IPv4,
		Client:                   cl,
		ClusterCASecretName:      clusterCA.Name,
		ClusterCASecretNamespace: clusterCA.Namespace,
		DefaultImage:             consts.DefaultDataPlaneImage,
		ValidateDataPlaneImage:   true,
		CertTTL:                  consts.DefaultCertTTL,
		KonnectEnabled:           true,
		DataPlaneController: &dataplane.Reconciler{
			DataPlaneIPFamily:        ipfamily.IPv4,
			Client:                   cl,
			ClusterCASecretName:      clusterCA.Name,
			ClusterCASecretNamespace: clusterCA.Namespace,
			DefaultImage:             consts.DefaultDataPlaneImage,
			ValidateDataPlaneImage:   true,
			CertTTL:                  consts.DefaultCertTTL,
			KonnectEnabled:           true,
		},
	}
	envtest.StartReconcilers(ctx, t, mgr, logs,
		bgReconciler,
		&secretcert.Reconciler{
			Client:               mgr.GetClient(),
			CertExpirationMargin: consts.DefaultCertExpirationMargin,
		},
		&konnect.KonnectExtensionReconciler{
			Client:                   cl,
			SyncPeriod:               konnectInfiniteSyncTime,
			ClusterCASecretName:      clusterCA.Name,
			ClusterCASecretNamespace: clusterCA.Namespace,
			CertTTL:                  consts.DefaultCertTTL,
		},
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongDataPlaneClientCertificate](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongDataPlaneClientCertificate](&metricsmocks.MockRecorder{}),
		),
	)

	auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, cl, auth, deploy.WithKonnectID("aee0667a-90c6-45a6-a2d8-575e1e487b90"))
	ke := &konnectv1alpha2.KonnectExtension{
		Name:      "ke-bluegreen-hybrid",
		Namespace: ns.Name,
		Spec: konnectv1alpha2.KonnectExtensionSpec{
			Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
				ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
					Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
						Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: cp.Name},
					},
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, ke))

	var (
		originalImage = consts.DefaultDataPlaneBaseImage + ":" + versions.MinimumDataPlaneVersion.String()
		upgradedImage = consts.DefaultDataPlaneBaseImage + ":" + consts.DefaultDataPlaneTag
	)
	dp := &operatorv1beta1.DataPlane{
		Name:      "dp-bluegreen-hybrid",
		Namespace: ns.Name,
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Extensions: []commonv1alpha1.ExtensionRef{{
					Group: konnectv1alpha1.GroupVersion.Group,
					Kind:  "KonnectExtension",
					Name:  ke.Name,
				}},
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
									Image: originalImage,
								}},
							},
						},
					},
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, dp))

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var services corev1.ServiceList
		require.NoError(ct, cl.List(ctx, &services,
			client.MatchingLabels{
				"app":                             dp.Name,
				consts.DataPlaneServiceTypeLabel:  string(consts.DataPlaneIngressServiceLabelValue),
				consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValueLive,
			},
		))
		require.Len(ct, services.Items, 1)

		ingressService := services.Items[0]
		ingressService.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "6.7.8.9"}}
		require.NoError(ct, cl.Status().Update(ctx, &ingressService))
	}, waitTime, tickTime)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var deploymentList appsv1.DeploymentList
		require.NoError(ct, cl.List(ctx, &deploymentList,
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
		require.NoError(ct, cl.Status().Update(ctx, &liveDeployment))
	}, waitTime, tickTime)

	require.Eventually(t,
		testutils.DataPlaneUpdateEventually(t, ctx, client.ObjectKeyFromObject(dp), cl, func(dp *operatorv1beta1.DataPlane) {
			proxy := k8sutils.GetPodContainerByName(&dp.Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
			if !assert.NotNil(t, proxy) {
				return
			}
			proxy.Image = upgradedImage
		}),
		waitTime, tickTime)

	t.Log("Verifying the preview Deployment has no admin API and no admin Services exist")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var previewDeployments appsv1.DeploymentList
		require.NoError(ct, cl.List(ctx, &previewDeployments,
			client.MatchingLabels{
				"app":                                dp.Name,
				consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValuePreview,
				consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
			},
		))
		require.Len(ct, previewDeployments.Items, 1)
		previewProxy := k8sutils.GetPodContainerByName(&previewDeployments.Items[0].Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(ct, previewProxy)
		require.Equal(ct, upgradedImage, previewProxy.Image)
		assertNoAdminAPIInDeployment(ct, &previewDeployments.Items[0])

		var adminServices corev1.ServiceList
		require.NoError(ct, cl.List(ctx, &adminServices,
			client.MatchingLabels{
				"app":                            dp.Name,
				consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneAdminServiceLabelValue),
			},
		))
		require.Empty(ct, adminServices.Items, "expected no live nor preview admin Services")

		current := &operatorv1beta1.DataPlane{}
		require.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(dp), current))
		assertNoAdminAPIResources(ct, ctx, cl, current)
		require.NotNil(ct, current.Status.RolloutStatus)
		require.NotNil(ct, current.Status.RolloutStatus.Services)
		require.NotNil(ct, current.Status.RolloutStatus.Services.Ingress)
		require.Nil(ct, current.Status.RolloutStatus.Services.AdminAPI)
	}, waitTime, tickTime)

	var deployments appsv1.DeploymentList
	require.NoError(t, cl.List(ctx, &deployments, client.MatchingLabels{"app": dp.Name}))
	require.Len(t, deployments.Items, 2)
	for _, d := range deployments.Items {
		require.False(t, lo.ContainsBy(d.Spec.Template.Spec.Volumes, func(v corev1.Volume) bool {
			return v.Name == consts.ClusterCertificateVolume
		}), "expected no cluster certificate volume in Deployment %s", d.Name)
	}
}
