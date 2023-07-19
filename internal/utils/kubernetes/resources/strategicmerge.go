package resources

import (
	"fmt"

	"github.com/goccy/go-json"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

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
