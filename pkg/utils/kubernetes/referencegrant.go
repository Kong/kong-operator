package kubernetes

import (
	"context"

	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// AllowedByReferenceGrants checks if the reference from the input `from` to the object(s)
// in namespace `targetNamespace` with group, kind, name given in the input `to` is allowed by any ReferenceGrant.
func AllowedByReferenceGrants(
	ctx context.Context,
	cl client.Client,
	from gatewayv1beta1.ReferenceGrantFrom,
	targetNamespace string,
	to gatewayv1beta1.ReferenceGrantTo,
) (bool, error) {
	// Same namespace is always allowed.
	if from.Namespace == gatewayv1beta1.Namespace(targetNamespace) {
		return true, nil
	}
	referenceGrantList := gatewayv1beta1.ReferenceGrantList{}
	namespacedClient := client.NewNamespacedClient(cl, targetNamespace)
	err := namespacedClient.List(
		ctx,
		&referenceGrantList,
	)
	if err != nil {
		return false, err
	}
	for _, referenceGrant := range referenceGrantList.Items {
		// If the `spec.from` does not contain the input `from`, we skip the ReferenceGrant
		// becuase it is impossible to grant the reference to the input `from`.
		if !lo.ContainsBy(referenceGrant.Spec.From, func(refGrantFrom gatewayv1beta1.ReferenceGrantFrom) bool {
			return isSameGroup(refGrantFrom.Group, from.Group) &&
				refGrantFrom.Kind == from.Kind &&
				refGrantFrom.Namespace == from.Namespace
		}) {
			continue
		}
		// If the ReferenceGrant contains the input `from` in `spec.from`, and contains the referenced target in `to`,
		// we return true because it allows the reference.
		if lo.ContainsBy(referenceGrant.Spec.To, func(refGrantTo gatewayv1beta1.ReferenceGrantTo) bool {
			return isSameGroup(refGrantTo.Group, to.Group) &&
				refGrantTo.Kind == to.Kind &&
				(refGrantTo.Name == nil || (to.Name != nil && *refGrantTo.Name == *to.Name))
		}) {
			return true, nil
		}
	}
	// If we did not find one ReferenceGrant that allows the reference, return false.
	return false, nil
}

func isSameGroup(group1, group2 gatewayv1beta1.Group) bool {
	if group1 == gatewayv1beta1.Group("core") {
		group1 = gatewayv1beta1.Group("")
	}
	if group2 == gatewayv1beta1.Group("core") {
		group2 = gatewayv1beta1.Group("")
	}
	return group1 == group2
}
