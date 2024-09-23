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

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	"github.com/kong/gateway-operator/controller/pkg/log"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	"github.com/kong/kubernetes-configuration/pkg/metadata"
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
	if errWrapped := wrapErrIfKonnectOpFailed(err, CreateOp, consumer); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				conditions.KonnectEntityProgrammedReasonKonnectAPIOpFailed,
				errWrapped.Error(),
				consumer.GetGeneration(),
			),
			consumer,
		)
		return errWrapped
	}

	// Set the Konnect ID in the status to keep it even if ConsumerGroup assignments fail.
	consumer.Status.Konnect.SetKonnectID(*resp.Consumer.ID)

	if err = handleConsumerGroupAssignments(ctx, consumer, cl, cgSDK, cpID); err != nil {
		return fmt.Errorf("failed to handle ConsumerGroup assignments: %w", err)
	}

	// The Consumer is considered Programmed if it was successfully created and all its _valid_ ConsumerGroup references
	// are in sync.
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
	if errWrapped := wrapErrIfKonnectOpFailed(err, UpdateOp, consumer); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				conditions.KonnectEntityProgrammedReasonKonnectAPIOpFailed,
				errWrapped.Error(),
				consumer.GetGeneration(),
			),
			consumer,
		)
		return errWrapped
	}

	if err = handleConsumerGroupAssignments(ctx, consumer, cl, cgSDK, cpID); err != nil {
		return fmt.Errorf("failed to handle ConsumerGroup assignments: %w", err)
	}

	// The Consumer is considered Programmed if it was successfully updated and all its _valid_ ConsumerGroup references
	// are in sync.
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
	if err != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				conditions.KonnectEntityProgrammedReasonFailedToResolveConsumerGroupRefs,
				err.Error(),
				consumer.GetGeneration(),
			),
			consumer,
		)
		return err
	}

	// Even if we have invalid ConsumerGroup references, we carry on with the ones that are valid. Invalid ones will be
	// reported in the condition.
	populateConsumerGroupRefsValidCondition(invalidConsumerGroups, consumer)

	// Reconcile the ConsumerGroups assigned to the KongConsumer in Konnect (list the actual ConsumerGroups, calculate the
	// difference, and add/remove the Consumer from the ConsumerGroups accordingly).
	if err := reconcileConsumerGroupsWithKonnect(ctx, desiredConsumerGroupsIDs, cgSDK, cpID, consumer); err != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				conditions.KonnectEntityProgrammedReasonFailedToReconcileConsumerGroupsWithKonnect,
				err.Error(),
				consumer.GetGeneration(),
			),
			consumer,
		)
		return err
	}
	return nil
}

// reconcileConsumerGroupsWithKonnect reconciles the ConsumerGroups assigned to a KongConsumer in Konnect. It calculates
// the difference between the desired ConsumerGroups and the actual ConsumerGroups in Konnect and adds or removes the
// Consumer from the ConsumerGroups accordingly. It returns an error if any of the Konnect operations fail.
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
				conditions.KongConsumerGroupRefsValidConditionType,
				metav1.ConditionFalse,
				conditions.KongConsumerGroupRefsReasonInvalid,
				fmt.Sprintf("Invalid ConsumerGroup references: %s", strings.Join(reasons, ", ")),
				consumer.GetGeneration(),
			),
			consumer,
		)
	} else {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KongConsumerGroupRefsValidConditionType,
				metav1.ConditionTrue,
				conditions.KongConsumerGroupRefsReasonValid,
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
		if cg.GetKonnectStatus() != nil && cg.GetKonnectStatus().GetKonnectID() == "" {
			invalidConsumerGroups = append(invalidConsumerGroups, invalidConsumerGroupRef{
				Name:   cgName,
				Reason: "NotCreatedInKonnect",
			})
			continue
		}
		desiredConsumerGroupsIDs = append(desiredConsumerGroupsIDs, cg.GetKonnectStatus().GetKonnectID())
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
) sdkkonnectcomp.ConsumerInput {
	return sdkkonnectcomp.ConsumerInput{
		CustomID: &consumer.CustomID,
		Tags:     append(metadata.ExtractTags(consumer), GenerateKubernetesMetadataTags(consumer)...),
		Username: &consumer.Username,
	}
}
