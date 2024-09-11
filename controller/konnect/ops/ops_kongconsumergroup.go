package ops

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	"github.com/kong/kubernetes-configuration/pkg/metadata"
)

func createConsumerGroup(
	ctx context.Context,
	sdk ConsumerGroupSDK,
	group *configurationv1beta1.KongConsumerGroup,
) error {
	if group.GetControlPlaneID() == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", group, client.ObjectKeyFromObject(group))
	}

	resp, err := sdk.CreateConsumerGroup(ctx,
		group.Status.Konnect.ControlPlaneID,
		kongConsumerGroupToSDKConsumerGroupInput(group),
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrapped := wrapErrIfKonnectOpFailed(err, CreateOp, group); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KonnectEntityProgrammedConditionType,
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
			conditions.KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			conditions.KonnectEntityProgrammedReasonProgrammed,
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
	sdk ConsumerGroupSDK,
	group *configurationv1beta1.KongConsumerGroup,
) error {
	cpID := group.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf("can't update %T %s without a Konnect ControlPlane ID", group, client.ObjectKeyFromObject(group))
	}

	_, err := sdk.UpsertConsumerGroup(ctx,
		sdkkonnectops.UpsertConsumerGroupRequest{
			ControlPlaneID:  cpID,
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
				conditions.KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToCreate",
				errWrapped.Error(),
				group.GetGeneration(),
			),
			group,
		)
		return errWrapped
	}

	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			conditions.KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			conditions.KonnectEntityProgrammedReasonProgrammed,
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
	sdk ConsumerGroupSDK,
	consumer *configurationv1beta1.KongConsumerGroup,
) error {
	id := consumer.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteConsumerGroup(ctx, consumer.Status.Konnect.ControlPlaneID, id)
	if errWrapped := wrapErrIfKonnectOpFailed(err, DeleteOp, consumer); errWrapped != nil {
		// Consumer delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
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
) sdkkonnectcomp.ConsumerGroupInput {
	return sdkkonnectcomp.ConsumerGroupInput{
		Tags: append(metadata.ExtractTags(group), GenerateKubernetesMetadataTags(group)...),
		Name: group.Spec.Name,
	}
}
