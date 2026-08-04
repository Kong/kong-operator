package resources

import (
	"fmt"
	"reflect"

	"github.com/goccy/go-json"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	pkgapiscorev1 "k8s.io/kubernetes/pkg/apis/core/v1"

	"github.com/kong/kong-operator/v2/pkg/consts"
)

// StrategicMergePatchPodTemplateSpec adds patches to base using a strategic merge patch and
// iterating by container name, failing on the first error.
func StrategicMergePatchPodTemplateSpec(base, patch *corev1.PodTemplateSpec) (*corev1.PodTemplateSpec, error) {
	if patch == nil {
		return base, nil
	}
	// NOTE: the caller may pass a pointer into a live object (e.g. the DataPlane's
	// spec.deployment.podTemplateSpec), and both extractProbeDeletes and
	// SetDefaultsPodTemplateSpec below mutate the patch in place. Copy so the caller's
	// object is left untouched - otherwise the `{}` probe delete sentinel is erased
	// before the DataPlane spec hash is computed and the delete is silently skipped
	// when running with --enforce-config=false.
	patch = patch.DeepCopy()

	baseBytes, err := json.Marshal(base)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON for base %s: %w", base.Name, err)
	}

	// NOTE: an explicitly empty probe (e.g. `readinessProbe: {}`) signals that the
	// corresponding probe should be removed from the base. A nil probe can't be used
	// for this because it's indistinguishable from an unset one once marshaled to JSON
	// (both are omitted), so CreateTwoWayMergePatch would never emit a delete directive
	// for it. We detect the empty-probe sentinel before defaulting/diffing, strip it so
	// it doesn't get defaulted into a bogus non-empty probe, and re-inject it as an
	// explicit `null` into the computed merge patch below.
	containerProbeDeletes := extractProbeDeletes(patch.Spec.Containers)
	initContainerProbeDeletes := extractProbeDeletes(patch.Spec.InitContainers)

	SetDefaultsPodTemplateSpec(patch)
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON for patch %s: %w", patch.Name, err)
	}
	defaultPatchBase := &corev1.PodTemplateSpec{}
	SetDefaultsPodTemplateSpec(defaultPatchBase)
	defaultPatchBaseBytes, err := json.Marshal(defaultPatchBase)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON for default patch base: %w", err)
	}
	mergePatchBytes, err := strategicpatch.CreateTwoWayMergePatch(defaultPatchBaseBytes, patchBytes, &corev1.PodTemplateSpec{})
	if err != nil {
		return nil, fmt.Errorf("failed to create merge patch for %s: %w", patch.Name, err)
	}
	mergePatchBytes, err = injectProbeDeletes(mergePatchBytes, "containers", containerProbeDeletes)
	if err != nil {
		return nil, fmt.Errorf("failed to inject probe deletes for %s: %w", patch.Name, err)
	}
	mergePatchBytes, err = injectProbeDeletes(mergePatchBytes, "initContainers", initContainerProbeDeletes)
	if err != nil {
		return nil, fmt.Errorf("failed to inject probe deletes for %s: %w", patch.Name, err)
	}

	// Calculate the patch result.
	jsonResultBytes, err := strategicpatch.StrategicMergePatch(baseBytes, mergePatchBytes, &corev1.PodTemplateSpec{})
	if err != nil {
		return nil, fmt.Errorf("failed to generate merge patch for %s: %w", base.Name, err)
	}

	patchResult := &corev1.PodTemplateSpec{}
	if err := json.Unmarshal(jsonResultBytes, patchResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal merged %s: %w", base.Name, err)
	}

	return patchResult, nil
}

// extractProbeDeletes scans containers for probes explicitly set to an empty
// struct (e.g. `readinessProbe: {}`), which signals that the probe should be
// removed from the base rather than merged. Matching probes are nilled out in
// place (so SetDefaultsPodTemplateSpec doesn't turn them into a bogus non-empty
// probe) and returned as a map of container name to the JSON field names of
// the probes to delete, for use with injectProbeDeletes.
func extractProbeDeletes(containers []corev1.Container) map[string][]string {
	var deletes map[string][]string
	for i := range containers {
		c := &containers[i]
		var fields []string
		if c.ReadinessProbe != nil && reflect.DeepEqual(*c.ReadinessProbe, corev1.Probe{}) {
			fields = append(fields, "readinessProbe")
			c.ReadinessProbe = nil
		}
		if c.LivenessProbe != nil && reflect.DeepEqual(*c.LivenessProbe, corev1.Probe{}) {
			fields = append(fields, "livenessProbe")
			c.LivenessProbe = nil
		}
		if c.StartupProbe != nil && reflect.DeepEqual(*c.StartupProbe, corev1.Probe{}) {
			fields = append(fields, "startupProbe")
			c.StartupProbe = nil
		}
		if len(fields) > 0 {
			if deletes == nil {
				deletes = map[string][]string{}
			}
			deletes[c.Name] = fields
		}
	}
	return deletes
}

// injectProbeDeletes adds explicit `null` entries for the probes recorded by
// extractProbeDeletes into a computed strategic merge patch, keyed by container
// name under the given container list field ("containers" or "initContainers").
// This is necessary because the merge patch computed by CreateTwoWayMergePatch
// never contains a delete directive for a probe that was nilled out beforehand -
// omitting a field from a patch means "leave it alone", not "remove it".
func injectProbeDeletes(mergePatch []byte, listField string, deletes map[string][]string) ([]byte, error) {
	if len(deletes) == 0 {
		return mergePatch, nil
	}

	var patch map[string]any
	if err := json.Unmarshal(mergePatch, &patch); err != nil {
		return nil, fmt.Errorf("failed to unmarshal merge patch: %w", err)
	}

	spec, _ := patch["spec"].(map[string]any)
	containers, _ := spec[listField].([]any)
	// NOTE: iterate the patch's containers rather than the deletes map so the output
	// order is deterministic (Go map iteration order is randomized) and so a name that
	// isn't in the patch can't inject a bogus image-less container. deletes is derived
	// from the same container list the merge patch is computed from, so every name is
	// present.
	for _, c := range containers {
		entry, ok := c.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		for _, field := range deletes[name] {
			entry[field] = nil
		}
	}

	result, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merge patch: %w", err)
	}
	return result, nil
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
	if v.Name != consts.ClusterCertificateVolume {
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

	for i := range c.Env {
		if c.Env[i].ValueFrom != nil {
			if c.Env[i].ValueFrom.FieldRef != nil {
				pkgapiscorev1.SetDefaults_ObjectFieldSelector(c.Env[i].ValueFrom.FieldRef)
			}

			if c.Env[i].ValueFrom.ResourceFieldRef != nil {
				// NOTE: Divisor defaults to 1 but doesn't have a SetDefaults function.
				// Ensure that the divisor is set to 1 if it's not set.
				if c.Env[i].ValueFrom.ResourceFieldRef.Divisor.IsZero() {
					c.Env[i].ValueFrom.ResourceFieldRef.Divisor = _quantityOne
				}
			}
		}
	}
}
