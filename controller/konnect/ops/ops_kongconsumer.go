package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/pkg/log"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func createConsumer(
	ctx context.Context,
	sdk ConsumersSDK,
	cgSDK ConsumerGroupSDK,
	cl client.Client,
	consumer *configurationv1.KongConsumer,
) error {
	cpID := consumer.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", consumer, client.ObjectKeyFromObject(consumer))
	}

	resp, err := sdk.CreateConsumer(ctx,
		cpID,
		kongConsumerToSDKConsumerInput(consumer),
	)
	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, consumer); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(consumer, konnectv1alpha1.KonnectEntityProgrammedReasonKonnectAPIOpFailed, errWrap.Error())
		return errWrap
	}

	// Set the Konnect ID in the status to keep it even if ConsumerGroup assignments fail.
	consumer.Status.Konnect.SetKonnectID(*resp.Consumer.ID)

	if err = handleConsumerGroupAssignments(ctx, consumer, cl, cgSDK, cpID); err != nil {
		return fmt.Errorf("failed to handle ConsumerGroup assignments: %w", err)
	}

	// The Consumer is considered Programmed if it was successfully created and all its _valid_ ConsumerGroup references
	// are in sync.
	SetKonnectEntityProgrammedCondition(consumer)

	return nil
}

// updateConsumer updates a KongConsumer in Konnect.
// The KongConsumer is assumed to have a Konnect ID set in status.
// It returns an error if the KongConsumer does not have a ControlPlaneRef.
func updateConsumer(
	ctx context.Context,
	sdk ConsumersSDK,
	cgSDK ConsumerGroupSDK,
	cl client.Client,
	consumer *configurationv1.KongConsumer,
) error {
	cpID := consumer.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf("can't update %T without a ControlPlaneID", consumer)
	}

	_, err := sdk.UpsertConsumer(ctx,
		sdkkonnectops.UpsertConsumerRequest{
			ControlPlaneID: cpID,
			ConsumerID:     consumer.GetKonnectStatus().GetKonnectID(),
			Consumer:       kongConsumerToSDKConsumerInput(consumer),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, consumer); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(consumer, konnectv1alpha1.KonnectEntityProgrammedReasonKonnectAPIOpFailed, errWrap.Error())
		return errWrap
	}

	if err = handleConsumerGroupAssignments(ctx, consumer, cl, cgSDK, cpID); err != nil {
		return fmt.Errorf("failed to handle ConsumerGroup assignments: %w", err)
	}

	// The Consumer is considered Programmed if it was successfully updated and all its _valid_ ConsumerGroup references
	// are in sync.
	SetKonnectEntityProgrammedCondition(consumer)

	return nil
}

// handleConsumerGroupAssignments resolves ConsumerGroup references of a KongConsumer, reconciles them with Konnect, and
// updates the Consumer conditions accordingly:
// - sets Programmed to False if any operation fails
// - sets KongConsumerGroupRefsValid to False if any of the ConsumerGroup references are invalid
// - sets KongConsumerGroupRefsValid to True if all ConsumerGroup references are valid
// It returns an error if any of the operations fail (fetching KongConsumers from cache, fetching the actual Konnect
// ConsumerGroups the Consumer is assigned to, etc.).
func handleConsumerGroupAssignments(
	ctx context.Context,
	consumer *configurationv1.KongConsumer,
	cl client.Client,
	cgSDK ConsumerGroupSDK,
	cpID string,
) error {
	// Resolve the Konnect IDs of the ConsumerGroups referenced by the KongConsumer.
	desiredConsumerGroupsIDs, invalidConsumerGroups, err := resolveConsumerGroupsKonnectIDs(ctx, consumer, cl)

	// Even if we have invalid ConsumerGroup references, we carry on with the ones that are valid. Invalid ones will be
	// reported in the condition.
	populateConsumerGroupRefsValidCondition(invalidConsumerGroups, consumer)

	if err != nil {
		SetKonnectEntityProgrammedConditionFalse(consumer, konnectv1alpha1.KonnectEntityProgrammedReasonFailedToResolveConsumerGroupRefs, err.Error())
		return err
	}

	// Reconcile the ConsumerGroups assigned to the KongConsumer in Konnect (list the actual ConsumerGroups, calculate the
	// difference, and add/remove the Consumer from the ConsumerGroups accordingly).
	if err := reconcileConsumerGroupsWithKonnect(ctx, desiredConsumerGroupsIDs, cgSDK, cpID, consumer); err != nil {
		SetKonnectEntityProgrammedConditionFalse(consumer, konnectv1alpha1.KonnectEntityProgrammedReasonFailedToReconcileConsumerGroupsWithKonnect, err.Error())
		return err
	}
	return nil
}

// reconcileConsumerGroupsWithKonnect reconciles the ConsumerGroups assigned to a KongConsumer in Konnect. It calculates
// the difference between the desired ConsumerGroups and the actual ConsumerGroups in Konnect and adds or removes the
// Consumer from the ConsumerGroups accordingly. It returns an error if any of the Konnect operations fail.
//
// TODO: https://github.com/Kong/gateway-operator/issues/634
// Please note this implementation relies on imperative operations to list, add and remove Consumers from ConsumerGroups.
// This is because the Konnect API does not provide a way to atomically assign a Consumer to multiple ConsumerGroups
// declaratively. It's to be changed once such an API is made available (KOKO-1952).
func reconcileConsumerGroupsWithKonnect(
	ctx context.Context,
	desiredConsumerGroupsIDs []string,
	cgSDK ConsumerGroupSDK,
	cpID string,
	consumer *configurationv1.KongConsumer,
) error {
	// List the ConsumerGroups that the Consumer is assigned to in Konnect.
	cgsResp, err := cgSDK.ListConsumerGroupsForConsumer(ctx, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
		ControlPlaneID: cpID,
		ConsumerID:     consumer.GetKonnectStatus().GetKonnectID(),
	})
	if err != nil {
		return fmt.Errorf("failed to list ConsumerGroups for Consumer %s: %w", client.ObjectKeyFromObject(consumer), err)
	}

	// Account for an unexpected case where the response body is nil.
	respBody := lo.FromPtrOr(cgsResp.Object, sdkkonnectops.ListConsumerGroupsForConsumerResponseBody{})
	// Filter out empty IDs with lo.Compact just in case we get nil IDs in the response.
	actualConsumerGroupsIDs := lo.Compact(lo.Map(respBody.Data, func(cg sdkkonnectcomp.ConsumerGroup, _ int) string {
		// Convert nil IDs to empty strings.
		return lo.FromPtrOr(cg.GetID(), "")
	}))

	// Calculate the difference between the desired and actual ConsumerGroups.
	consumerGroupsToBeAddedTo, consumerGroupsToBeRemovedFrom := lo.Difference(desiredConsumerGroupsIDs, actualConsumerGroupsIDs)
	log.Debug(ctrllog.FromContext(ctx), "reconciling ConsumerGroups for KongConsumer", consumer,
		"groupsToBeAddedTo", consumerGroupsToBeAddedTo,
		"groupsToBeRemovedFrom", consumerGroupsToBeRemovedFrom,
	)

	// Adding consumer to consumer groups that it is not assigned to yet.
	for _, cgID := range consumerGroupsToBeAddedTo {
		log.Debug(ctrllog.FromContext(ctx), "adding KongConsumer to group", consumer,
			"group", cgID,
		)
		_, err := cgSDK.AddConsumerToGroup(ctx, sdkkonnectops.AddConsumerToGroupRequest{
			ControlPlaneID:  cpID,
			ConsumerGroupID: cgID,
			RequestBody: &sdkkonnectops.AddConsumerToGroupRequestBody{
				ConsumerID: lo.ToPtr(consumer.GetKonnectStatus().GetKonnectID()),
			},
		})
		if err != nil {
			return fmt.Errorf("failed to add Consumer %s to ConsumerGroup %s: %w", client.ObjectKeyFromObject(consumer), cgID, err)
		}
	}

	// Removing consumer from consumer groups that it is not assigned to anymore.
	for _, cgID := range consumerGroupsToBeRemovedFrom {
		log.Debug(ctrllog.FromContext(ctx), "removing KongConsumer from group", consumer,
			"group", cgID,
		)
		_, err := cgSDK.RemoveConsumerFromGroup(ctx, sdkkonnectops.RemoveConsumerFromGroupRequest{
			ControlPlaneID:  cpID,
			ConsumerGroupID: cgID,
			ConsumerID:      consumer.GetKonnectStatus().GetKonnectID(),
		})
		if err != nil {
			return fmt.Errorf("failed to remove Consumer %s from ConsumerGroup %s: %w", client.ObjectKeyFromObject(consumer), cgID, err)
		}
	}

	return nil
}

func populateConsumerGroupRefsValidCondition(invalidConsumerGroups []invalidConsumerGroupRef, consumer *configurationv1.KongConsumer) {
	if len(invalidConsumerGroups) > 0 {
		reasons := make([]string, 0, len(invalidConsumerGroups))
		for _, cg := range invalidConsumerGroups {
			reasons = append(reasons, fmt.Sprintf("%s: %s", cg.Name, cg.Reason))
		}
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				konnectv1alpha1.KongConsumerGroupRefsValidConditionType,
				metav1.ConditionFalse,
				konnectv1alpha1.KongConsumerGroupRefsReasonInvalid,
				fmt.Sprintf("Invalid ConsumerGroup references: %s", strings.Join(reasons, ", ")),
				consumer.GetGeneration(),
			),
			consumer,
		)
	} else {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				konnectv1alpha1.KongConsumerGroupRefsValidConditionType,
				metav1.ConditionTrue,
				konnectv1alpha1.KongConsumerGroupRefsReasonValid,
				"",
				consumer.GetGeneration(),
			),
			consumer,
		)
	}
}

type invalidConsumerGroupRef struct {
	Name   string
	Reason string
}

// resolveConsumerGroupsKonnectIDs resolves the Konnect IDs of the ConsumerGroups referenced by the KongConsumer. It
// returns the IDs of the desired ConsumerGroups, a list of invalid ConsumerGroup references, and an error if fetching
// any of the ConsumerGroups fails with an error other than NotFound.
func resolveConsumerGroupsKonnectIDs(
	ctx context.Context,
	consumer *configurationv1.KongConsumer,
	cl client.Client,
) ([]string, []invalidConsumerGroupRef, error) {
	var (
		desiredConsumerGroupsIDs []string
		invalidConsumerGroups    []invalidConsumerGroupRef
	)
	for _, cgName := range consumer.ConsumerGroups {
		var cg configurationv1beta1.KongConsumerGroup
		if err := cl.Get(ctx, client.ObjectKey{Name: cgName, Namespace: consumer.Namespace}, &cg); err != nil {
			if k8serrors.IsNotFound(err) {
				invalidConsumerGroups = append(invalidConsumerGroups, invalidConsumerGroupRef{
					Name:   cgName,
					Reason: "NotFound",
				})
				continue
			}
			return nil, nil, fmt.Errorf("failed to get KongConsumerGroup %s/%s: %w", consumer.Namespace, cgName, err)
		}
		if cg.GetKonnectStatus() == nil || cg.GetKonnectStatus().GetKonnectID() == "" {
			invalidConsumerGroups = append(invalidConsumerGroups, invalidConsumerGroupRef{
				Name:   cgName,
				Reason: "NotCreatedInKonnect",
			})
			continue
		}
		desiredConsumerGroupsIDs = append(desiredConsumerGroupsIDs, cg.GetKonnectStatus().GetKonnectID())
	}
	if len(invalidConsumerGroups) > 0 {
		err := errors.New("some KongConsumerGroups couldn't be assigned to KongConsumer, see KongConsumer status for details")
		return desiredConsumerGroupsIDs, invalidConsumerGroups, err
	}
	return desiredConsumerGroupsIDs, invalidConsumerGroups, nil
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
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, consumer); errWrap != nil {
		// Consumer delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
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
			Err: errWrap,
		}
	}

	return nil
}

func kongConsumerToSDKConsumerInput(
	consumer *configurationv1.KongConsumer,
) sdkkonnectcomp.ConsumerInput {
	return sdkkonnectcomp.ConsumerInput{
		CustomID: lo.ToPtr(consumer.CustomID),
		Tags:     GenerateTagsForObject(consumer),
		Username: lo.ToPtr(consumer.Username),
	}
}
