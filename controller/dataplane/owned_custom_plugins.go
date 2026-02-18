package dataplane

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kong/kong-operator/v2/internal/utils/config"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

type customPlugin struct {
	// Name of the KongPluginInstallation resource.
	Name string
	// ConfigMapNN is the namespace/name of the ConfigMap that contains the plugin.
	ConfigMapNN types.NamespacedName
	// Generation is the generation of the KongPluginInstallation that contains the plugin.
	Generation int64
}

func withCustomPlugins(customPlugins ...customPlugin) k8sresources.DeploymentOpt {
	// Noop/cleanup operation that is safe to execute if no plugins are provided.
	if len(customPlugins) == 0 {
		return func(d *appsv1.Deployment) {
			// It's safe to perform a delete operation on a nil map.
			delete(
				d.Spec.Template.Annotations,
				consts.AnnotationKongPluginInstallationGenerationInternal,
			)
		}
	}

	var (
		kpisNames        = make([]string, 0, len(customPlugins))
		kpisGenerations  = make([]string, 0, len(customPlugins))
		kpisVolumeMounts = make([]corev1.VolumeMount, 0, len(customPlugins))
		kpisVolumes      = make([]corev1.Volume, 0, len(customPlugins))
	)

	for _, cp := range customPlugins {
		kpisNames = append(kpisNames, cp.Name)
		kpisGenerations = append(kpisGenerations, fmt.Sprintf("%s:%d", cp.Name, cp.Generation))
		kpisVolumeMounts = append(kpisVolumeMounts, corev1.VolumeMount{
			Name:      cp.Name,
			MountPath: "/opt/kong/plugins/" + cp.Name,
		})
		kpisVolumes = append(kpisVolumes, corev1.Volume{
			Name: cp.Name,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cp.ConfigMapNN.Name,
					},
				},
			},
		})
	}

	return func(deployment *appsv1.Deployment) {
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = make(map[string]string)
		}
		deployment.Spec.Template.Annotations[consts.AnnotationKongPluginInstallationGenerationInternal] = strings.Join(kpisGenerations, ",")
		deployment.Spec.Template.Spec.Containers[0].Env = append(
			deployment.Spec.Template.Spec.Containers[0].Env,
			config.ConfigureKongPluginRelatedEnvVars(kpisNames)...,
		)
		deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(
			deployment.Spec.Template.Spec.Containers[0].VolumeMounts,
			kpisVolumeMounts...,
		)
		deployment.Spec.Template.Spec.Volumes = append(
			deployment.Spec.Template.Spec.Volumes,
			kpisVolumes...,
		)
	}
}
