package konnect

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectgocomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectgoops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/models/sdkerrors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func createConsumer(
	ctx context.Context,
	sdk *sdkkonnectgo.SDK,
	consumer *configurationv1.KongConsumer,
) error {
	if consumer.GetControlPlaneID() == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", consumer, client.ObjectKeyFromObject(consumer))
	}

	resp, err := sdk.Consumers.CreateConsumer(ctx,
		consumer.Status.Konnect.ControlPlaneID,
		kongConsumerToSDKConsumerInput(consumer),
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrapped := wrapErrIfKonnectOpFailed[configurationv1.KongConsumer](err, CreateOp, consumer); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				KonnectEntityProgrammedConditionType,
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
			KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			KonnectEntityProgrammedReason,
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
	sdk *sdkkonnectgo.SDK,
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

	resp, err := sdk.Consumers.UpsertConsumer(ctx,
		sdkkonnectgoops.UpsertConsumerRequest{
			ControlPlaneID: cp.Status.ID,
			ConsumerID:     consumer.GetKonnectStatus().GetKonnectID(),
			Consumer:       kongConsumerToSDKConsumerInput(consumer),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrapped := wrapErrIfKonnectOpFailed[configurationv1.KongConsumer](err, UpdateOp, consumer); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				KonnectEntityProgrammedConditionType,
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
			KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			KonnectEntityProgrammedReason,
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
	sdk *sdkkonnectgo.SDK,
	consumer *configurationv1.KongConsumer,
) error {
	id := consumer.Status.Konnect.GetKonnectID()
	_, err := sdk.Consumers.DeleteConsumer(ctx, consumer.Status.Konnect.ControlPlaneID, id)
	if errWrapped := wrapErrIfKonnectOpFailed[configurationv1.KongConsumer](err, DeleteOp, consumer); errWrapped != nil {
		// Consumer delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkerrors.SDKError
		if errors.As(errWrapped, &sdkError) && sdkError.StatusCode == 404 {
			ctrllog.FromContext(ctx).
				Info("entity not found in Konnect, skipping delete",
					"op", DeleteOp, "type", consumer.GetTypeName(), "id", id,
				)
			return nil
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
		Tags:     ExtractUserTags(consumer),
		Username: &consumer.Username,
	}
}

// ExtractUserTags extracts a set of tags from a comma-separated string.
// Copy pasted from: https://github.com/Kong/kubernetes-ingress-controller/blob/eb80ec2c58f4d53f8c6d7c997bcfb1f334b801e1/internal/annotations/annotations.go#L407-L416
func ExtractUserTags(obj metav1.Object) []string {
	anns := obj.GetAnnotations()
	val := anns[AnnotationPrefix+UserTagKey]
	// If the annotation is not present, the map provides an empty value,
	// and splitting that will create a slice containing a single empty string tag.
	// These aren't valid, hence this special case.
	if len(val) == 0 {
		return []string{}
	}
	return strings.Split(val, ",")
}
