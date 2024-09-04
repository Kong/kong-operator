package konnect

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectgocomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectgoops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/models/sdkerrors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
	"github.com/kong/kubernetes-configuration/pkg/metadata"
)

func createConsumerGroup(
	ctx context.Context,
	sdk *sdkkonnectgo.SDK,
	group *configurationv1beta1.KongConsumerGroup,
) error {
	if group.GetControlPlaneID() == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", group, client.ObjectKeyFromObject(group))
	}

	resp, err := sdk.ConsumerGroups.CreateConsumerGroup(ctx,
		group.Status.Konnect.ControlPlaneID,
		kongConsumerGroupToSDKConsumerGroupInput(group),
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrapped := wrapErrIfKonnectOpFailed(err, CreateOp, group); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToCreate",
				errWrapped.Error(),
				group.GetGeneration(),
			),
			group,
		)
		return errWrapped
	}

	group.Status.Konnect.SetKonnectID(*resp.ConsumerGroup.ID)
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			KonnectEntityProgrammedReasonProgrammed,
			"",
			group.GetGeneration(),
		),
		group,
	)

	return nil
}

// updateConsumerGroup updates a KongConsumerGroup in Konnect.
// The KongConsumerGroup is assumed to have a Konnect ID set in status.
// It returns an error if the KongConsumerGroup does not have a ControlPlaneRef.
func updateConsumerGroup(
	ctx context.Context,
	sdk *sdkkonnectgo.SDK,
	cl client.Client,
	group *configurationv1beta1.KongConsumerGroup,
) error {
	if group.Spec.ControlPlaneRef == nil {
		return fmt.Errorf("can't update %T without a ControlPlaneRef", group)
	}

	// TODO(pmalek) handle other types of CP ref
	// TODO(pmalek) handle cross namespace refs
	nnCP := types.NamespacedName{
		Namespace: group.Namespace,
		Name:      group.Spec.ControlPlaneRef.KonnectNamespacedRef.Name,
	}
	var cp konnectv1alpha1.KonnectGatewayControlPlane
	if err := cl.Get(ctx, nnCP, &cp); err != nil {
		return fmt.Errorf("failed to get KonnectGatewayControlPlane %s: for %T %s: %w",
			nnCP, group, client.ObjectKeyFromObject(group), err,
		)
	}

	if cp.Status.ID == "" {
		return fmt.Errorf(
			"can't update %T when referenced KonnectGatewayControlPlane %s does not have the Konnect ID",
			group, nnCP,
		)
	}

	resp, err := sdk.ConsumerGroups.UpsertConsumerGroup(ctx,
		sdkkonnectgoops.UpsertConsumerGroupRequest{
			ControlPlaneID:  cp.Status.ID,
			ConsumerGroupID: group.GetKonnectStatus().GetKonnectID(),
			ConsumerGroup:   kongConsumerGroupToSDKConsumerGroupInput(group),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrapped := wrapErrIfKonnectOpFailed(err, UpdateOp, group); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToCreate",
				errWrapped.Error(),
				group.GetGeneration(),
			),
			group,
		)
		return errWrapped
	}

	group.Status.Konnect.SetKonnectID(*resp.ConsumerGroup.ID)
	group.Status.Konnect.SetControlPlaneID(cp.Status.ID)
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			KonnectEntityProgrammedReasonProgrammed,
			"",
			group.GetGeneration(),
		),
		group,
	)

	return nil
}

// deleteConsumerGroup deletes a KongConsumerGroup in Konnect.
// The KongConsumerGroup is assumed to have a Konnect ID set in status.
// It returns an error if the operation fails.
func deleteConsumerGroup(
	ctx context.Context,
	sdk *sdkkonnectgo.SDK,
	consumer *configurationv1beta1.KongConsumerGroup,
) error {
	id := consumer.Status.Konnect.GetKonnectID()
	_, err := sdk.ConsumerGroups.DeleteConsumerGroup(ctx, consumer.Status.Konnect.ControlPlaneID, id)
	if errWrapped := wrapErrIfKonnectOpFailed(err, DeleteOp, consumer); errWrapped != nil {
		// Consumer delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkerrors.SDKError
		if errors.As(errWrapped, &sdkError) {
			if sdkError.StatusCode == 404 {
				ctrllog.FromContext(ctx).
					Info("entity not found in Konnect, skipping delete",
						"op", DeleteOp, "type", consumer.GetTypeName(), "id", id,
					)
				return nil
			}
			return FailedKonnectOpError[configurationv1beta1.KongConsumerGroup]{
				Op:  DeleteOp,
				Err: sdkError,
			}
		}
		return FailedKonnectOpError[configurationv1beta1.KongConsumerGroup]{
			Op:  DeleteOp,
			Err: errWrapped,
		}
	}

	return nil
}

func kongConsumerGroupToSDKConsumerGroupInput(
	group *configurationv1beta1.KongConsumerGroup,
) sdkkonnectgocomp.ConsumerGroupInput {
	return sdkkonnectgocomp.ConsumerGroupInput{
		Tags: metadata.ExtractTags(group),
		Name: group.Spec.Name,
	}
}
