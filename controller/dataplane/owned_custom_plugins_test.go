package dataplane

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestOptWithCustomPlugin(t *testing.T) {
	testCases := []struct {
		name                 string
		customPlugins        []customPlugin
		expectedEnv          []corev1.EnvVar
		expectedVolumes      []corev1.Volume
		expectedVolumeMounts []corev1.VolumeMount
		expectedAnnotations  map[string]string
	}{
		{
			name:          "no custom plugins",
			customPlugins: []customPlugin{},
		},
		{
			name: "one custom plugin",
			customPlugins: []customPlugin{
				{
					Name: "plugin1",
					ConfigMapNN: types.NamespacedName{
						Name: "configmap1",
					},
					Generation: 1,
				},
			},
			expectedEnv: []corev1.EnvVar{
				{
					Name:  "KONG_PLUGINS",
					Value: "bundled,plugin1",
				},
				{
					Name:  "KONG_LUA_PACKAGE_PATH",
					Value: "/opt/?.lua;;",
				},
			},
			expectedVolumes: []corev1.Volume{
				{
					Name: "plugin1",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "configmap1",
							},
						},
					},
				},
			},
			expectedVolumeMounts: []corev1.VolumeMount{
				{
					Name:      "plugin1",
					MountPath: "/opt/kong/plugins/plugin1",
				},
			},
			expectedAnnotations: map[string]string{
				consts.AnnotationKongPluginInstallationGenerationInternal: "plugin1:1",
			},
		},
		{
			name: "multiple custom plugins",
			customPlugins: []customPlugin{
				{
					Name: "plugin1",
					ConfigMapNN: types.NamespacedName{
						Name: "configmap1",
					},
					Generation: 1,
				},
				{
					Name: "plugin2",
					ConfigMapNN: types.NamespacedName{
						Name: "configmap2",
					},
					Generation: 2,
				},
			},
			expectedEnv: []corev1.EnvVar{
				{
					Name:  "KONG_PLUGINS",
					Value: "bundled,plugin1,plugin2",
				},
				{
					Name:  "KONG_LUA_PACKAGE_PATH",
					Value: "/opt/?.lua;;",
				},
			},
			expectedVolumes: []corev1.Volume{
				{
					Name: "plugin1",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "configmap1",
							},
						},
					},
				},
				{
					Name: "plugin2",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "configmap2",
							},
						},
					},
				},
			},
			expectedVolumeMounts: []corev1.VolumeMount{
				{
					Name:      "plugin1",
					MountPath: "/opt/kong/plugins/plugin1",
				},
				{
					Name:      "plugin2",
					MountPath: "/opt/kong/plugins/plugin2",
				},
			},
			expectedAnnotations: map[string]string{
				consts.AnnotationKongPluginInstallationGenerationInternal: "plugin1:1,plugin2:2",
			},
		},
	}

	for _, tt := range testCases {
		t.Run(
			tt.name,
			func(t *testing.T) {
				deployment := &appsv1.Deployment{
					Spec: appsv1.DeploymentSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{},
								},
							},
						},
					},
				}
				withCustomPlugins(tt.customPlugins...)(deployment)

				require.Equal(t, tt.expectedAnnotations, deployment.Spec.Template.Annotations)
				require.Equal(t, tt.expectedEnv, deployment.Spec.Template.Spec.Containers[0].Env)
				require.Equal(t, tt.expectedVolumeMounts, deployment.Spec.Template.Spec.Containers[0].VolumeMounts)
				require.Equal(t, tt.expectedVolumes, deployment.Spec.Template.Spec.Volumes)
			},
		)
	}
}

func TestOptNoopWithCustomPlugin(t *testing.T) {
	const (
		annotationThatShouldBePreservedKey   = "annotation-to-preserve"
		annotationThatShouldBePreservedValue = "this-is-it"
	)
	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						consts.AnnotationKongPluginInstallationGenerationInternal: "plugin1:1",
						annotationThatShouldBePreservedKey:                        annotationThatShouldBePreservedValue,
					},
				},
			},
		},
	}
	withCustomPlugins()(deployment)

	require.Equal(
		t, map[string]string{annotationThatShouldBePreservedKey: annotationThatShouldBePreservedValue}, deployment.Spec.Template.Annotations,
	)
}
