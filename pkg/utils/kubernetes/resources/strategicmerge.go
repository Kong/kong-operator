package resources

import (
	"fmt"

	"github.com/goccy/go-json"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	pkgapiscorev1 "k8s.io/kubernetes/pkg/apis/core/v1"

	"github.com/kong/kong-operator/pkg/consts"
)

// findVolume returns the corev1.Volume of the given name, inside a Pod
func findVolume(name string, base *corev1.PodTemplateSpec) *corev1.Volume {
	for _, vol := range base.Spec.Volumes {
		if vol.Name == name {
			return &vol
		}
	}

	return nil
}

// StrategicMergePatchPodTemplateSpec adds patches to base using a strategic merge patch and
// iterating by container name, failing on the first error
func StrategicMergePatchPodTemplateSpec(base, patch *corev1.PodTemplateSpec) (*corev1.PodTemplateSpec, error) {
	if patch == nil {
		return base, nil
	}

	baseBytes, err := json.Marshal(base)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON for base %s: %w", base.Name, err)
	}

	// HACK: If the cluster cert or Admission WebHook Volume is present, push it into the front of the
	//       patch's Volumes slice.
	//       This avoids errors like "may not specify more than 1 volume type".
	clusterCertVolume := findVolume(consts.ClusterCertificateVolume, base)
	if clusterCertVolume != nil {
		patch.Spec.Volumes = append([]corev1.Volume{*clusterCertVolume}, patch.Spec.Volumes...)
	}
	admissionWebhookVolume := findVolume(consts.ControlPlaneAdmissionWebhookVolumeName, base)
	if admissionWebhookVolume != nil {
		patch.Spec.Volumes = append([]corev1.Volume{*admissionWebhookVolume}, patch.Spec.Volumes...)
	}

	SetDefaultsPodTemplateSpec(patch)
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON for patch %s: %w", patch.Name, err)
	}

	// Calculate the patch result.
	jsonResultBytes, err := strategicpatch.StrategicMergePatch(baseBytes, patchBytes, &corev1.PodTemplateSpec{})
	if err != nil {
		return nil, fmt.Errorf("failed to generate merge patch for %s: %w", base.Name, err)
	}

	patchResult := base.DeepCopy()
	if err := json.Unmarshal(jsonResultBytes, patchResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal merged %s: %w", base.Name, err)
	}

	return patchResult, nil
}

// SetDefaultsPodTemplateSpec sets defaults in the provided PodTemplateSpec.
// This is useful for setting defaults in patches, where the defaults are not
// applied and we end up with structs that are filled with "zero default values".
//
// The reason for this is that native Kubernetes structs (e.g. `Pod`) define their default values
// in comments and are applied in the SetDefaults_* functions.
// To prevent situations where users use fields from the PodTemplateSpec which imply
// usage of other fields which do not have zero values as defaults (e.g. probe timeouts
// or SecretVolumeSource default mode) we need to apply the defaults to the patch.
func SetDefaultsPodTemplateSpec(pts *corev1.PodTemplateSpec) {
	if pts == nil {
		return
	}

	// NOTE: copy the service account name to the deprecated field as the
	// API server does that itself.
	pts.Spec.DeprecatedServiceAccount = pts.Spec.ServiceAccountName

	pkgapiscorev1.SetDefaults_PodSpec(&pts.Spec)
	for i := range pts.Spec.Volumes {
		SetDefaultsVolume(&pts.Spec.Volumes[i])
	}
	for i := range pts.Spec.InitContainers {
		SetDefaultsContainer(&pts.Spec.InitContainers[i])
	}
	for i := range pts.Spec.Containers {
		SetDefaultsContainer(&pts.Spec.Containers[i])
	}
}

// SetDefaultsVolume sets defaults in the provided Volume.
func SetDefaultsVolume(v *corev1.Volume) {
	if v.Secret != nil {
		pkgapiscorev1.SetDefaults_SecretVolumeSource(v.Secret)
	}
	if v.ConfigMap != nil {
		pkgapiscorev1.SetDefaults_ConfigMapVolumeSource(v.ConfigMap)
	}
	if v.DownwardAPI != nil {
		pkgapiscorev1.SetDefaults_DownwardAPIVolumeSource(v.DownwardAPI)
	}
	if v.Projected != nil {
		pkgapiscorev1.SetDefaults_ProjectedVolumeSource(v.Projected)
	}

	// NOTE: We don't fill in the default for the volume entries that are defined
	// in PodTemplateSpec patch only for the purpose of keeping the order of entries
	// and not mixing their values when calling strategicpatch.StrategicMergePatch.
	// Without this we get errors like:
	// [spec.template.spec.volumes[0].secret: Forbidden: may not specify more than 1 volume type
	//
	// That's because we need the entries to match but we don't cross inspect base patch
	// to verify which entries are in base to know which to ignore. Hence removing
	// this if condition would yield the code to fill in the defaults for volumes that
	// already have their type (by allocating field in Volume struct).
	//
	// This is the only default volume that we include for both ControlPlanes
	// and DataPlanes so we're good for now.
	//
	// TODO: https://github.com/kong/kong-operator/issues/150
	if v.Name != consts.ClusterCertificateVolume && v.Name != consts.ControlPlaneAdmissionWebhookVolumeName {
		pkgapiscorev1.SetDefaults_Volume(v)
		if v.HostPath != nil {
			pkgapiscorev1.SetDefaults_HostPathVolumeSource(v.HostPath)
		}
		if v.Secret != nil {
			pkgapiscorev1.SetDefaults_SecretVolumeSource(v.Secret)
		}
		if v.Projected != nil {
			pkgapiscorev1.SetDefaults_ProjectedVolumeSource(v.Projected)
		}
		if v.ConfigMap != nil {
			pkgapiscorev1.SetDefaults_ConfigMapVolumeSource(v.ConfigMap)
		}
		if v.DownwardAPI != nil {
			pkgapiscorev1.SetDefaults_DownwardAPIVolumeSource(v.DownwardAPI)
		}
	}
}

var _quantityOne = resource.MustParse("1")

// SetDefaultsContainer sets defaults in the provided Container.
func SetDefaultsContainer(c *corev1.Container) {
	pkgapiscorev1.SetDefaults_Container(c)
	if lp := c.LivenessProbe; lp != nil {
		pkgapiscorev1.SetDefaults_Probe(lp)
		if lp.HTTPGet != nil {
			pkgapiscorev1.SetDefaults_HTTPGetAction(lp.HTTPGet)
		}
	}
	if sp := c.ReadinessProbe; sp != nil {
		pkgapiscorev1.SetDefaults_Probe(sp)
		if sp.HTTPGet != nil {
			pkgapiscorev1.SetDefaults_HTTPGetAction(sp.HTTPGet)
		}
	}
	if ss := c.StartupProbe; ss != nil {
		pkgapiscorev1.SetDefaults_Probe(ss)
		if ss.HTTPGet != nil {
			pkgapiscorev1.SetDefaults_HTTPGetAction(ss.HTTPGet)
		}
	}

	for _, e := range c.Env {
		if e.ValueFrom != nil {
			if e.ValueFrom.FieldRef != nil {
				pkgapiscorev1.SetDefaults_ObjectFieldSelector(e.ValueFrom.FieldRef)
			}

			if e.ValueFrom.ResourceFieldRef != nil {
				// NOTE: Divisor defaults to 1 but doesn't have a SetDefaults function.
				// Ensure that the divisor is set to 1 if it's not set.
				if e.ValueFrom.ResourceFieldRef.Divisor.IsZero() {
					e.ValueFrom.ResourceFieldRef.Divisor = _quantityOne
				}
			}
		}
	}
}
