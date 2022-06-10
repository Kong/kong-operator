package gateway

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
)

// -----------------------------------------------------------------------------
// Gateway Utils - Public Functions - Status Updates
// -----------------------------------------------------------------------------

// IsGatewayScheduled indicates whether or not the provided Gateway object was
// marked as scheduled by the controller.
func IsGatewayScheduled(gateway *gatewayv1alpha2.Gateway) bool {
	for _, cond := range gateway.Status.Conditions {
		if cond.Type == string(gatewayv1alpha2.GatewayConditionScheduled) &&
			cond.Reason == string(gatewayv1alpha2.GatewayReasonScheduled) &&
			cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// IsGatewayReady indicates whether or not the provided Gateway object was
// marked as ready by the controller.
func IsGatewayReady(gateway *gatewayv1alpha2.Gateway) bool {
	for _, cond := range gateway.Status.Conditions {
		if cond.Type == string(gatewayv1alpha2.GatewayConditionReady) &&
			cond.Reason == string(gatewayv1alpha2.GatewayReasonReady) &&
			cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// Gateway Utils - Public Functions - Owner References
// -----------------------------------------------------------------------------

// ListDataPlanesForGateway is a helper function to map a list of DataPlanes
// that are owned and managed by a Gateway.
func ListDataPlanesForGateway(
	ctx context.Context,
	c client.Client,
	gateway *gatewayv1alpha2.Gateway,
) ([]operatorv1alpha1.DataPlane, error) {
	if gateway.Namespace == "" {
		return nil, fmt.Errorf("can't list dataplanes for gateway: gateway resource was missing namespace")
	}

	requirement, err := labels.NewRequirement(
		consts.GatewayOperatorControlledLabel,
		selection.Equals,
		[]string{consts.GatewayManagedLabelValue},
	)
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector().Add(*requirement)

	listOptions := &client.ListOptions{
		Namespace:     gateway.Namespace,
		LabelSelector: selector,
	}

	dataplaneList := &operatorv1alpha1.DataPlaneList{}
	if err := c.List(ctx, dataplaneList, listOptions); err != nil {
		return nil, err
	}

	dataplanes := make([]operatorv1alpha1.DataPlane, 0)
	for _, dataplane := range dataplaneList.Items {
		for _, ownerRef := range dataplane.ObjectMeta.OwnerReferences {
			if ownerRef.UID == gateway.UID {
				dataplanes = append(dataplanes, dataplane)
				break
			}
		}
	}

	return dataplanes, nil
}

// ListControlPlanesForGateway is a helper function to map a list of ControlPlanes
// that are owned and managed by a Gateway.
func ListControlPlanesForGateway(
	ctx context.Context,
	c client.Client,
	gateway *gatewayv1alpha2.Gateway,
) ([]operatorv1alpha1.ControlPlane, error) {
	if gateway.Namespace == "" {
		return nil, fmt.Errorf("can't list dataplanes for gateway: gateway resource was missing namespace")
	}

	requirement, err := labels.NewRequirement(
		consts.GatewayOperatorControlledLabel,
		selection.Equals,
		[]string{consts.GatewayManagedLabelValue},
	)
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector().Add(*requirement)

	listOptions := &client.ListOptions{
		Namespace:     gateway.Namespace,
		LabelSelector: selector,
	}

	controlplaneList := &operatorv1alpha1.ControlPlaneList{}
	if err := c.List(ctx, controlplaneList, listOptions); err != nil {
		return nil, err
	}

	controlplanes := make([]operatorv1alpha1.ControlPlane, 0)
	for _, controlplane := range controlplaneList.Items {
		for _, ownerRef := range controlplane.ObjectMeta.OwnerReferences {
			if ownerRef.UID == gateway.UID {
				controlplanes = append(controlplanes, controlplane)
				break
			}
		}
	}

	return controlplanes, nil
}
