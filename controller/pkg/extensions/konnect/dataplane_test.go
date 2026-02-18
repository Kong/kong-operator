package konnect

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func TestDataPlaneKonnectExtensionProcessor_Process(t *testing.T) {
	s := scheme.Get()

	const (
		testNamespace  = "test-namespace"
		extensionName  = "test-konnect-extension"
		secretName     = "test-tls-secret"
		controlPlaneID = "test-control-plane-id"
	)

	// Helper function to create a valid KonnectExtension with status filled.
	createValidKonnectExtension := func() *konnectv1alpha2.KonnectExtension {
		return &konnectv1alpha2.KonnectExtension{
			ObjectMeta: metav1.ObjectMeta{
				Name:      extensionName,
				Namespace: testNamespace,
			},
			Status: konnectv1alpha2.KonnectExtensionStatus{
				Konnect: &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
					ControlPlaneID: controlPlaneID,
					Endpoints: konnectv1alpha2.KonnectEndpoints{
						ControlPlaneEndpoint: "7b46471d3b.us.cp.konghq.tech",
						TelemetryEndpoint:    "7b46471d3b.us.tp.konghq.tech",
					},
					ClusterType: konnectv1alpha2.ClusterTypeControlPlane,
				},
				DataPlaneClientAuth: &konnectv1alpha2.DataPlaneClientAuthStatus{
					CertificateSecretRef: &konnectv1alpha2.SecretRef{
						Name: secretName,
					},
				},
				Conditions: []metav1.Condition{
					{
						Type:   konnectv1alpha2.KonnectExtensionReadyConditionType,
						Status: metav1.ConditionTrue,
					},
				},
			},
		}
	}

	// Helper function to create a valid DataPlane.
	createValidDataPlane := func() *operatorv1beta1.DataPlane {
		return &operatorv1beta1.DataPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dataplane",
				Namespace: testNamespace,
			},
			Spec: operatorv1beta1.DataPlaneSpec{
				DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
					Extensions: []commonv1alpha1.ExtensionRef{
						{
							Group: konnectv1alpha1.SchemeGroupVersion.Group,
							Kind:  konnectv1alpha2.KonnectExtensionKind,
							NamespacedRef: commonv1alpha1.NamespacedRef{
								Name: extensionName,
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
											Image: "kong:latest",
										},
									},
								},
							},
						},
					},
				},
			},
		}
	}

	clientWithObjects := func(s *runtime.Scheme, objs ...client.Object) client.Client {
		return fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(objs...).
			Build()
	}

	tests := []struct {
		name            string
		object          *operatorv1beta1.DataPlane
		setupClient     func(t *testing.T) client.Client
		wantProcessed   bool
		wantErr         bool
		wantErrContains string
		checkAssertions func(t *testing.T, dp *operatorv1beta1.DataPlane)
	}{
		{
			name:   "success - KonnectExtension with status filled",
			object: createValidDataPlane(),
			setupClient: func(t *testing.T) client.Client {
				return clientWithObjects(s, createValidKonnectExtension())
			},
			wantProcessed: true,
			wantErr:       false,
			checkAssertions: func(t *testing.T, dp *operatorv1beta1.DataPlane) {
				require.NotNil(t, dp.Spec.Deployment.PodTemplateSpec)

				volumes := dp.Spec.Deployment.PodTemplateSpec.Spec.Volumes
				hasClusterCertVolume := lo.ContainsBy(volumes, func(v corev1.Volume) bool {
					return v.Name == consts.KongClusterCertVolume
				})
				assert.True(t, hasClusterCertVolume, "expected KongClusterCertVolume to be present")

				hasClusterCertificateVolume := lo.ContainsBy(volumes, func(v corev1.Volume) bool {
					return v.Name == consts.ClusterCertificateVolume
				})
				assert.True(t, hasClusterCertificateVolume, "expected ClusterCertificateVolume to be present")

				container := k8sutils.GetPodContainerByName(&dp.Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
				require.NotNil(t, container)

				hasClusterCertVolumeMount := lo.ContainsBy(container.VolumeMounts, func(vm corev1.VolumeMount) bool {
					return vm.Name == consts.KongClusterCertVolume &&
						vm.MountPath == consts.KongClusterCertVolumeMountPath
				})
				assert.True(t, hasClusterCertVolumeMount, "expected KongClusterCertVolume mount to be present")

				hasClusterCertificateVolumeMount := lo.ContainsBy(container.VolumeMounts, func(vm corev1.VolumeMount) bool {
					return vm.Name == consts.ClusterCertificateVolume &&
						vm.MountPath == consts.ClusterCertificateVolumeMountPath
				})
				assert.True(t, hasClusterCertificateVolumeMount, "expected ClusterCertificateVolume mount to be present")

				require.NotEmpty(t, container.Env, "expected environment variables to be set")

				envMap := make(map[string]string)
				for _, env := range container.Env {
					envMap[env.Name] = env.Value
				}

				assert.Contains(t, envMap, "KONG_KONNECT_MODE", "expected KONG_KONNECT_MODE env var")
				assert.Equal(t, "on", envMap["KONG_KONNECT_MODE"])
				assert.Contains(t, envMap, "KONG_CLUSTER_MTLS", "expected KONG_CLUSTER_MTLS env var")
				assert.Equal(t, "pki", envMap["KONG_CLUSTER_MTLS"])
			},
		},
		{
			name:   "failure - KonnectExtension status not filled",
			object: createValidDataPlane(),
			setupClient: func(t *testing.T) client.Client {
				ext := createValidKonnectExtension()
				ext.Status = konnectv1alpha2.KonnectExtensionStatus{
					Conditions: []metav1.Condition{
						{
							Type:   konnectv1alpha2.KonnectExtensionReadyConditionType,
							Status: metav1.ConditionTrue,
						},
					},
				}

				return clientWithObjects(s, ext)
			},
			wantProcessed:   false,
			wantErr:         true,
			wantErrContains: "konnect extension is not ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := tt.setupClient(t)

			p := &DataPlaneKonnectExtensionProcessor{}
			processed, err := p.Process(t.Context(), cl, tt.object)

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrContains != "" {
					assert.Contains(t, err.Error(), tt.wantErrContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantProcessed, processed)

			if tt.checkAssertions != nil {
				tt.checkAssertions(t, tt.object)
			}
		})
	}
}
