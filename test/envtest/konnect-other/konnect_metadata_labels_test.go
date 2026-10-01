package konnectother

import (
	"maps"

	"k8s.io/apimachinery/pkg/types"

	"github.com/kong/kong-operator/v2/controller/konnect/ops"
)

// withoutOperatorLabels returns labels without the Kubernetes metadata labels
// the operator adds to Konnect entities (nil when none are left), and whether
// those labels identify the object with the given UID.
func withoutOperatorLabels(labels map[string]string, uid types.UID) (map[string]string, bool) {
	if uid == "" ||
		labels[ops.KubernetesUIDLabelKey] != string(uid) ||
		labels[ops.ManagedByLabelKey] != ops.ManagedByKongOperatorLabelValue {
		return nil, false
	}
	out := maps.Clone(labels)
	for _, k := range []string{
		ops.KubernetesNameLabelKey,
		ops.KubernetesNamespaceLabelKey,
		ops.KubernetesUIDLabelKey,
		ops.KubernetesGenerationLabelKey,
		ops.KubernetesKindLabelKey,
		ops.KubernetesGroupLabelKey,
		ops.KubernetesVersionLabelKey,
		ops.ManagedByLabelKey,
	} {
		delete(out, k)
	}
	if len(out) == 0 {
		return nil, true
	}
	return out, true
}
