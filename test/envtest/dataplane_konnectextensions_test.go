package envtest

import (
	"fmt"
	"net/http"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnect2 "github.com/kong/kong-operator/v2/api/konnect"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/dataplane"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestDataPlaneKonnectExtension(t *testing.T) {
	t.Parallel()

	for _, keyType := range []certificate.KeyType{certificate.RSA, certificate.ECDSA} {
		t.Run("for CA with key type "+string(keyType), func(t *testing.T) {
			cfg, ns := Setup(t, t.Context(), scheme.Get(), WithInstallGatewayCRDs(true))
			ctx := t.Context()

			mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

			cl := client.NewNamespacedClient(mgr.GetClient(), ns.Name)
			factory := sdkmocks.NewMockSDKFactory(t)
			sdk := factory.SDK.DataPlaneCertificatesSDK

			clusterCASecretNN := types.NamespacedName{
				Name:      fmt.Sprintf("cluster-ca-%s", keyType),
				Namespace: ns.Name,
			}

			t.Logf("Creating cluster CA secret %s (key type: %s)", clusterCASecretNN, keyType)
			cert, key := certificate.MustGenerateCertPEMFormat(
				certificate.WithCommonName("kong-operator-cluster-ca"),
				certificate.WithCATrue(),
				certificate.WithKeyType(keyType),
			)
			caSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: clusterCASecretNN.Namespace,
					Name:      clusterCASecretNN.Name,
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
			require.NoError(t, cl.Create(ctx, caSecret))

			dpReconciler := &dataplane.Reconciler{
				Client:                   cl,
				ClusterCASecretName:      clusterCASecretNN.Name,
				ClusterCASecretNamespace: clusterCASecretNN.Namespace,
				DefaultImage:             consts.DefaultDataPlaneImage,
				ValidateDataPlaneImage:   true,
				KonnectEnabled:           true,
				EnforceConfig:            true,
			}
			konnectExtensionReconciler := &konnect.KonnectExtensionReconciler{
				Client:     cl,
				SdkFactory: factory,
				// To ensure we don't resync in test. Reconciler will be called automatically on changes.
				SyncPeriod:               konnectInfiniteSyncTime,
				ClusterCASecretName:      clusterCASecretNN.Name,
				ClusterCASecretNamespace: clusterCASecretNN.Namespace,
			}

			StartReconcilers(ctx, t, mgr, logs,
				dpReconciler,
				konnectExtensionReconciler,
				konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
					konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongDataPlaneClientCertificate](konnectInfiniteSyncTime),
					konnect.WithMetricRecorder[configurationv1alpha1.KongDataPlaneClientCertificate](&metricsmocks.MockRecorder{}),
				),
			)

			t.Run("base", func(t *testing.T) {
				const (
					konnectControlPlaneID = "aee0667a-90c6-45a6-a2d8-575e1e487b86"
					dpCertID              = "111111111111111111111111111111111-2"
				)

				t.Logf("Setting up expected ListDpClientCertificates SDK call returning no certificates")
				// TODO: https://github.com/Kong/kong-operator/issues/2630 this call can be removed when 2630 is done.
				sdk.EXPECT().ListDpClientCertificates(mock.Anything, konnectControlPlaneID).
					Return(&sdkkonnectops.ListDpClientCertificatesResponse{
						StatusCode: http.StatusOK,
					}, nil)

				sdk.EXPECT().CreateDataplaneCertificate(mock.Anything, konnectControlPlaneID, mock.Anything).
					Return(&sdkkonnectops.CreateDataplaneCertificateResponse{
						DataPlaneClientCertificateResponse: &sdkkonnectcomp.DataPlaneClientCertificateResponse{
							Item: &sdkkonnectcomp.DataPlaneClientCertificate{
								ID:   new(dpCertID),
								Cert: new(deploy.TestValidCACertPEM),
							},
						},
					}, nil)

				t.Logf("Waiting for caches to sync as CA manager relies on it")
				mgr.GetCache().WaitForCacheSync(ctx)

				t.Logf("Creating KonnectAPIAuthConfiguration")
				konnectAPIAuthConfiguration := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)

				t.Logf("Creating and setting expecting status for corresponding KonnectControlPlane with Konnect ID: %s", konnectControlPlaneID)
				cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, cl, konnectAPIAuthConfiguration, deploy.WithKonnectID(konnectControlPlaneID))

				t.Logf("Creating KonnectExtension")
				konnectExtension := konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "ke-",
						Namespace:    ns.Name,
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: cp.Name,
									},
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
					require.True(t,
						k8sutils.HasConditionTrue("Ready", &konnectExtension),
						"expected KonnectExtension to have a ready condition, got: %+v", konnectExtension.Status.Conditions,
					)
				}, waitTime, tickTime)

				t.Logf("Waiting for Deployment to be created and verifying Deployment has KonnectExtension applied")
				require.EventuallyWithT(t, func(t *assert.CollectT) {
					var deployments appsv1.DeploymentList
					require.NoError(t, cl.List(ctx, &deployments,
						client.InNamespace(ns.Name),
						client.MatchingLabels{
							"app": dp.Name,
						},
					))

					require.Len(t, deployments.Items, 1)
					createdDeployment := &deployments.Items[0]

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
			})

			t.Run("DataPlane with custom volumes", func(t *testing.T) {
				const (
					konnectControlPlaneID = "aee0667a-90c6-45a6-a2d8-575e1e487b88"
					dpCertID              = "111111111111111111111111111111111-1"
				)

				t.Logf("Setting up expected ListDpClientCertificates SDK call returning no certificates")
				sdk.EXPECT().ListDpClientCertificates(mock.Anything, konnectControlPlaneID).
					Return(&sdkkonnectops.ListDpClientCertificatesResponse{
						StatusCode: http.StatusOK,
					}, nil)

				sdk.EXPECT().CreateDataplaneCertificate(mock.Anything, konnectControlPlaneID, mock.Anything).
					Return(&sdkkonnectops.CreateDataplaneCertificateResponse{
						DataPlaneClientCertificateResponse: &sdkkonnectcomp.DataPlaneClientCertificateResponse{
							Item: &sdkkonnectcomp.DataPlaneClientCertificate{
								ID:   new(dpCertID),
								Cert: new(deploy.TestValidCACertPEM),
							},
						},
					}, nil)

				t.Log("Check if user provided volumes and volume mounts are preserved when KonnectExtension is applied to DataPlane")

				t.Logf("Waiting for caches to sync as CA manager relies on it")
				mgr.GetCache().WaitForCacheSync(ctx)

				t.Logf("Creating KonnectAPIAuthConfiguration")
				konnectAPIAuthConfiguration := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)

				t.Logf("Creating and setting expecting status for corresponding KonnectControlPlane with Konnect ID: %s", konnectControlPlaneID)
				cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, cl, konnectAPIAuthConfiguration, deploy.WithKonnectID(konnectControlPlaneID))

				t.Logf("Creating KonnectExtension")
				konnectExtension := konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "ke-",
						Namespace:    ns.Name,
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: cp.Name,
									},
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
											Volumes: []corev1.Volume{
												{
													Name: "custom-volume",
													VolumeSource: corev1.VolumeSource{
														EmptyDir: &corev1.EmptyDirVolumeSource{
															SizeLimit: resource.NewQuantity(100, resource.Format("Gi")),
														},
													},
												},
											},
											Containers: []corev1.Container{
												{
													Name:  consts.DataPlaneProxyContainerName,
													Image: consts.DefaultDataPlaneImage,
													VolumeMounts: []corev1.VolumeMount{
														{
															Name:      "custom-volume",
															MountPath: "/var/custom-volume",
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
				}
				require.NoError(t, cl.Create(ctx, &dp))

				t.Logf("Waiting for KonnectExtension to become ready")
				require.EventuallyWithT(t, func(t *assert.CollectT) {
					require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(&konnectExtension), &konnectExtension))
					require.True(t,
						k8sutils.HasConditionTrue("Ready", &konnectExtension),
						"expected KonnectExtension to have a ready condition, got: %+v", konnectExtension.Status.Conditions,
					)
				}, waitTime, tickTime)

				t.Logf("Waiting for Deployment to be created and verifying Deployment has KonnectExtension applied")
				require.EventuallyWithT(t, func(t *assert.CollectT) {
					var deployments appsv1.DeploymentList
					require.NoError(t, cl.List(ctx, &deployments,
						client.InNamespace(ns.Name),
						client.MatchingLabels{
							"app": dp.Name,
						},
					))

					require.Len(t, deployments.Items, 1)
					createdDeployment := &deployments.Items[0]

					dpContainer := k8sutils.GetPodContainerByName(&createdDeployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
					require.NotNil(t, dpContainer)
					volumes := createdDeployment.Spec.Template.Spec.Volumes
					volumeMounts := dpContainer.VolumeMounts

					hasClusterCertVolume := lo.ContainsBy(createdDeployment.Spec.Template.Spec.Volumes, func(v corev1.Volume) bool {
						return v.Name == consts.ClusterCertificateVolume
					})
					require.Truef(t, hasClusterCertVolume, "expected deployment spec to have cluster certificate volume, got: %+v", volumes)

					hasCustomVolume := lo.ContainsBy(createdDeployment.Spec.Template.Spec.Volumes, func(v corev1.Volume) bool {
						return v.Name == "custom-volume"
					})
					require.Truef(t, hasCustomVolume, "expected deployment spec to have custom-volume volume, got: %+v", volumes)

					hasCustomVolumeMount := lo.ContainsBy(dpContainer.VolumeMounts, func(vm corev1.VolumeMount) bool {
						return vm.Name == "custom-volume" &&
							vm.MountPath == "/var/custom-volume"
					})
					require.True(t, hasCustomVolumeMount, "expected deployment spec to have custom volume mount, got: %+v", volumeMounts)

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
				}, waitTime, tickTime)

				t.Logf("Waiting for DataPlane to have KonnectExtensionApplied condition")
				require.EventuallyWithT(t, func(t *assert.CollectT) {
					require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(&dp), &dp))
					require.True(t, lo.ContainsBy(dp.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == string(konnect2.KonnectExtensionAppliedType) && c.Status == metav1.ConditionTrue
					}), "expected DataPlane to have KonnectExtensionApplied condition, got: %+v", dp.Status.Conditions)
				}, waitTime, tickTime)
			})
		})
	}

}
