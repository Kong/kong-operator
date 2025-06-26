package gateway

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/pkg/consts"
)

// -----------------------------------------------------------------------------
// Gateway Utils - Labels
// -----------------------------------------------------------------------------

// LabelObjectAsGatewayManaged ensures that labels are set on the provided
// object to signal that it's owned by a Gateway resource and that it's
// lifecycle is managed by this operator.
func LabelObjectAsGatewayManaged(obj client.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[consts.GatewayOperatorManagedByLabel] = consts.GatewayManagedLabelValue
	obj.SetLabels(labels)
}
