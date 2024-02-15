package specialized

import (
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
)

// -----------------------------------------------------------------------------
// AIGatewayReconciler - Status Management
// -----------------------------------------------------------------------------

func (r *AIGatewayReconciler) manageStatus() (
	bool, // whether any changes were made
	ctrl.Result,
	error,
) {
	// TODO: https://github.com/Kong/gateway-operator/issues/1429
	return false, ctrl.Result{}, fmt.Errorf("FIXME: implement me")
}
