package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1beta1 "github.com/kong/kong-operator/api/configuration/v1beta1"
	kcfgkonnect "github.com/kong/kong-operator/api/konnect"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/controller/pkg/log"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

func createConsumer(
	ctx context.Context,
	sdk sdkops.ConsumersSDK,
	cgSDK sdkops.ConsumerGroupSDK,
	cl client.Client,
	consumer *configurationv1.KongConsumer,
) error {
	cpID := consumer.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: consumer, Op: CreateOp}
	}

	resp, err := sdk.CreateConsumer(ctx,
		cpID,
		kongConsumerToSDKConsumerInput(consumer),
	)

	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, consumer); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.Consumer == nil || resp.Consumer.ID == nil || *resp.Consumer.ID == "" {
		return fmt.Errorf("failed creating %s: %w", consumer.GetTypeName(), ErrNilResponse)
	}

	// Set the Konnect ID in the status to keep it even if ConsumerGroup assignments fail.
	id := *resp.Consumer.ID
	consumer.SetKonnectID(id)

	if err = handleConsumerGroupAssignments(ctx, consumer, cl, sdk, cgSDK, cpID); err != nil {
		return KonnectEntityCreatedButRelationsFailedError{
			KonnectID: id,
			Reason:    kcfgkonnect.FailedToAttachConsumerToConsumerGroupReason,
			Err:       err,
		}
	}

	return nil
}

// updateConsumer updates a KongConsumer in Konnect.
// The KongConsumer is assumed to have a Konnect ID set in status.
// It returns an error if the KongConsumer does not have a ControlPlaneRef.
func updateConsumer(
	ctx context.Context,
	sdk sdkops.ConsumersSDK,
	cgSDK sdkops.ConsumerGroupSDK,
	cl client.Client,
	consumer *configurationv1.KongConsumer,
) error {
	cpID := consumer.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: consumer, Op: UpdateOp}
	}
	id := consumer.GetKonnectStatus().GetKonnectID()

	_, err := sdk.UpsertConsumer(ctx,
		sdkkonnectops.UpsertConsumerRequest{
			ControlPlaneID: cpID,
			ConsumerID:     id,
			Consumer:       kongConsumerToSDKConsumerInput(consumer),
		},
	)

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, consumer); errWrap != nil {
		return errWrap
	}

	if err = handleConsumerGroupAssignments(ctx, consumer, cl, sdk, cgSDK, cpID); err != nil {
		return KonnectEntityCreatedButRelationsFailedError{
			KonnectID: id,
			Reason:    kcfgkonnect.FailedToAttachConsumerToConsumerGroupReason,
			Err:       err,
		}
	}

	return nil
}

// handleConsumerGroupAssignments resolves ConsumerGroup references of a KongConsumer, reconciles them with Konnect, and
// updates the Consumer conditions accordingly:
// - sets KongConsumerGroupRefsValid to False if any of the ConsumerGroup references are invalid
// - sets KongConsumerGroupRefsValid to True if all ConsumerGroup references are valid
// It returns an error if any of the operations fail (fetching KongConsumers from cache, fetching the actual Konnect
// ConsumerGroups the Consumer is assigned to, etc.).
func handleConsumerGroupAssignments(
	ctx context.Context,
	consumer *configurationv1.KongConsumer,
	cl client.Client,
	sdk sdkops.ConsumersSDK,
	cgSDK sdkops.ConsumerGroupSDK,
	cpID string,
) error {
	// Resolve the Konnect IDs of the ConsumerGroups referenced by the KongConsumer.
	desiredConsumerGroupsIDs, invalidConsumerGroups, err := resolveConsumerGroupsKonnectIDs(ctx, consumer, cl)

	// Even if we have invalid ConsumerGroup references, we carry on with the ones that are valid. Invalid ones will be
	// reported in the condition.
	populateConsumerGroupRefsValidCondition(invalidConsumerGroups, consumer)

	if err != nil {
		return KonnectEntityCreatedButRelationsFailedError{
			KonnectID: consumer.GetKonnectID(),
			Reason:    konnectv1alpha1.KonnectEntityProgrammedReasonFailedToResolveConsumerGroupRefs,
			Err:       err,
		}
	}

	// Reconcile the ConsumerGroups assigned to the KongConsumer in Konnect (list the actual ConsumerGroups, calculate the
	// difference, and add/remove the Consumer from the ConsumerGroups accordingly).
	if err := reconcileConsumerGroupsWithKonnect(ctx, desiredConsumerGroupsIDs, sdk, cgSDK, cpID, consumer); err != nil {
		return KonnectEntityCreatedButRelationsFailedError{
			KonnectID: consumer.GetKonnectID(),
			Reason:    konnectv1alpha1.KonnectEntityProgrammedReasonFailedToReconcileConsumerGroupsWithKonnect,
			Err:       err,
		}
	}
	return nil
}

// reconcileConsumerGroupsWithKonnect reconciles the ConsumerGroups assigned to a KongConsumer in Konnect. It calculates
// the difference between the desired ConsumerGroups and the actual ConsumerGroups in Konnect and adds or removes the
// Consumer from the ConsumerGroups accordingly. It returns an error if any of the Konnect operations fail.
//
// TODO: https://github.com/kong/kong-operator/issues/634
// Please note this implementation relies on imperative operations to list, add and remove Consumers from ConsumerGroups.
// This is because the Konnect API does not provide a way to atomically assign a Consumer to multiple ConsumerGroups
// declaratively. It's to be changed once such an API is made available (KOKO-1952).
func reconcileConsumerGroupsWithKonnect(
	ctx context.Context,
	desiredConsumerGroupsIDs []string,
	sdk sdkops.ConsumersSDK,
	cgSDK sdkops.ConsumerGroupSDK,
	cpID string,
	consumer *configurationv1.KongConsumer,
) error {
	logger := ctrllog.FromContext(ctx).WithValues("kongconsumer", client.ObjectKeyFromObject(consumer).String())

	// List the ConsumerGroups that the Consumer is assigned to in Konnect.
	cgsResp, err := sdk.ListConsumerGroupsForConsumer(ctx, sdkkonnectops.ListConsumerGroupsForConsumerRequest{
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
	log.Debug(logger, "reconciling ConsumerGroups for KongConsumer",
		"groupsToBeAddedTo", consumerGroupsToBeAddedTo,
		"groupsToBeRemovedFrom", consumerGroupsToBeRemovedFrom,
	)

	// Adding consumer to consumer groups that it is not assigned to yet.
	for _, cgID := range consumerGroupsToBeAddedTo {
		log.Debug(ctrllog.FromContext(ctx), "adding KongConsumer to group",
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
		log.Debug(logger, "removing KongConsumer from group",
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
	sdk sdkops.ConsumersSDK,
	consumer *configurationv1.KongConsumer,
) error {
	id := consumer.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteConsumer(ctx, consumer.Status.Konnect.ControlPlaneID, id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, consumer); errWrap != nil {
		return handleDeleteError(ctx, err, consumer)
	}

	return nil
}

func kongConsumerToSDKConsumerInput(
	consumer *configurationv1.KongConsumer,
) sdkkonnectcomp.Consumer {
	return sdkkonnectcomp.Consumer{
		CustomID: lo.ToPtr(consumer.CustomID),
		Tags:     GenerateTagsForObject(consumer, consumer.Spec.Tags...),
		Username: lo.ToPtr(consumer.Username),
	}
}

// getKongConsumerForUID lists consumers in Konnect with given k8s uid as its tag.
func getKongConsumerForUID(
	ctx context.Context,
	sdk sdkops.ConsumersSDK,
	consumer *configurationv1.KongConsumer,
) (string, error) {
	cpID := consumer.GetControlPlaneID()
	reqList := sdkkonnectops.ListConsumerRequest{
		// NOTE: only filter on object's UID.
		// Other fields like name might have changed in the meantime but that's OK.
		// Those will be enforced via subsequent updates.
		ControlPlaneID: cpID,
		Tags:           lo.ToPtr(UIDLabelForObject(consumer)),
	}

	resp, err := sdk.ListConsumer(ctx, reqList)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", consumer.GetTypeName(), err)
	}
	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", consumer.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), consumer)
}
