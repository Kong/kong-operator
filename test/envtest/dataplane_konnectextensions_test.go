package envtest

import (
	"crypto/x509"
	"net/http"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/dataplane"
	"github.com/kong/gateway-operator/controller/konnect"
	sdkmocks "github.com/kong/gateway-operator/controller/konnect/ops/sdk/mocks"
	"github.com/kong/gateway-operator/controller/pkg/secrets"
	"github.com/kong/gateway-operator/modules/manager/logging"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	konnect2 "github.com/kong/kubernetes-configuration/api/konnect"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestDataPlaneKonnectExtension(t *testing.T) {
	t.Parallel()

	cfg, _ := Setup(t, t.Context(), scheme.Get())
	ctx := t.Context()

	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: NameFromT(t),
		},
	}
	require.NoError(t, mgr.GetClient().Create(ctx, ns))

	cl := client.NewNamespacedClient(mgr.GetClient(), ns.Name)
	factory := sdkmocks.NewMockSDKFactory(t)

	const (
		clusterCASecretName   = "cluster-ca"
		konnectControlPlaneID = "aee0667a-90c6-45a6-a2d8-575e1e487b86"
	)

	clusterCAKeyConfig := secrets.KeyConfig{
		Type: x509.RSA,
		Size: 2048,
	}
	dpReconciler := &dataplane.Reconciler{
		Client:                   cl,
		Scheme:                   scheme.Get(),
		ClusterCASecretName:      clusterCASecretName,
		ClusterCASecretNamespace: ns.Name,
		ClusterCAKeyConfig:       clusterCAKeyConfig,
		DefaultImage:             consts.DefaultDataPlaneImage,
		LoggingMode:              logging.DevelopmentMode,
		ValidateDataPlaneImage:   true,
		KonnectEnabled:           true,
		EnforceConfig:            true,
	}
	konnectExtensionReconciler := &konnect.KonnectExtensionReconciler{
		Client:                   cl,
		LoggingMode:              logging.DevelopmentMode,
		SdkFactory:               factory,
		SyncPeriod:               time.Hour * 24, // To ensure we don't resync in test. Reconciler will be called automatically on changes.
		ClusterCASecretName:      clusterCASecretName,
		ClusterCASecretNamespace: ns.Name,
		ClusterCAKeyConfig:       clusterCAKeyConfig,
	}

	StartReconcilers(ctx, t, mgr, logs,
		dpReconciler,
		konnectExtensionReconciler,
	)

	t.Logf("Setting up expected ListControlPlanes SDK call returning our control plane")
	factory.SDK.ControlPlaneSDK.EXPECT().ListControlPlanes(mock.Anything, mock.Anything).
		Return(
			&sdkkonnectops.ListControlPlanesResponse{
				StatusCode: http.StatusOK,
				ListControlPlanesResponse: &sdkkonnectcomp.ListControlPlanesResponse{
					Data: []sdkkonnectcomp.ControlPlane{
						{
							ID:   konnectControlPlaneID,
							Name: "konnect-cp",
							Config: sdkkonnectcomp.Config{
								ControlPlaneEndpoint: "cp.endpoint",
								TelemetryEndpoint:    "tp.endpoint",
								ClusterType:          sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlane,
							},
						},
					},
				},
			}, nil)

	t.Logf("Setting up expected ListDpClientCertificates SDK call returning no certificates")
	factory.SDK.DataPlaneCertificatesSDK.EXPECT().ListDpClientCertificates(mock.Anything, konnectControlPlaneID).
		Return(&sdkkonnectops.ListDpClientCertificatesResponse{
			StatusCode: http.StatusOK,
		}, nil)

	t.Logf("Setting up expected CreateDataplaneCertificate SDK call")
	factory.SDK.DataPlaneCertificatesSDK.EXPECT().CreateDataplaneCertificate(mock.Anything, konnectControlPlaneID, mock.Anything).
		Return(&sdkkonnectops.CreateDataplaneCertificateResponse{
			StatusCode: http.StatusCreated,
			DataPlaneClientCertificateResponse: &sdkkonnectcomp.DataPlaneClientCertificateResponse{
				Item: &sdkkonnectcomp.DataPlaneClientCertificate{
					ID: lo.ToPtr("dp-client-cert-id"),
				},
			},
		}, nil)

	t.Logf("Waiting for caches to sync as CA manager relies on it")
	mgr.GetCache().WaitForCacheSync(ctx)

	t.Logf("Creating cluster CA secret")
	require.NoError(t, secrets.CreateClusterCACertificate(ctx, cl, types.NamespacedName{
		Name:      clusterCASecretName,
		Namespace: ns.Name,
	}, clusterCAKeyConfig))

	t.Logf("Creating KonnectAPIAuthConfiguration")
	konnectAPIAuthConfiguration := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)

	t.Logf("Creating KonnectExtension")
	konnectExtension := konnectv1alpha1.KonnectExtension{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "ke-",
			Namespace:    ns.Name,
		},
		Spec: konnectv1alpha1.KonnectExtensionSpec{
			Konnect: konnectv1alpha1.KonnectExtensionKonnectSpec{
				ControlPlane: konnectv1alpha1.KonnectExtensionControlPlane{
					Ref: commonv1alpha1.ControlPlaneRef{
						Type:      commonv1alpha1.ControlPlaneRefKonnectID,
						KonnectID: lo.ToPtr(commonv1alpha1.KonnectIDType(konnectControlPlaneID)),
					},
				},
				Configuration: &konnectv1alpha1.KonnectConfiguration{
					APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
						Name: konnectAPIAuthConfiguration.Name,
					},
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, &konnectExtension))

	t.Logf("Creating DataPlane with KonnectExtension reference")
	dp := operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "dp-",
			Namespace:    ns.Name,
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						Group: konnectv1alpha1.GroupVersion.Group,
						Kind:  "KonnectExtension",
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: konnectExtension.Name,
						},
					},
				},
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
	}
	require.NoError(t, cl.Create(ctx, &dp))

	t.Logf("Waiting for KonnectExtension to become ready")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(&konnectExtension), &konnectExtension))
		conditions := konnectExtension.Status.Conditions
		require.True(t, lo.ContainsBy(conditions, func(c metav1.Condition) bool {
			return c.Type == "Ready" && c.Status == metav1.ConditionTrue
		}), "expected KonnectExtension to have a ready condition, got: %+v", conditions)
	}, waitTime, tickTime)

	t.Logf("Waiting for Deployment to be created")
	createdDeployment := &appsv1.Deployment{}
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		var deployments appsv1.DeploymentList
		require.NoError(t, cl.List(ctx, &deployments,
			client.InNamespace(ns.Name),
			client.MatchingLabels{
				"app": dp.Name,
			},
		))

		require.Len(t, deployments.Items, 1)
		createdDeployment = &deployments.Items[0]
	}, waitTime, tickTime)

	t.Logf("Verifying Deployment has KonnectExtension applied")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		dpContainer := k8sutils.GetPodContainerByName(&createdDeployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(t, dpContainer)
		volumes := createdDeployment.Spec.Template.Spec.Volumes
		volumeMounts := dpContainer.VolumeMounts

		hasClusterCertVolume := lo.ContainsBy(createdDeployment.Spec.Template.Spec.Volumes, func(v corev1.Volume) bool {
			return v.Name == consts.ClusterCertificateVolume
		})
		require.Truef(t, hasClusterCertVolume, "expected deployment spec to have cluster certificate volume, got: %+v", volumes)

		hasClusterCertVolumeMount := lo.ContainsBy(dpContainer.VolumeMounts, func(vm corev1.VolumeMount) bool {
			return vm.Name == consts.ClusterCertificateVolume &&
				vm.MountPath == consts.ClusterCertificateVolumeMountPath &&
				vm.ReadOnly == true
		})
		require.True(t, hasClusterCertVolumeMount, "expected deployment spec to have cluster certificate volume mount, got: %+v", volumeMounts)

		hasKongClusterCertVolume := lo.ContainsBy(createdDeployment.Spec.Template.Spec.Volumes, func(v corev1.Volume) bool {
			return v.Name == consts.KongClusterCertVolume
		})
		require.True(t, hasKongClusterCertVolume, "expected deployment spec to have Kong cluster certificate volume, got: %+v", volumes)

		hasKongClusterCertVolumeMount := lo.ContainsBy(dpContainer.VolumeMounts, func(vm corev1.VolumeMount) bool {
			return vm.Name == consts.KongClusterCertVolume &&
				vm.MountPath == consts.KongClusterCertVolumeMountPath
		})
		require.True(t, hasKongClusterCertVolumeMount, "expected deployment spec to have Kong cluster certificate volume mount, got: %+v", volumeMounts)

		expectedEnvVars := []corev1.EnvVar{
			{
				Name:  "KONG_CLUSTER_CERT",
				Value: consts.KongClusterCertVolumeMountPath + "/tls.crt",
			},
			{
				Name:  "KONG_CLUSTER_CERT_KEY",
				Value: consts.KongClusterCertVolumeMountPath + "/tls.key",
			},
			{
				Name:  "KONG_CLUSTER_CONTROL_PLANE",
				Value: "cp.endpoint:443",
			},
			{
				Name:  "KONG_CLUSTER_MTLS",
				Value: "pki",
			},
			{
				Name:  "KONG_CLUSTER_SERVER_NAME",
				Value: "cp.endpoint",
			},
			{
				Name:  "KONG_CLUSTER_TELEMETRY_ENDPOINT",
				Value: "tp.endpoint:443",
			},
			{
				Name:  "KONG_CLUSTER_TELEMETRY_SERVER_NAME",
				Value: "tp.endpoint",
			},
			{
				Name:  "KONG_KONNECT_MODE",
				Value: "on",
			},
			{
				Name:  "KONG_LUA_SSL_TRUSTED_CERTIFICATE",
				Value: "system",
			},
			{
				Name:  "KONG_ROLE",
				Value: "data_plane",
			},
			{
				Name:  "KONG_VITALS",
				Value: "off",
			},
		}
		for _, expectedEnvVar := range expectedEnvVars {
			assert.Contains(t, dpContainer.Env, expectedEnvVar)
		}
	}, waitTime, tickTime)

	t.Logf("Waiting for DataPlane to have KonnectExtensionApplied condition")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(&dp), &dp))
		require.True(t, lo.ContainsBy(dp.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == string(konnect2.KonnectExtensionAppliedType) && c.Status == metav1.ConditionTrue
		}), "expected DataPlane to have KonnectExtensionApplied condition, got: %+v", dp.Status.Conditions)
	}, waitTime, tickTime)
}
