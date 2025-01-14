package gatewayclass

import (
	"context"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
)

// getAcceptedCondition returns the accepted condition for the GatewayClass, with
// the proper status, reason and message.
func getAcceptedCondition(ctx context.Context, cl client.Client, gwc *gatewayv1.GatewayClass) (*metav1.Condition, error) {
	reason := string(gatewayv1.GatewayClassReasonAccepted)
	message := []string{}
	status := metav1.ConditionFalse

	if gwc.Spec.ParametersRef != nil {
		validRef := true
		if gwc.Spec.ParametersRef.Group != gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group) ||
			gwc.Spec.ParametersRef.Kind != "GatewayConfiguration" {
			reason = string(gatewayv1.GatewayClassReasonInvalidParameters)
			message = append(message, "ParametersRef must reference a gateway-operator.konghq.com/GatewayConfiguration")
			validRef = false
		}

		if gwc.Spec.ParametersRef.Namespace == nil {
			reason = string(gatewayv1.GatewayClassReasonInvalidParameters)
			message = append(message, "ParametersRef must reference a namespaced resource")
			validRef = false
		}

		if validRef {
			gatewayConfig := operatorv1beta1.GatewayConfiguration{}
			err := cl.Get(ctx, client.ObjectKey{Name: gwc.Spec.ParametersRef.Name, Namespace: string(*gwc.Spec.ParametersRef.Namespace)}, &gatewayConfig)
			if client.IgnoreNotFound(err) != nil {
				return nil, err
			}
			if k8serrors.IsNotFound(err) {
				reason = string(gatewayv1.GatewayClassReasonInvalidParameters)
				message = append(message, "The referenced GatewayConfiguration does not exist")
			}
		}
	}
	if reason == string(gatewayv1.GatewayClassReasonAccepted) {
		status = metav1.ConditionTrue
		message = []string{"GatewayClass is accepted"}
	}

	acceptedCondition := k8sutils.NewConditionWithGeneration(
		consts.ConditionType(gatewayv1.GatewayClassConditionStatusAccepted),
		status,
		consts.ConditionReason(reason),
		strings.Join(message, ". "),
		gwc.GetGeneration(),
	)

	return &acceptedCondition, nil
}
