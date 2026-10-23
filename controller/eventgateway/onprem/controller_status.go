/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package onprem

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// The messages of the conditions this controller sets may hold the errors of
// every gw-admin pod, each with a gw-admin response: they are bounded by
// maxConditionMessageLength.

// applyStatus sets the status of the gateway from the outcome of the push,
// with a Server-Side Apply of the status subresource: a field this
// controller set and no longer sets, e.g. a condition, is removed, and one
// changed by someone else is set back, while what someone else adds stays.
// The current conditions it sets are carried over, so their
// lastTransitionTime is kept until their status changes, as a change of their
// reason or message alone isn't a transition. The status is applied only when
// it differs from the one of the cache, sparing a read of the API server.
//
// This controller owns status.serverURL: one it doesn't publish, e.g. without
// a GWAdmin, is removed, even when someone else set it, as data planes would
// reach gw-admin there.
func (r *Reconciler) applyStatus(
	ctx context.Context,
	logger logr.Logger,
	gateway *eventgatewayv1alpha1.OnPremEventGateway,
	result pushResult,
	pushErr error,
) error {
	old := gateway.DeepCopy()
	status := eventgatewayv1alpha1.OnPremEventGatewayStatus{
		ObservedGeneration: gateway.GetGeneration(),
		ServerURL:          serverURL(result, pushErr),
		// Recorded apart, before the push (see recordRevision).
		ConfigRevision: gateway.Status.ConfigRevision,
		ConfigHash:     gateway.Status.ConfigHash,
	}
	conditions := pushConditions(gateway, result, pushErr)
	for _, cond := range conditions {
		if current := apimeta.FindStatusCondition(gateway.Status.Conditions, cond.Type); current != nil {
			status.Conditions = append(status.Conditions, *current)
		}
	}
	for _, cond := range conditions {
		apimeta.SetStatusCondition(&status.Conditions, cond)
	}
	gateway.Status = status
	if sameStatus(old.Status, status) {
		return nil
	}

	desired := gateway.DeepCopy()
	desired.SetGroupVersionKind(eventgatewayv1alpha1.GroupVersion.WithKind(eventgatewayv1alpha1.OnPremEventGatewayKind))
	// The revision is written by recordRevision alone, with an optimistic
	// lock: applying it too could set it back from a stale cache.
	desired.Status.ConfigRevision, desired.Status.ConfigHash = 0, ""
	if _, err := controllerpkgssa.ApplyStatusIfChanged(ctx, logger, r.Client, r.TypeConverter, desired, controllerpkgssa.FieldManager); err != nil {
		return fmt.Errorf("failed to update the status: %w", err)
	}
	// An empty field is left out of the apply: a serverURL set by someone
	// else would stay.
	if status.ServerURL == "" && old.Status.ServerURL != "" {
		withServerURL := gateway.DeepCopy()
		withServerURL.Status.ServerURL = old.Status.ServerURL
		if err := r.Status().Patch(ctx, gateway, client.MergeFrom(withServerURL)); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to remove the cp endpoint from the status: %w", err)
		}
	}
	return nil
}

// sameStatus reports whether the status the controller computed is the
// current one: its observed generation, its cp endpoint and its conditions,
// the lastTransitionTime of each included, whatever else someone else set.
func sameStatus(current, computed eventgatewayv1alpha1.OnPremEventGatewayStatus) bool {
	if current.ObservedGeneration != computed.ObservedGeneration || current.ServerURL != computed.ServerURL {
		return false
	}
	// A condition the controller set before and no longer sets, e.g.
	// GWAdminResolved for a gateway whose UID is invalid, is removed.
	for _, conditionType := range []kcfgconsts.ConditionType{
		eventgatewayv1alpha1.OnPremEventGatewayGWAdminResolvedType,
		eventgatewayv1alpha1.OnPremEventGatewayConfigurationPushedType,
		eventgatewayv1alpha1.OnPremEventGatewayProgrammedType,
	} {
		have := apimeta.FindStatusCondition(current.Conditions, string(conditionType))
		want := apimeta.FindStatusCondition(computed.Conditions, string(conditionType))
		switch {
		case have == nil && want == nil:
		case have == nil || want == nil:
			return false
		case have.Status != want.Status || have.Reason != want.Reason || have.Message != want.Message ||
			have.ObservedGeneration != want.ObservedGeneration || !have.LastTransitionTime.Equal(&want.LastTransitionTime):
			return false
		}
	}
	return true
}

// serverURL returns the cp endpoint of the gw-admin of the gateway the status
// publishes: the one of its GWAdmin, when the gateway may use it, even while
// it isn't ready, e.g. during a rollout of its pods, or "".
func serverURL(result pushResult, pushErr error) string {
	gwAdmin := (*operatorv1alpha1.GWAdmin)(nil)
	switch notReady, isNotReady := errors.AsType[gwAdminNotReadyError](pushErr); {
	case result.target != nil:
		gwAdmin = result.target.GWAdmin
	case isNotReady && !notReady.notAllowed:
		gwAdmin = notReady.gwAdmin
	}
	if gwAdmin == nil || gwAdmin.Status.CP == nil {
		return ""
	}
	return gwAdmin.Status.CP.ServerURL
}

// pushConditions returns the GWAdminResolved, ConfigurationPushed and
// Programmed conditions of the gateway for the outcome of the push.
// GWAdminResolved is left out when the push didn't get to resolve the
// GWAdmin, e.g. because of the UID of the gateway.
func pushConditions(gateway *eventgatewayv1alpha1.OnPremEventGateway, result pushResult, pushErr error) []metav1.Condition {
	var conditions []metav1.Condition
	notReady, isNotReady := errors.AsType[gwAdminNotReadyError](pushErr)
	switch {
	case isNotReady:
		conditions = append(conditions, k8sutils.NewConditionWithGeneration(eventgatewayv1alpha1.OnPremEventGatewayGWAdminResolvedType,
			metav1.ConditionFalse, notReady.resolvedReason(), k8sutils.TruncateMessage(notReady.msg, maxConditionMessageLength), gateway.GetGeneration()))
	case result.target != nil:
		conditions = append(conditions, k8sutils.NewConditionWithGeneration(eventgatewayv1alpha1.OnPremEventGatewayGWAdminResolvedType,
			metav1.ConditionTrue, eventgatewayv1alpha1.OnPremEventGatewayGWAdminResolvedReason,
			fmt.Sprintf("The GWAdmin %s serves the gateway", gateway.GWAdminKey()), gateway.GetGeneration()))
	}
	if pushErr != nil {
		reason := failureReason(pushErr)
		message := k8sutils.TruncateMessage(pushErr.Error(), maxConditionMessageLength)
		programmedMessage := message
		if isNotReady {
			// GWAdminResolved tells why.
			programmedMessage = eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotResolvedMessage
		}
		return append(conditions,
			k8sutils.NewConditionWithGeneration(eventgatewayv1alpha1.OnPremEventGatewayConfigurationPushedType,
				metav1.ConditionFalse, reason, message, gateway.GetGeneration()),
			k8sutils.NewConditionWithGeneration(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType,
				metav1.ConditionFalse, reason, programmedMessage, gateway.GetGeneration()),
		)
	}
	pushed := len(result.target.Pods) - len(result.pending)
	if pushed == 0 {
		// Every pod is pending: none serves the gateway yet.
		message := k8sutils.TruncateMessage("Waiting for the gw-admin pods not serving yet: "+podNames(result.pending), maxConditionMessageLength)
		return append(conditions,
			k8sutils.NewConditionWithGeneration(eventgatewayv1alpha1.OnPremEventGatewayConfigurationPushedType,
				metav1.ConditionFalse, eventgatewayv1alpha1.DependenciesNotReadyReason, message, gateway.GetGeneration()),
			k8sutils.NewConditionWithGeneration(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType,
				metav1.ConditionFalse, eventgatewayv1alpha1.DependenciesNotReadyReason, message, gateway.GetGeneration()),
		)
	}
	message := pushedMessage(pushed)
	programmedMessage := "The configuration has been pushed to every gw-admin pod"
	if len(result.pending) > 0 {
		message = k8sutils.TruncateMessage(message+"; waiting for the gw-admin pods not serving yet: "+podNames(result.pending), maxConditionMessageLength)
		programmedMessage = message
	}
	return append(conditions,
		k8sutils.NewConditionWithGeneration(eventgatewayv1alpha1.OnPremEventGatewayConfigurationPushedType,
			metav1.ConditionTrue, eventgatewayv1alpha1.OnPremEventGatewayConfigurationPushedReason,
			message, gateway.GetGeneration()),
		k8sutils.NewConditionWithGeneration(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType,
			metav1.ConditionTrue, eventgatewayv1alpha1.OnPremEventGatewayProgrammedReason,
			programmedMessage, gateway.GetGeneration()),
	)
}

// failureReason returns the reason of the conditions of the gateway
// reporting the error of a push.
func failureReason(pushErr error) kcfgconsts.ConditionReason {
	if _, ok := errors.AsType[gwAdminNotReadyError](pushErr); ok {
		return eventgatewayv1alpha1.DependenciesNotReadyReason
	}
	if _, ok := errors.AsType[invalidUIDError](pushErr); ok {
		return eventgatewayv1alpha1.OnPremEventGatewayInvalidUIDReason
	}
	return eventgatewayv1alpha1.OnPremEventGatewayConfigurationPushFailedReason
}

// podNames returns the names of the pods, separated by commas.
func podNames(pods []gwadminsync.Pod) string {
	names := make([]string, 0, len(pods))
	for _, pod := range pods {
		names = append(names, pod.Name)
	}
	return strings.Join(names, ", ")
}

// pushedMessage returns the message of the ConfigurationPushed condition of a
// gateway pushed to that many gw-admin pods.
func pushedMessage(pods int) string {
	if pods == 1 {
		return "Pushed to 1 gw-admin pod"
	}
	return fmt.Sprintf("Pushed to %d gw-admin pods", pods)
}

// reportEntities sets the Programmed condition of the entities of the
// gateway, from its configuration and its own Programmed condition: an
// entity in the configuration is programmed along with the gateway, one left
// out tells why. The SecretRefValid condition of an entity reading Secrets
// tells whether they resolve. The status of an entity is patched only when
// its conditions change.
func (r *Reconciler) reportEntities(ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway, cfg configuration) error {
	gatewayKey := client.ObjectKeyFromObject(gateway)
	programmed := apimeta.FindStatusCondition(gateway.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType))

	var errs []error
	for _, e := range cfg.included {
		cond := entityCondition(e.obj, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType, metav1.ConditionTrue, eventgatewayv1alpha1.OnPremEntityProgrammedReason,
			fmt.Sprintf("In the configuration of the OnPremEventGateway %s, pushed to its gw-admin pods", gatewayKey)+keptMessage(e))
		if programmed == nil || programmed.Status != metav1.ConditionTrue {
			message := fmt.Sprintf("In the configuration of the OnPremEventGateway %s, which is not programmed", gatewayKey)
			if programmed != nil {
				message += ": " + programmed.Message
			}
			cond = entityCondition(e.obj, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType, metav1.ConditionFalse,
				eventgatewayv1alpha1.OnPremEntityGatewayNotProgrammedReason, message+keptMessage(e))
		}
		errs = append(errs, r.setEntityStatus(ctx, e, cond))
	}
	for _, e := range cfg.excluded {
		cond := entityCondition(e.obj, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType, metav1.ConditionFalse, e.reason,
			fmt.Sprintf("Left out of the configuration of the OnPremEventGateway %s: %s", gatewayKey, e.message))
		errs = append(errs, r.setEntityStatus(ctx, e.entityStatus, cond))
	}
	errs = append(errs, r.reportDetached(ctx, gateway, cfg))
	return errors.Join(errs...)
}

// reportedEntity is an object whose status was reported for a gateway.
type reportedEntity struct {
	typ reflect.Type
	key client.ObjectKey
}

// reportDetached sets the Programmed condition of the objects reported last
// for the gateway and no longer in its configuration to False, e.g. a virtual
// cluster whose backend cluster was deleted: nothing else reports them. An
// object whose Programmed condition another gateway set since, e.g. it moved
// to it, is left alone, and so is one being deleted. One that fails to be
// read or patched is kept in the objects reported, to be tried again.
func (r *Reconciler) reportDetached(ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway, cfg configuration) error {
	current := map[reportedEntity]struct{}{}
	for _, e := range cfg.included {
		current[reportedEntity{typ: reflect.TypeOf(e.obj), key: client.ObjectKeyFromObject(e.obj)}] = struct{}{}
	}
	for _, e := range cfg.excluded {
		current[reportedEntity{typ: reflect.TypeOf(e.obj), key: client.ObjectKeyFromObject(e.obj)}] = struct{}{}
	}
	last, _ := r.reported.Load(gateway.UID)
	lastReported, _ := last.(map[reportedEntity]struct{})

	gatewayKey := client.ObjectKeyFromObject(gateway)
	var errs []error
	for e := range lastReported {
		if _, ok := current[e]; ok {
			continue
		}
		obj, ok := reflect.TypeAssert[entityObject](reflect.New(e.typ.Elem()))
		if !ok {
			continue
		}
		if err := r.Get(ctx, e.key, obj); err != nil {
			if !apierrors.IsNotFound(err) {
				current[e] = struct{}{}
				errs = append(errs, fmt.Errorf("failed to read the entity %s: %w", e.key, err))
			}
			continue
		}
		programmed := apimeta.FindStatusCondition(obj.GetConditions(), string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType))
		if !obj.GetDeletionTimestamp().IsZero() || programmed == nil || !namesGateway(programmed.Message, gatewayKey) {
			continue
		}
		if err := r.setEntityConditions(ctx, obj, nil, entityCondition(obj, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType,
			metav1.ConditionFalse, eventgatewayv1alpha1.OnPremEntityDetachedReason,
			fmt.Sprintf("No longer in the configuration of the OnPremEventGateway %s: it doesn't belong to it anymore, e.g. its parent was deleted", gatewayKey))); err != nil {
			current[e] = struct{}{}
			errs = append(errs, err)
		}
	}
	r.reported.Store(gateway.UID, current)
	return errors.Join(errs...)
}

// namesGateway tells the message of a Programmed condition the controller
// set names the gateway, e.g. "In the configuration of the
// OnPremEventGateway ns/name, ...".
func namesGateway(message string, gateway client.ObjectKey) bool {
	_, rest, found := strings.Cut(message, "OnPremEventGateway "+gateway.String())
	return found && (rest == "" || strings.ContainsAny(rest[:1], ",: "))
}

// reportEntitiesNotProgrammed sets the Programmed condition of the entities
// of the gateway to False, with the message, e.g. when the gateway is being
// deleted: they are not pushed. The entities are those its configuration
// would hold, or leave out.
func (r *Reconciler) reportEntitiesNotProgrammed(ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway, message string) error {
	cfg, err := buildConfiguration(ctx, r.Client, gateway, "", nil)
	if err != nil {
		return err
	}
	var errs []error
	notProgrammed := func(obj entityObject) {
		errs = append(errs, r.setEntityConditions(ctx, obj, nil, entityCondition(obj, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType,
			metav1.ConditionFalse, eventgatewayv1alpha1.OnPremEntityGatewayNotProgrammedReason, message)))
	}
	for _, e := range cfg.included {
		notProgrammed(e.obj)
	}
	for _, e := range cfg.excluded {
		notProgrammed(e.obj)
	}
	return errors.Join(errs...)
}

// keptMessage tells, for an entity sent as it was last, that its last
// configuration is kept, or "".
func keptMessage(e entityStatus) string {
	if !e.kept {
		return ""
	}
	return "; a Secret it reads can't be used, its last configuration is kept"
}

// setEntityStatus sets the conditions of the entity: programmed, and its
// SecretRefValid condition, removed when it reads no Secret, e.g. one whose
// value moved from a Secret to an inline one.
func (r *Reconciler) setEntityStatus(ctx context.Context, e entityStatus, programmed metav1.Condition) error {
	if e.secretRefValid == nil {
		return r.setEntityConditions(ctx, e.obj, []string{string(konnectv1alpha1.SecretRefValidConditionType)}, programmed)
	}
	return r.setEntityConditions(ctx, e.obj, nil, *e.secretRefValid, programmed)
}

// secretRefValidCondition returns the SecretRefValid condition of the
// entity: True, or False with the problem when there is one. Its
// lastTransitionTime is set when it is applied (see setEntityConditions), so
// that the same entities build the same configuration.
func secretRefValidCondition(obj entityObject, problem string) *metav1.Condition {
	cond := entityCondition(obj, konnectv1alpha1.SecretRefValidConditionType, metav1.ConditionTrue,
		konnectv1alpha1.SecretRefReasonValid, "The Secrets it reads are resolved")
	if problem != "" {
		cond = entityCondition(obj, konnectv1alpha1.SecretRefValidConditionType, metav1.ConditionFalse,
			konnectv1alpha1.SecretRefReasonInvalid, problem)
	}
	cond.LastTransitionTime = metav1.Time{}
	return &cond
}

// entityCondition returns a condition of the entity, at its generation.
func entityCondition[T, R ~string](obj entityObject, conditionType T, status metav1.ConditionStatus, reason R, message string) metav1.Condition {
	return k8sutils.NewConditionWithGeneration(kcfgconsts.ConditionType(conditionType), status, kcfgconsts.ConditionReason(reason),
		k8sutils.TruncateMessage(message, maxConditionMessageLength), obj.GetGeneration())
}

// setEntityConditions sets the conditions on the entity and removes those of
// the removed types, patching its status when they change, with an
// optimistic lock: a cache behind the API server could otherwise set back the
// conditions someone else set. An entity deleted meanwhile is left alone.
func (r *Reconciler) setEntityConditions(ctx context.Context, obj entityObject, removed []string, conds ...metav1.Condition) error {
	conditions := slices.Clone(obj.GetConditions())
	changed := false
	for _, conditionType := range removed {
		changed = apimeta.RemoveStatusCondition(&conditions, conditionType) || changed
	}
	for _, cond := range conds {
		changed = apimeta.SetStatusCondition(&conditions, cond) || changed
	}
	if !changed {
		return nil
	}
	old, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		return fmt.Errorf("failed to copy %T", obj)
	}
	obj.SetConditions(conditions)
	if err := r.Status().Patch(ctx, obj, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to set the conditions of the entity %s: %w", client.ObjectKeyFromObject(obj), err)
	}
	return nil
}
