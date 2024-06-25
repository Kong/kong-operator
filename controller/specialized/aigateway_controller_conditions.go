package specialized

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/api/gateway-operator/v1alpha1"
)

// ----------------------------------------------------------------------------
// AIGatewayReconciler - Conditions
// ----------------------------------------------------------------------------

// newAIGatewayAcceptedCondition returns a new Accepted condition for the
// AIGateway resource to indicate to the user that the controller is
// accepting responsibility for the resource and will process it.
func newAIGatewayAcceptedCondition(obj client.Object) metav1.Condition {
	return metav1.Condition{
		Type:               v1alpha1.AIGatewayConditionTypeAccepted,
		Status:             metav1.ConditionTrue,
		Reason:             "Accepted",
		Message:            "resource accepted by the controller",
		ObservedGeneration: obj.GetGeneration(),
		LastTransitionTime: metav1.Now(),
	}
}
