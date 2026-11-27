package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// enforceAIGatewayConsumerConsumerGroups reconciles the AIGatewayConsumerGroups
// referenced by spec.consumerGroups into Konnect. Unlike KongConsumer (whose
// Konnect API requires imperative add/remove), the AI Gateway API exposes a
// declarative endpoint that replaces the whole membership set in one call, so
// we resolve the referenced groups to their Konnect names and push the set via
// UpdateAiGatewayConsumerGroupsForConsumer.
//
// Invalid references (missing CR, or a CR not yet Programmed in Konnect) do not
// abort enforcement of the valid subset: the valid names are pushed, the
// ConsumerGroupRefsValid condition reports the invalid ones, and an error is
// returned so the consumer is requeued.
//
// The generated create/update ops call this after the consumer's Konnect ID is
// set. It is intentionally hand-written (like ops_kongconsumer.go); the
// crd-from-oas pipeline only emits the spec field, the ref struct, and the call
// site — see the "associations" opt-in in crd-from-oas/config.yaml.
func enforceAIGatewayConsumerConsumerGroups(
	ctx context.Context,
	cl client.Client,
	sdk sdkkonnectgo.AIGatewayConsumersSDK,
	obj *aiconfigurationv1alpha1.AIGatewayConsumer,
	gatewayID string,
) error {
	resolvedNames, invalid, err := resolveAIGatewayConsumerConsumerGroups(ctx, cl, obj)
	if err != nil {
		return err
	}

	setAIGatewayConsumerGroupRefsValidCondition(obj, invalid)
	_, err = sdk.UpdateAiGatewayConsumerGroupsForConsumer(ctx, sdkkonnectops.UpdateAiGatewayConsumerGroupsForConsumerRequest{
		GatewayID:        gatewayID,
		ConsumerIDOrName: obj.GetKonnectID(),
		RequestBody: sdkkonnectops.UpdateAiGatewayConsumerGroupsForConsumerRequestBody{
			ConsumerGroups: resolvedNames,
		},
	})
	if err != nil {
		return KonnectEntityCreatedButRelationsFailedError{
			KonnectID: obj.GetKonnectID(),
			Reason:    konnectv1alpha1.KonnectEntityProgrammedReasonFailedToReconcileConsumerGroupsWithKonnect,
			Err:       err,
		}
	}

	if len(invalid) > 0 {
		return KonnectEntityCreatedButRelationsFailedError{
			KonnectID: obj.GetKonnectID(),
			Reason:    konnectv1alpha1.KonnectEntityProgrammedReasonFailedToResolveConsumerGroupRefs,
			Err:       errors.New("some AIGatewayConsumerGroups couldn't be assigned to the AIGatewayConsumer, see status for details"),
		}
	}
	return nil
}

// aiGatewayConsumerGroupsSyncState is the resolved consumer group membership
// of an AIGatewayConsumer, part of its sync payload.
type aiGatewayConsumerGroupsSyncState struct {
	Names   []string                           `json:"names,omitempty"`
	Invalid []invalidAIGatewayConsumerGroupRef `json:"invalid,omitempty"`
}

// syncStateAIGatewayConsumerConsumerGroups returns the resolved consumer group
// membership of the consumer, sorted, so that the guarded update sends the
// consumer again when the membership or the validity of a reference changes.
func syncStateAIGatewayConsumerConsumerGroups(
	ctx context.Context,
	cl client.Client,
	obj *aiconfigurationv1alpha1.AIGatewayConsumer,
) (any, error) {
	names, invalid, err := resolveAIGatewayConsumerConsumerGroups(ctx, cl, obj)
	if err != nil {
		return nil, err
	}
	names = slices.Clone(names)
	slices.Sort(names)
	invalid = slices.Clone(invalid)
	slices.SortFunc(invalid, func(a, b invalidAIGatewayConsumerGroupRef) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Reason, b.Reason))
	})
	return aiGatewayConsumerGroupsSyncState{Names: names, Invalid: invalid}, nil
}

// enforceAIGatewayConsumerConsumerGroupsIfDrifted reconciles the consumer group
// membership like enforceAIGatewayConsumerConsumerGroups, but only writes it
// when the membership in Konnect differs from the desired one. It is used when
// the consumer itself was verified with a GET instead of being sent, so that
// membership drift is reverted without a write per sync period.
func enforceAIGatewayConsumerConsumerGroupsIfDrifted(
	ctx context.Context,
	cl client.Client,
	sdk sdkkonnectgo.AIGatewayConsumersSDK,
	obj *aiconfigurationv1alpha1.AIGatewayConsumer,
	gatewayID string,
) error {
	resolvedNames, invalid, err := resolveAIGatewayConsumerConsumerGroups(ctx, cl, obj)
	if err != nil {
		return err
	}
	actualNames, err := listAIGatewayConsumerGroupNamesForConsumer(ctx, sdk, gatewayID, obj.GetKonnectID())
	if err != nil {
		return KonnectEntityCreatedButRelationsFailedError{
			KonnectID: obj.GetKonnectID(),
			Reason:    konnectv1alpha1.KonnectEntityProgrammedReasonFailedToReconcileConsumerGroupsWithKonnect,
			Err:       err,
		}
	}
	// spec.consumerGroups can reference a group more than once, while Konnect
	// holds each membership once: compare both sides as sets.
	desired := slices.Clone(resolvedNames)
	slices.Sort(desired)
	slices.Sort(actualNames)
	if !slices.Equal(slices.Compact(desired), slices.Compact(actualNames)) || len(invalid) > 0 {
		return enforceAIGatewayConsumerConsumerGroups(ctx, cl, sdk, obj, gatewayID)
	}
	setAIGatewayConsumerGroupRefsValidCondition(obj, invalid)
	return nil
}

// listAIGatewayConsumerGroupNamesForConsumer returns the names of the consumer
// groups the consumer belongs to in Konnect.
func listAIGatewayConsumerGroupNamesForConsumer(
	ctx context.Context,
	sdk sdkkonnectgo.AIGatewayConsumersSDK,
	gatewayID, consumerID string,
) ([]string, error) {
	var (
		names []string
		after *string
		// Cursors already requested, to detect a next-page cursor that does
		// not advance (directly or through a longer cycle).
		seenCursors = map[string]struct{}{}
	)
	for {
		resp, err := sdk.ListAiGatewayConsumerGroupsForConsumer(ctx, sdkkonnectops.ListAiGatewayConsumerGroupsForConsumerRequest{
			GatewayID:        gatewayID,
			ConsumerIDOrName: consumerID,
			PageAfter:        after,
		})
		if err != nil {
			return nil, fmt.Errorf("failed listing consumer groups of AI Gateway consumer %s: %w", consumerID, err)
		}
		if resp == nil || resp.ListAIGatewayConsumerGroupsResponse == nil {
			return nil, ErrNilResponse
		}
		for _, group := range resp.ListAIGatewayConsumerGroupsResponse.GetData() {
			names = append(names, group.GetName())
		}
		meta := resp.ListAIGatewayConsumerGroupsResponse.GetMeta()
		page := meta.GetPage()
		if after, err = nextPageCursor(page.GetNext(), seenCursors); err != nil {
			return nil, fmt.Errorf("failed listing consumer groups of AI Gateway consumer %s: %w", consumerID, err)
		}
		if after == nil {
			return names, nil
		}
	}
}

// invalidAIGatewayConsumerGroupRef describes an unresolvable consumer-group reference.
type invalidAIGatewayConsumerGroupRef struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// resolveAIGatewayConsumerConsumerGroups resolves spec.consumerGroups references
// to the referenced AIGatewayConsumerGroups' Konnect names. References that point
// to a missing or not-yet-Programmed CR are collected as invalid (the reconcile
// proceeds with the valid subset). A non-NotFound client error aborts.
func resolveAIGatewayConsumerConsumerGroups(
	ctx context.Context,
	cl client.Client,
	obj *aiconfigurationv1alpha1.AIGatewayConsumer,
) (resolvedNames []string, invalid []invalidAIGatewayConsumerGroupRef, err error) {
	resolvedNames = make([]string, 0, len(obj.Spec.ConsumerGroups))
	for _, ref := range obj.Spec.ConsumerGroups {
		var cg aiconfigurationv1alpha1.AIGatewayConsumerGroup
		nn := client.ObjectKey{
			Namespace: obj.GetNamespace(),
			Name:      ref.Name,
		}
		if getErr := cl.Get(ctx, nn, &cg); getErr != nil {
			if apierrors.IsNotFound(getErr) {
				invalid = append(invalid, invalidAIGatewayConsumerGroupRef{Name: ref.Name, Reason: "NotFound"})
				continue
			}
			return nil, nil, fmt.Errorf("failed to get AIGatewayConsumerGroup %s/%s: %w", obj.GetNamespace(), ref.Name, getErr)
		}
		if cg.GetKonnectID() == "" {
			invalid = append(invalid, invalidAIGatewayConsumerGroupRef{Name: ref.Name, Reason: "NotProgrammed"})
			continue
		}
		resolvedNames = append(resolvedNames, string(cg.Spec.APISpec.Name))
	}
	return resolvedNames, invalid, nil
}

// setAIGatewayConsumerGroupRefsValidCondition sets the ConsumerGroupRefsValid
// condition to False (listing the invalid refs) or True.
func setAIGatewayConsumerGroupRefsValidCondition(obj *aiconfigurationv1alpha1.AIGatewayConsumer, invalid []invalidAIGatewayConsumerGroupRef) {
	if len(invalid) > 0 {
		reasons := make([]string, 0, len(invalid))
		for _, ref := range invalid {
			reasons = append(reasons, fmt.Sprintf("%s: %s", ref.Name, ref.Reason))
		}
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				commonv1alpha1.ConsumerGroupRefsValidConditionType,
				metav1.ConditionFalse,
				commonv1alpha1.ConsumerGroupRefsReasonInvalid,
				fmt.Sprintf("Invalid AIGatewayConsumerGroup references: %s", strings.Join(reasons, ", ")),
				obj.GetGeneration(),
			),
			obj,
		)
		return
	}
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			commonv1alpha1.ConsumerGroupRefsValidConditionType,
			metav1.ConditionTrue,
			commonv1alpha1.ConsumerGroupRefsReasonValid,
			"",
			obj.GetGeneration(),
		),
		obj,
	)
}
