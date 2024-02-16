package specialized

import (
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
)

// -----------------------------------------------------------------------------
// AIGatewayReconciler - Owned Resources Management
// -----------------------------------------------------------------------------

func (r *AIGatewayReconciler) manageGateway() (
	bool, // whether any changes were made
	ctrl.Result,
	error,
) {
	// TODO: https://github.com/Kong/gateway-operator/issues/1429
	return false, ctrl.Result{}, fmt.Errorf("FIXME: implement me")
}

func (r *AIGatewayReconciler) managePlugins() (
	bool, // whether any changes were made
	ctrl.Result,
	error,
) {
	// TODO: https://github.com/Kong/gateway-operator/issues/1429
	return false, ctrl.Result{}, fmt.Errorf("FIXME: implement me")
}
