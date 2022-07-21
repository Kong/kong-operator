package gateway

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
)

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

	dataplaneList := &operatorv1alpha1.DataPlaneList{}

	err := c.List(
		ctx,
		dataplaneList,
		client.InNamespace(gateway.Namespace),
		client.MatchingLabels{consts.GatewayOperatorControlledLabel: consts.GatewayManagedLabelValue},
	)

	if err != nil {
		return nil, err
	}

	dataplanes := make([]operatorv1alpha1.DataPlane, 0)
	for _, dataplane := range dataplaneList.Items {
		if k8sutils.IsOwnedByRefUID(&dataplane.ObjectMeta, gateway.UID) {
			dataplanes = append(dataplanes, dataplane)
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

	controlplaneList := &operatorv1alpha1.ControlPlaneList{}

	err := c.List(
		ctx,
		controlplaneList,
		client.InNamespace(gateway.Namespace),
		client.MatchingLabels{consts.GatewayOperatorControlledLabel: consts.GatewayManagedLabelValue},
	)
	if err != nil {
		return nil, err
	}

	controlplanes := make([]operatorv1alpha1.ControlPlane, 0)
	for _, controlplane := range controlplaneList.Items {
		if k8sutils.IsOwnedByRefUID(&controlplane.ObjectMeta, gateway.UID) {
			controlplanes = append(controlplanes, controlplane)
		}
	}

	return controlplanes, nil
}

// GetDataplaneServiceNameForControlplane is a helper functions that retrieves
// the name of the service owned by dataplane associated to the controlplane
func GetDataplaneServiceNameForControlplane(
	ctx context.Context,
	c client.Client,
	controlplane *operatorv1alpha1.ControlPlane,
) (string, error) {
	if controlplane.Spec.DataPlane == nil || *controlplane.Spec.DataPlane == "" {
		return "", fmt.Errorf("%w, controlplane = %s/%s", operatorerrors.ErrDataPlaneNotSet, controlplane.Namespace, controlplane.Name)
	}

	dataplane := operatorv1alpha1.DataPlane{}
	dataplaneName := *controlplane.Spec.DataPlane
	if err := c.Get(ctx, types.NamespacedName{Namespace: controlplane.Namespace, Name: dataplaneName}, &dataplane); err != nil {
		return "", err
	}

	services, err := k8sutils.ListServicesForOwner(ctx,
		c,
		consts.GatewayOperatorControlledLabel,
		consts.DataPlaneManagedLabelValue,
		dataplane.Namespace,
		dataplane.UID,
	)
	if err != nil {
		return "", err
	}

	count := len(services)
	if count > 1 {
		return "", fmt.Errorf("found %d services for DataPlane currently unsupported: expected 1 or less", count)
	}

	if count == 0 {
		return "", fmt.Errorf("found 0 services for DataPlane")
	}

	return services[0].Name, nil
}
