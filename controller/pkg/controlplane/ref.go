package controlplane

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
)

// GetCPForRef returns the KonnectGatewayControlPlane for the given ControlPlaneRef.
func GetCPForRef(
	ctx context.Context,
	cl client.Client,
	cpRef commonv1alpha1.ControlPlaneRef,
	namespace string,
) (*konnectv1alpha2.KonnectGatewayControlPlane, error) {
	switch cpRef.Type {
	case commonv1alpha1.ControlPlaneRefKonnectNamespacedRef:
		return getCPForNamespacedRef(ctx, cl, cpRef, namespace)
	default:
		return nil, ReferencedKongGatewayControlPlaneIsUnsupported{Reference: cpRef}
	}
}

// GetControlPlaneRef returns the ControlPlaneRef for the given entity.
func GetControlPlaneRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) mo.Option[commonv1alpha1.ControlPlaneRef] {
	none := mo.None[commonv1alpha1.ControlPlaneRef]()
	type GetControlPlaneRef interface {
		GetControlPlaneRef() *commonv1alpha1.ControlPlaneRef
	}

	if eGetter, ok := any(e).(GetControlPlaneRef); ok {
		if cpRef := eGetter.GetControlPlaneRef(); lo.IsNotEmpty(cpRef) {
			return mo.Some(*cpRef)
		}
	}
	return none
}

func getCPForNamespacedRef(
	ctx context.Context,
	cl client.Client,
	ref commonv1alpha1.ControlPlaneRef,
	namespace string,
) (*konnectv1alpha2.KonnectGatewayControlPlane, error) {
	// TODO(pmalek): handle cross namespace refs
	if namespace != "" && ref.KonnectNamespacedRef.Namespace != "" && ref.KonnectNamespacedRef.Namespace != namespace {
		return nil, fmt.Errorf("%s ControlPlaneRef from different namespace than %s", ref.KonnectNamespacedRef.Namespace, namespace)
	}

	nn := types.NamespacedName{
		Name:      ref.KonnectNamespacedRef.Name,
		Namespace: namespace,
	}

	// Set namespace of control plane when it is non-empty. Only applies for cluster-scoped resources (KongVault).
	if namespace == "" && ref.KonnectNamespacedRef.Namespace != "" {
		nn.Namespace = ref.KonnectNamespacedRef.Namespace
	}

	var cp konnectv1alpha2.KonnectGatewayControlPlane
	if err := cl.Get(ctx, nn, &cp); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, ReferencedControlPlaneDoesNotExistError{
				Reference: ref,
				Err:       err,
			}
		}
		return nil, fmt.Errorf("failed to get ControlPlane %s: %w", nn, err)
	}
	return &cp, nil
}
