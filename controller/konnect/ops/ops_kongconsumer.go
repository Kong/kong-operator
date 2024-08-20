package ops

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectgocomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectgoops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnectgoerrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
	"github.com/kong/kubernetes-configuration/pkg/metadata"
)

func createConsumer(
	ctx context.Context,
	sdk ConsumersSDK,
	consumer *configurationv1.KongConsumer,
) error {
	if consumer.GetControlPlaneID() == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", consumer, client.ObjectKeyFromObject(consumer))
	}

	resp, err := sdk.CreateConsumer(ctx,
		consumer.Status.Konnect.ControlPlaneID,
		kongConsumerToSDKConsumerInput(consumer),
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrapped := wrapErrIfKonnectOpFailed(err, CreateOp, consumer); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToCreate",
				errWrapped.Error(),
				consumer.GetGeneration(),
			),
			consumer,
		)
		return errWrapped
	}

	consumer.Status.Konnect.SetKonnectID(*resp.Consumer.ID)
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			conditions.KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			conditions.KonnectEntityProgrammedReasonProgrammed,
			"",
			consumer.GetGeneration(),
		),
		consumer,
	)

	return nil
}

// updateConsumer updates a KongConsumer in Konnect.
// The KongConsumer is assumed to have a Konnect ID set in status.
// It returns an error if the KongConsumer does not have a ControlPlaneRef.
func updateConsumer(
	ctx context.Context,
	sdk ConsumersSDK,
	cl client.Client,
	consumer *configurationv1.KongConsumer,
) error {
	if consumer.Spec.ControlPlaneRef == nil {
		return fmt.Errorf("can't update %T without a ControlPlaneRef", consumer)
	}

	// TODO(pmalek) handle other types of CP ref
	// TODO(pmalek) handle cross namespace refs
	nnCP := types.NamespacedName{
		Namespace: consumer.Namespace,
		Name:      consumer.Spec.ControlPlaneRef.KonnectNamespacedRef.Name,
	}
	var cp konnectv1alpha1.KonnectControlPlane
	if err := cl.Get(ctx, nnCP, &cp); err != nil {
		return fmt.Errorf("failed to get KonnectControlPlane %s: for %T %s: %w",
			nnCP, consumer, client.ObjectKeyFromObject(consumer), err,
		)
	}

	if cp.Status.ID == "" {
		return fmt.Errorf(
			"can't update %T when referenced KonnectControlPlane %s does not have the Konnect ID",
			consumer, nnCP,
		)
	}

	resp, err := sdk.UpsertConsumer(ctx,
		sdkkonnectgoops.UpsertConsumerRequest{
			ControlPlaneID: cp.Status.ID,
			ConsumerID:     consumer.GetKonnectStatus().GetKonnectID(),
			Consumer:       kongConsumerToSDKConsumerInput(consumer),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrapped := wrapErrIfKonnectOpFailed(err, UpdateOp, consumer); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToCreate",
				errWrapped.Error(),
				consumer.GetGeneration(),
			),
			consumer,
		)
		return errWrapped
	}

	consumer.Status.Konnect.SetKonnectID(*resp.Consumer.ID)
	consumer.Status.Konnect.SetControlPlaneID(cp.Status.ID)
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			conditions.KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			conditions.KonnectEntityProgrammedReasonProgrammed,
			"",
			consumer.GetGeneration(),
		),
		consumer,
	)

	return nil
}

// deleteConsumer deletes a KongConsumer in Konnect.
// The KongConsumer is assumed to have a Konnect ID set in status.
// It returns an error if the operation fails.
func deleteConsumer(
	ctx context.Context,
	sdk ConsumersSDK,
	consumer *configurationv1.KongConsumer,
) error {
	id := consumer.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteConsumer(ctx, consumer.Status.Konnect.ControlPlaneID, id)
	if errWrapped := wrapErrIfKonnectOpFailed(err, DeleteOp, consumer); errWrapped != nil {
		// Consumer delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnectgoerrs.SDKError
		if errors.As(errWrapped, &sdkError) {
			if sdkError.StatusCode == 404 {
				ctrllog.FromContext(ctx).
					Info("entity not found in Konnect, skipping delete",
						"op", DeleteOp, "type", consumer.GetTypeName(), "id", id,
					)
				return nil
			}
			return FailedKonnectOpError[configurationv1.KongConsumer]{
				Op:  DeleteOp,
				Err: sdkError,
			}
		}
		return FailedKonnectOpError[configurationv1.KongConsumer]{
			Op:  DeleteOp,
			Err: errWrapped,
		}
	}

	return nil
}

func kongConsumerToSDKConsumerInput(
	consumer *configurationv1.KongConsumer,
) sdkkonnectgocomp.ConsumerInput {
	return sdkkonnectgocomp.ConsumerInput{
		CustomID: &consumer.CustomID,
		Tags:     metadata.ExtractTags(consumer),
		Username: &consumer.Username,
	}
}
