package gateway

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/pkg/consts"
)

// -----------------------------------------------------------------------------
// Gateway Utils - Labels
// -----------------------------------------------------------------------------

// LabelObjectAsGatewayManaged ensures that labels are set on the provided
// object to signal that it's owned by a Gateway resource and that its
// lifecycle is managed by this operator. gatewayName is the name of the
// owning Gateway and is set as the GEP-1762 gateway-name label.
func LabelObjectAsGatewayManaged(obj client.Object, gatewayName string) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[consts.GatewayOperatorManagedByLabel] = consts.GatewayManagedLabelValue
	labels[consts.GatewayNameLabel] = gatewayName
	obj.SetLabels(labels)
}
