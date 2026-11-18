package resources

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// KongConfWatchConfigMapName returns the name of the ConfigMap holding the
// watched kong.conf for the given DataPlane.
//
// The name is derived from the DataPlane name so that it is stable across
// reconciliations: the Pod template references the ConfigMap by name, and a name
// that changed with the contents would change the Pod template and cause the
// rollout this feature exists to avoid.
func KongConfWatchConfigMapName(dataplaneName string) string {
	name := dataplaneName + consts.KongConfWatchConfigMapNameSuffix
	if len(name) <= validation.DNS1123SubdomainMaxLength {
		return name
	}
	// Truncate the DataPlane name, never the suffix, so the result stays
	// recognizable and keeps the suffix the lookup relies on.
	keep := validation.DNS1123SubdomainMaxLength - len(consts.KongConfWatchConfigMapNameSuffix)
	return dataplaneName[:keep] + consts.KongConfWatchConfigMapNameSuffix
}

// GenerateKongConfWatchConfigMapForDataPlane generates the ConfigMap holding the
// kong.conf watched by the DataPlane's Kong Gateway container.
// configMapLabelSelector is the operator's --configmap-label-selector. It must
// be set on the generated ConfigMap, because the manager's cache only ingests
// ConfigMaps carrying that label: without it the operator cannot read back the
// ConfigMap it just wrote.
func GenerateKongConfWatchConfigMapForDataPlane(
	dataplane *operatorv1beta1.DataPlane,
	contents string,
	configMapLabelSelector string,
) *corev1.ConfigMap {
	labels := GetManagedLabelForOwner(dataplane)
	labels[consts.DataPlaneConfigMapTypeLabel] = consts.DataPlaneConfigMapTypeKongConfWatch
	if configMapLabelSelector != "" {
		labels[configMapLabelSelector] = "true"
	}

	cm := &corev1.ConfigMap{
		Name:      KongConfWatchConfigMapName(dataplane.Name),
		Namespace: dataplane.Namespace,
		Labels:    labels,
		Data: map[string]string{
			consts.KongConfWatchFileName: contents,
		},
	}

	k8sutils.SetOwnerForObject(cm, dataplane)

	return cm
}

// KongConfWatchVolume returns the volume holding the watched kong.conf, backed
// by the given ConfigMap.
func KongConfWatchVolume(configMapName string) corev1.Volume {
	volume := corev1.Volume{
		ConfigMap: &corev1.ConfigMapVolumeSource{},
	}
	SetDefaultsVolume(&volume)
	volume.Name = consts.KongConfWatchVolume
	volume.ConfigMap = &corev1.ConfigMapVolumeSource{
		Name: configMapName,
		Items: []corev1.KeyToPath{
			{
				Key:  consts.KongConfWatchFileName,
				Path: consts.KongConfWatchFileName,
			},
		},
	}
	return volume
}

// KongConfWatchVolumeMount returns the volume mount for the watched kong.conf.
//
// NOTE: the volume is deliberately mounted as a directory and without SubPath.
// Kubernetes does not update a file mounted with SubPath, so the in-container
// watcher would never see a change and no reload would ever happen.
func KongConfWatchVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.KongConfWatchVolume,
		ReadOnly:  true,
		MountPath: consts.KongConfWatchVolumeMountPath,
	}
}
