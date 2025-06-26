package controlplane

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorerrors "github.com/kong/kong-operator/internal/errors"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	operatorv2alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v2alpha1"
)

// GetDataPlaneForControlPlane retrieves the DataPlane object referenced by a ControlPlane.
func GetDataPlaneForControlPlane(
	ctx context.Context,
	c client.Client,
	cp *ControlPlane,
) (*operatorv1beta1.DataPlane, error) {
	switch cp.Spec.DataPlane.Type {
	case operatorv2alpha1.ControlPlaneDataPlaneTargetRefType:
		if cp.Spec.DataPlane.Ref == nil || cp.Spec.DataPlane.Ref.Name == "" {
			return nil, fmt.Errorf("%w, controlplane = %s/%s", operatorerrors.ErrDataPlaneNotSet, cp.Namespace, cp.Name)
		}

		dp := operatorv1beta1.DataPlane{}
		nn := types.NamespacedName{
			Namespace: cp.Namespace,
			Name:      cp.Spec.DataPlane.Ref.Name,
		}
		if err := c.Get(ctx, nn, &dp); err != nil {
			return nil, err
		}
		return &dp, nil

	// TODO(pmalek): implement DataPlane external URL type
	// ref: https://github.com/kong/kong-operator/issues/1366

	default:
		return nil, fmt.Errorf("unsupported ControlPlane's DataPlane type: %s", cp.Spec.DataPlane.Type)
	}
}
