package konnect

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/samber/lo"
	"github.com/samber/mo"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"

	"github.com/kong/kong-operator/controller/konnect/constraints"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/controller/pkg/patch"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

func handleKonnectNetworkRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
	sdk sdkops.SDKWrapper,
) (ctrl.Result, error) {
	networkRefs, ok := getKonnectNetworkRefs(ent).Get()
	if !ok {
		return ctrl.Result{}, nil
	}

	setInvalidWithMsg := func(msg string) {
		patch.SetStatusWithConditionIfDifferent(
			ent,
			konnectv1alpha1.KonnectNetworkRefsValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KonnectNetworkRefsReasonInvalid,
			msg,
		)
	}

	var networkID string
	for _, ref := range networkRefs {
		switch ref.Type {
		case commonv1alpha1.ObjectRefTypeNamespacedRef:
			network := konnectv1alpha1.KonnectCloudGatewayNetwork{}
			nn := types.NamespacedName{
				Name: ref.NamespacedRef.Name,
				// TODO: handle cross namespace refs
				Namespace: ent.GetNamespace(),
			}

			err := cl.Get(ctx, nn, &network)
			if err != nil {
				setInvalidWithMsg(err.Error())
				if k8serrors.IsNotFound(err) {
					return ctrl.Result{}, ReferencedObjectDoesNotExist{
						Reference: nn,
						Err:       err,
					}
				}
				return ctrl.Result{}, fmt.Errorf("failed to get network ref %q: %w", nn, err)
			}

			if delTimestamp := network.GetDeletionTimestamp(); !delTimestamp.IsZero() {
				return ctrl.Result{}, ReferencedObjectIsBeingDeleted{
					Reference:         nn,
					DeletionTimestamp: delTimestamp.Time,
				}
			}

			// requeue it if referenced network is not programmed yet so we cannot do the following work.
			cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, &network)
			if !ok || cond.Status != metav1.ConditionTrue {
				setInvalidWithMsg(fmt.Sprintf("Referenced KonnectCloudGatewayNetwork %s is not programmed yet", nn))
				return ctrl.Result{}, ReferencedObjectIsInvalid{
					Reference: nn.String(),
					Msg:       "Referenced KonnectCloudGatewayNetwork is not programmed yet",
				}
			}

			if network.Status.State != string(sdkkonnectcomp.NetworkStateReady) {
				nn := client.ObjectKeyFromObject(&network)
				msg := fmt.Sprintf("Referenced KonnectCloudGatewayNetwork %s: is not ready yet, current state: %s", nn, network.Status.State)
				setInvalidWithMsg(msg)
				return ctrl.Result{}, ReferencedObjectIsInvalid{
					Reference: nn.String(),
					Msg:       msg,
				}
			}
			networkID = network.GetKonnectID()

		case commonv1alpha1.ObjectRefTypeKonnectID:
			n, err := sdk.GetCloudGatewaysSDK().GetNetwork(ctx, *ref.KonnectID)
			if err != nil {
				msg := fmt.Sprintf("Could not get the referenced KonnectCloudGatewayNetwork <konnectID:%s>: %v", *ref.KonnectID, err)
				setInvalidWithMsg(msg)
				return ctrl.Result{}, ReferencedObjectIsInvalid{
					Reference: *ref.KonnectID,
					Msg:       msg,
				}
			}
			if n.Network.State != sdkkonnectcomp.NetworkStateReady {
				msg := fmt.Sprintf("Referenced KonnectCloudGatewayNetwork <konnectID:%s>: is not ready yet, current state: %s", *ref.KonnectID, n.Network.State)
				setInvalidWithMsg(msg)
				return ctrl.Result{}, ReferencedObjectIsInvalid{
					Reference: *ref.KonnectID,
					Msg:       msg,
				}
			}
			networkID = *ref.KonnectID

		default:
			return ctrl.Result{}, fmt.Errorf("unsupported network ref type: %s", ref.Type)
		}
	}

	old := ent.DeepCopyObject().(TEnt)

	// Set status.networkID for KonnectCloudGatewayTransitGateway because it is required for creating transit gateway on Konnect.
	if tg, ok := any(ent).(*konnectv1alpha1.KonnectCloudGatewayTransitGateway); ok {
		tg.SetNetworkID(networkID)
	}
	if patch.SetStatusWithConditionIfDifferent(
		ent,
		konnectv1alpha1.KonnectNetworkRefsValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.KonnectNetworkRefsReasonValid,
		"Referenced KonnectCloudGatewayNetwork(s) are valid and programmed",
	) {
		if err := cl.Status().Patch(ctx, ent, client.MergeFrom(old)); err != nil {
			if k8serrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed to patch status with network refs valid condition: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

func getKonnectNetworkRefs[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) mo.Option[[]commonv1alpha1.ObjectRef] {
	switch e := any(e).(type) {
	case *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration:
		m := lo.Map(e.Spec.DataplaneGroups,
			func(g konnectv1alpha1.KonnectConfigurationDataPlaneGroup, _ int) commonv1alpha1.ObjectRef {
				return g.NetworkRef
			},
		)
		return mo.Some(m)
	case *konnectv1alpha1.KonnectCloudGatewayTransitGateway:
		return mo.Some([]commonv1alpha1.ObjectRef{e.Spec.NetworkRef})
	default:
		return mo.None[[]commonv1alpha1.ObjectRef]()
	}
}
