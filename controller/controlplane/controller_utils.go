package controlplane

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	operatorerrors "github.com/kong/kong-operator/internal/errors"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

// GetDataPlaneForControlPlane retrieves the DataPlane object referenced by a ControlPlane.
func GetDataPlaneForControlPlane(
	ctx context.Context,
	c client.Client,
	cp *ControlPlane,
) (*operatorv1beta1.DataPlane, error) {
	var dp operatorv1beta1.DataPlane

	switch cp.Spec.DataPlane.Type {
	case gwtypes.ControlPlaneDataPlaneTargetRefType:
		if cp.Spec.DataPlane.Ref == nil || cp.Spec.DataPlane.Ref.Name == "" {
			return nil, fmt.Errorf("%w, controlplane = %s/%s", operatorerrors.ErrDataPlaneNotSet, cp.Namespace, cp.Name)
		}

		nn := types.NamespacedName{
			Namespace: cp.Namespace,
			Name:      cp.Spec.DataPlane.Ref.Name,
		}
		if err := c.Get(ctx, nn, &dp); err != nil {
			return nil, err
		}
		return &dp, nil

	case gwtypes.ControlPlaneDataPlaneTargetManagedByType:
		if cp.Status.DataPlane == nil {
			return nil, fmt.Errorf("ControlPlane's DataPlane is not set")
		}

		nn := types.NamespacedName{
			Namespace: cp.Namespace,
			Name:      cp.Status.DataPlane.Name,
		}
		if err := c.Get(ctx, nn, &dp); err != nil {
			return nil, err
		}
		return &dp, nil

	default:
		return nil, fmt.Errorf("unsupported ControlPlane's DataPlane type: %s", cp.Spec.DataPlane.Type)
	}
}
