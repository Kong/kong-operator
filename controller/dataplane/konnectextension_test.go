package dataplane

import (
	"context"
	"sort"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	dputils "github.com/kong/gateway-operator/internal/utils/dataplane"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	operatorv1alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

func TestApplyKonnectExtension(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, operatorv1alpha1.AddToScheme(s))
	require.NoError(t, operatorv1beta1.AddToScheme(s))

	tests := []struct {
		name          string
		dataPlane     *operatorv1beta1.DataPlane
		konnectExt    *operatorv1alpha1.KonnectExtension
		secret        *corev1.Secret
		expectedError error
	}{
		{
			name: "no extensions",
			dataPlane: &operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Extensions: []commonv1alpha1.ExtensionRef{},
					},
				},
			},
		},
		{
			name: "cross-namespace reference",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: operatorv1alpha1.SchemeGroupVersion.Group,
								Kind:  operatorv1alpha1.KonnectExtensionKind,
								NamespacedRef: commonv1alpha1.NamespacedRef{
									Name:      "konnect-ext",
									Namespace: lo.ToPtr("other"),
								},
							},
						},
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{},
							},
						},
					},
				},
			},
			konnectExt: &operatorv1alpha1.KonnectExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "konnect-ext",
					Namespace: "other",
				},
				Spec: operatorv1alpha1.KonnectExtensionSpec{
					AuthConfiguration: operatorv1alpha1.KonnectControlPlaneAPIAuthConfiguration{
						ClusterCertificateSecretRef: operatorv1alpha1.ClusterCertificateSecretRef{
							Name: "cluster-cert-secret",
						},
					},
					ControlPlaneRef: configurationv1alpha1.ControlPlaneRef{
						KonnectID: lo.ToPtr("konnect-id"),
					},
					ControlPlaneRegion: "us-west",
					ServerHostname:     "konnect.example.com",
				},
			},
			expectedError: ErrCrossNamespaceReference,
		},
		{
			name: "Extension not found",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: operatorv1alpha1.SchemeGroupVersion.Group,
								Kind:  operatorv1alpha1.KonnectExtensionKind,
								NamespacedRef: commonv1alpha1.NamespacedRef{
									Name: "konnect-ext",
								},
							},
						},
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{},
							},
						},
					},
				},
			},
			expectedError: ErrKonnectExtensionNotFound,
		},
		{
			name: "Extension properly referenced, secret not found",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: operatorv1alpha1.SchemeGroupVersion.Group,
								Kind:  operatorv1alpha1.KonnectExtensionKind,
								NamespacedRef: commonv1alpha1.NamespacedRef{
									Name: "konnect-ext",
								},
							},
						},
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{},
							},
						},
					},
				},
			},
			konnectExt: &operatorv1alpha1.KonnectExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "konnect-ext",
					Namespace: "default",
				},
				Spec: operatorv1alpha1.KonnectExtensionSpec{
					AuthConfiguration: operatorv1alpha1.KonnectControlPlaneAPIAuthConfiguration{
						ClusterCertificateSecretRef: operatorv1alpha1.ClusterCertificateSecretRef{
							Name: "cluster-cert-secret",
						},
					},
					ControlPlaneRef: configurationv1alpha1.ControlPlaneRef{
						KonnectID: lo.ToPtr("konnect-id"),
					},
					ControlPlaneRegion: "us-west",
					ServerHostname:     "konnect.example.com",
				},
			},
			expectedError: ErrClusterCertificateNotFound,
		},
		{
			name: "Extension properly referenced, no deployment Options set.",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: operatorv1alpha1.SchemeGroupVersion.Group,
								Kind:  operatorv1alpha1.KonnectExtensionKind,
								NamespacedRef: commonv1alpha1.NamespacedRef{
									Name: "konnect-ext",
								},
							},
						},
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{},
							},
						},
					},
				},
			},
			konnectExt: &operatorv1alpha1.KonnectExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "konnect-ext",
					Namespace: "default",
				},
				Spec: operatorv1alpha1.KonnectExtensionSpec{
					AuthConfiguration: operatorv1alpha1.KonnectControlPlaneAPIAuthConfiguration{
						ClusterCertificateSecretRef: operatorv1alpha1.ClusterCertificateSecretRef{
							Name: "cluster-cert-secret",
						},
					},
					ControlPlaneRef: configurationv1alpha1.ControlPlaneRef{
						KonnectID: lo.ToPtr("konnect-id"),
					},
					ControlPlaneRegion: "us-west",
					ServerHostname:     "konnect.example.com",
				},
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-cert-secret",
					Namespace: "default",
				},
			},
		},
		{
			name: "Extension properly referenced, with deployment Options set.",
			dataPlane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: operatorv1alpha1.SchemeGroupVersion.Group,
								Kind:  operatorv1alpha1.KonnectExtensionKind,
								NamespacedRef: commonv1alpha1.NamespacedRef{
									Name: "konnect-ext",
								},
							},
						},
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name: "proxy",
												Env: []corev1.EnvVar{
													{
														Name:  "KONG_TEST",
														Value: "test",
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
			konnectExt: &operatorv1alpha1.KonnectExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "konnect-ext",
					Namespace: "default",
				},
				Spec: operatorv1alpha1.KonnectExtensionSpec{
					AuthConfiguration: operatorv1alpha1.KonnectControlPlaneAPIAuthConfiguration{
						ClusterCertificateSecretRef: operatorv1alpha1.ClusterCertificateSecretRef{
							Name: "cluster-cert-secret",
						},
					},
					ControlPlaneRef: configurationv1alpha1.ControlPlaneRef{
						KonnectID: lo.ToPtr("konnect-id"),
					},
					ControlPlaneRegion: "us-west",
					ServerHostname:     "konnect.example.com",
				},
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-cert-secret",
					Namespace: "default",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs := []runtime.Object{tt.dataPlane}
			if tt.konnectExt != nil {
				objs = append(objs, tt.konnectExt)
			}
			if tt.secret != nil {
				objs = append(objs, tt.secret)
			}
			cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

			dataplane := tt.dataPlane.DeepCopy()
			err := applyKonnectExtension(context.Background(), cl, dataplane)
			if tt.expectedError != nil {
				require.ErrorIs(t, err, tt.expectedError)
			} else {
				require.NoError(t, err)
				requiredEnv := []corev1.EnvVar{}
				if tt.dataPlane.Spec.Deployment.PodTemplateSpec != nil {
					if container := k8sutils.GetPodContainerByName(&tt.dataPlane.Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName); container != nil {
						requiredEnv = container.Env
					}
				}

				if tt.konnectExt != nil {
					requiredEnv = append(requiredEnv, getKongInKonnectEnvVars(*tt.konnectExt)...)
					sort.Sort(k8sutils.SortableEnvVars(requiredEnv))
					assert.NotNil(t, dataplane.Spec.Deployment.PodTemplateSpec)
					assert.Equal(t, requiredEnv, dataplane.Spec.Deployment.PodTemplateSpec.Spec.Containers[0].Env)
				}
			}
		})
	}
}

func getKongInKonnectEnvVars(konnectExt operatorv1alpha1.KonnectExtension) []corev1.EnvVar {
	envSet := []corev1.EnvVar{}
	for k, v := range dputils.KongInKonnectDefaults(dputils.KongInKonnectParams{
		ControlPlane: *konnectExt.Spec.ControlPlaneRef.KonnectID,
		Region:       konnectExt.Spec.ControlPlaneRegion,
		Server:       konnectExt.Spec.ServerHostname,
	}) {
		envSet = append(envSet, corev1.EnvVar{
			Name:  k,
			Value: v,
		})
	}
	return envSet
}
