package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	obj *konnectv1alpha1.AIGatewayConsumer,
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

// invalidAIGatewayConsumerGroupRef describes an unresolvable consumer-group reference.
type invalidAIGatewayConsumerGroupRef struct {
	Name   string
	Reason string
}

// resolveAIGatewayConsumerConsumerGroups resolves spec.consumerGroups references
// to the referenced AIGatewayConsumerGroups' Konnect names. References that point
// to a missing or not-yet-Programmed CR are collected as invalid (the reconcile
// proceeds with the valid subset). A non-NotFound client error aborts.
func resolveAIGatewayConsumerConsumerGroups(
	ctx context.Context,
	cl client.Client,
	obj *konnectv1alpha1.AIGatewayConsumer,
) (resolvedNames []string, invalid []invalidAIGatewayConsumerGroupRef, err error) {
	resolvedNames = make([]string, 0, len(obj.Spec.ConsumerGroups))
	for _, ref := range obj.Spec.ConsumerGroups {
		var cg konnectv1alpha1.AIGatewayConsumerGroup
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
func setAIGatewayConsumerGroupRefsValidCondition(obj *konnectv1alpha1.AIGatewayConsumer, invalid []invalidAIGatewayConsumerGroupRef) {
	if len(invalid) > 0 {
		reasons := make([]string, 0, len(invalid))
		for _, ref := range invalid {
			reasons = append(reasons, fmt.Sprintf("%s: %s", ref.Name, ref.Reason))
		}
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				konnectv1alpha1.AIGatewayConsumerGroupRefsValidConditionType,
				metav1.ConditionFalse,
				konnectv1alpha1.AIGatewayConsumerGroupRefsReasonInvalid,
				fmt.Sprintf("Invalid AIGatewayConsumerGroup references: %s", strings.Join(reasons, ", ")),
				obj.GetGeneration(),
			),
			obj,
		)
		return
	}
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			konnectv1alpha1.AIGatewayConsumerGroupRefsValidConditionType,
			metav1.ConditionTrue,
			konnectv1alpha1.AIGatewayConsumerGroupRefsReasonValid,
			"",
			obj.GetGeneration(),
		),
		obj,
	)
}
