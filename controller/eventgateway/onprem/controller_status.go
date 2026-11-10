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
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

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
	if _, ok := errors.AsType[invalidPolicyChainsError](pushErr); ok {
		return "InvalidPolicyChain"
	}
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

// inConfigurationOfPrefix starts the message of the Programmed condition the
// controller sets on an entity of the configuration of a gateway.
const inConfigurationOfPrefix = "In the configuration of the OnPremEventGateway "

// reportEntities sets the Programmed condition of the entities of the
// gateway, from its configuration, its own Programmed condition and the
// outcome of the push: an entity in the configuration is programmed along
// with the gateway, one left out tells why. A data plane certificate is
// programmed once every gw-admin pod the push reached holds it, whatever the
// rest of the configuration: e.g. one an entity gw-admin rejects doesn't
// keep a data plane from rolling out onto it. While the push waits for its
// backoff, nothing tells, and its Programmed condition is left as it is.
// The SecretRefValid condition of an entity reading Secrets tells whether
// they resolve. The status of an entity is patched only when its conditions
// change.
func (r *Reconciler) reportEntities(ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway, cfg configuration, result pushResult) error {
	gatewayKey := client.ObjectKeyFromObject(gateway)
	programmed := apimeta.FindStatusCondition(gateway.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType))
	certificates := make(map[types.UID]dataPlaneCertificate, len(cfg.certificates))
	for _, cert := range cfg.certificates {
		certificates[cert.obj.GetUID()] = cert
	}

	var errs []error
	for _, e := range cfg.included {
		if cert, ok := certificates[e.obj.GetUID()]; ok && result.target != nil {
			if result.certificates == nil {
				// Nothing was pushed, e.g. while the push waits for its
				// backoff: the configuration is the one the Programmed
				// condition tells of, a change to it skipping the backoff,
				// so the condition is kept, at the generation it applies to.
				conds := []metav1.Condition{}
				if e.secretRefValid != nil {
					conds = append(conds, *e.secretRefValid)
				}
				if cond := apimeta.FindStatusCondition(e.obj.GetConditions(), string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType)); cond != nil {
					kept := *cond
					kept.ObservedGeneration = e.obj.GetGeneration()
					conds = append(conds, kept)
				}
				errs = append(errs, r.setEntityConditions(ctx, e.obj, nil, conds...))
				continue
			}
			errs = append(errs, r.setEntityStatus(ctx, e, certificateCondition(e, gatewayKey, result, cert)))
			continue
		}
		held := heldMessage(cfg, e)
		cond := entityCondition(e.obj, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType, metav1.ConditionTrue, eventgatewayv1alpha1.OnPremEntityProgrammedReason,
			fmt.Sprintf(inConfigurationOfPrefix+"%s, pushed to its gw-admin pods", gatewayKey)+keptMessage(e)+held)
		if programmed == nil || programmed.Status != metav1.ConditionTrue {
			message := fmt.Sprintf("In the configuration of the OnPremEventGateway %s, which is not programmed", gatewayKey)
			if programmed != nil {
				message += ": " + programmed.Message
			}
			cond = entityCondition(e.obj, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType, metav1.ConditionFalse,
				eventgatewayv1alpha1.OnPremEntityGatewayNotProgrammedReason, message+keptMessage(e)+held)
		}
		// An entity kept for the policies using it reports that its deletion is
		// blocked. Its status is left as it is: it is in the configuration, so it
		// is programmed along with the gateway, as is anything else in it.
		if held != "" {
			cond.Reason = string(eventgatewayv1alpha1.OnPremEntityDeletionBlockedReason)
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

// certificateCondition returns the Programmed condition of a data plane
// certificate in the configuration of the gateway: True once every gw-admin
// pod the push reached holds it (see certificateOutcome.held).
func certificateCondition(e entityStatus, gatewayKey client.ObjectKey, result pushResult, cert dataPlaneCertificate) metav1.Condition {
	held, missing, unconfirmed, rejected := result.certificates.held(result.target, result.pending, cert)
	if held {
		return entityCondition(e.obj, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType, metav1.ConditionTrue, eventgatewayv1alpha1.OnPremEntityProgrammedReason,
			fmt.Sprintf(inConfigurationOfPrefix+"%s, registered on every gw-admin pod serving it", gatewayKey))
	}
	if len(rejected) > 0 {
		reasons := make([]string, 0, len(rejected))
		for _, pod := range slices.Sorted(maps.Keys(rejected)) {
			reasons = append(reasons, fmt.Sprintf("gw-admin pod %s: %s", pod, rejected[pod]))
		}
		return entityCondition(e.obj, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType, metav1.ConditionFalse, eventgatewayv1alpha1.OnPremEntityInvalidReason,
			fmt.Sprintf("In the configuration of the OnPremEventGateway %s, rejected by gw-admin: %s", gatewayKey, strings.Join(reasons, "; ")))
	}
	var problems []string
	if len(missing) > 0 {
		problems = append(problems, "not registered yet on the gw-admin pods "+strings.Join(missing, ", "))
	}
	if len(unconfirmed) > 0 {
		problems = append(problems, "not confirmed on the gw-admin pods "+strings.Join(unconfirmed, ", ")+", whose push failed")
	}
	if len(problems) == 0 {
		problems = append(problems, "no gw-admin pod serves it yet")
	}
	message := fmt.Sprintf("In the configuration of the OnPremEventGateway %s, %s", gatewayKey, strings.Join(problems, "; "))
	return entityCondition(e.obj, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType, metav1.ConditionFalse, eventgatewayv1alpha1.OnPremEntityNotRegisteredReason, message)
}

// reportedEntity is an object whose status was reported for a gateway.
type reportedEntity struct {
	typ reflect.Type
	key client.ObjectKey
}

// reportDetached sets the Programmed condition of the objects reported last
// for the gateway and no longer in its configuration to False, e.g. a virtual
// cluster whose backend cluster was deleted: nothing else reports them. One
// whose parent chain leads to another gateway now is that gateway's to report,
// one that reaches no OnPremEventGateway anymore, e.g. now under a
// KonnectEventGateway, loses the conditions the controller set, and one whose
// Programmed condition someone else set since, or that is being deleted, is
// left alone. One that fails to be read or patched is kept in the objects
// reported, to be tried again.
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
		programmed := string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType)
		if !obj.GetDeletionTimestamp().IsZero() || !ownsEntityCondition(obj, programmed) {
			continue
		}
		reached, ok, missing, err := walkToOnPremEventGateway(ctx, r.Client, obj)
		switch {
		case err != nil:
			current[e] = struct{}{}
			errs = append(errs, err)
			continue
		case ok && reached != gatewayKey:
			continue
		case !ok && missing == "":
			err = r.setUnownedEntityConditions(ctx, obj, []string{programmed, string(konnectv1alpha1.SecretRefValidConditionType)})
		default:
			err = r.setUnownedEntityConditions(ctx, obj, nil, entityCondition(obj, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType,
				metav1.ConditionFalse, eventgatewayv1alpha1.OnPremEntityDetachedReason,
				fmt.Sprintf("No longer in the configuration of the OnPremEventGateway %s: it doesn't belong to it anymore, e.g. its parent was deleted", gatewayKey)))
		}
		if err != nil {
			current[e] = struct{}{}
			errs = append(errs, err)
		}
	}
	r.reported.Store(gateway.UID, current)
	return errors.Join(errs...)
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
	conditions := slices.Clone(e.conditions)
	if e.secretRefValid == nil {
		return r.setEntityConditions(ctx, e.obj, []string{string(konnectv1alpha1.SecretRefValidConditionType)}, append(conditions, programmed)...)
	}
	return r.setEntityConditions(ctx, e.obj, nil, append(conditions, *e.secretRefValid, programmed)...)
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

// entityStatusFieldManager is the field manager the controller applies the
// status of the entities with: the conditions it owns are those it applied,
// whatever their reason or message. It reads them from the managed fields of
// the entities, so the cache of the manager must keep them: a cache transform
// stripping them, e.g. cache.TransformStripManagedFields, would leave every
// condition unowned.
const entityStatusFieldManager = "kong-operator-onprem-eventgateway"

// setEntityConditions sets the conditions on an entity of a gateway, whose
// status the controller owns, and removes those of the removed types it set.
// What else is in its status was set by the Konnect reconciler, e.g. while its
// on-prem ancestor did not exist yet, or under a Konnect gateway before it
// moved: it is cleared, but for the conditions of the types set, which the
// controller takes over.
func (r *Reconciler) setEntityConditions(ctx context.Context, obj entityObject, removed []string, conds ...metav1.Condition) error {
	desired := desiredEntityConditions(obj, removed, conds)
	if err := r.clearForeignStatus(ctx, obj, desired); err != nil {
		return err
	}
	return r.applyEntityConditions(ctx, obj, true, desired)
}

// setUnownedEntityConditions sets the conditions on an entity that may not be
// of a gateway, e.g. one detached from it, which may be under a Konnect
// gateway now, and removes those of the removed types it set: what someone
// else set is left alone, and a condition someone else took over since isn't
// set.
func (r *Reconciler) setUnownedEntityConditions(ctx context.Context, obj entityObject, removed []string, conds ...metav1.Condition) error {
	err := r.applyEntityConditions(ctx, obj, false, desiredEntityConditions(obj, removed, conds))
	if apierrors.IsConflict(err) {
		// Someone else set one of them since: it is theirs.
		return nil
	}
	return err
}

// ownsEntityCondition tells whether the controller set the condition of the
// type on the entity, and nobody else changed what it tells since.
func ownsEntityCondition(obj client.Object, conditionType string) bool {
	return entityConditionOwnership(obj)[conditionType]
}

// entityConditionOwnership returns, by type, the conditions of the entity the
// controller applied, from its managed fields: true when it still owns what
// the condition tells, its status, reason and message, false when someone
// else changed some of them since and owns those, while the controller keeps
// the rest, the condition itself included.
func entityConditionOwnership(obj client.Object) map[string]bool {
	ownership := map[string]bool{}
	for _, entry := range obj.GetManagedFields() {
		// The API server records them under the status subresource; only the
		// status fields matter whatever the subresource.
		if entry.Manager != entityStatusFieldManager || entry.Operation != metav1.ManagedFieldsOperationApply || entry.FieldsV1 == nil {
			continue
		}
		var fields struct {
			Status struct {
				Conditions map[string]map[string]json.RawMessage `json:"f:conditions"`
			} `json:"f:status"`
		}
		if err := json.Unmarshal(entry.FieldsV1.GetRawBytes(), &fields); err != nil {
			continue
		}
		for key, owned := range fields.Status.Conditions {
			var item struct {
				Type string `json:"type"`
			}
			rest, ok := strings.CutPrefix(key, "k:")
			if !ok || json.Unmarshal([]byte(rest), &item) != nil || item.Type == "" {
				continue
			}
			_, status := owned["f:status"]
			_, reason := owned["f:reason"]
			_, message := owned["f:message"]
			ownership[item.Type] = ownership[item.Type] || (status && reason && message)
		}
	}
	return ownership
}

// desiredEntityConditions returns the conditions the controller applies on the
// entity: those it set, but for the removed types, with the conditions set,
// whose lastTransitionTime only changes with their status. A condition whose
// status, reason or message someone else changed since is applied as it is:
// left out, the server would remove it, theirs included. So is one of a removed
// type: it is theirs now.
func desiredEntityConditions(obj entityObject, removed []string, conds []metav1.Condition) []metav1.Condition {
	ownership := entityConditionOwnership(obj)
	var desired []metav1.Condition
	for _, c := range obj.GetConditions() {
		owned, applied := ownership[c.Type]
		if applied && (!owned || !slices.Contains(removed, c.Type)) {
			desired = append(desired, c)
		}
	}
	for _, cond := range conds {
		if existing := apimeta.FindStatusCondition(obj.GetConditions(), cond.Type); existing != nil && apimeta.FindStatusCondition(desired, cond.Type) == nil {
			// Someone else's: it is taken over, keeping its lastTransitionTime
			// when the status is the same.
			desired = append(desired, *existing)
		}
		apimeta.SetStatusCondition(&desired, cond)
	}
	return desired
}

// clearForeignStatus removes from the status of an entity the controller owns
// what someone else set: the conditions of other types than the desired ones
// that the controller didn't set, and the fields other than the conditions,
// with an optimistic lock, so that a cache behind the API server doesn't set
// back what someone else set meanwhile.
func (r *Reconciler) clearForeignStatus(ctx context.Context, obj entityObject, desired []metav1.Condition) error {
	ownership := entityConditionOwnership(obj)
	remaining := slices.DeleteFunc(slices.Clone(obj.GetConditions()), func(c metav1.Condition) bool {
		_, applied := ownership[c.Type]
		return !applied && apimeta.FindStatusCondition(desired, c.Type) == nil
	})
	cleared, err := otherStatusFields(obj)
	if err != nil {
		return err
	}
	if len(remaining) == len(obj.GetConditions()) && len(cleared) == 0 {
		return nil
	}
	status := map[string]any{"conditions": remaining}
	for _, field := range cleared {
		status[field] = nil
	}
	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"resourceVersion": obj.GetResourceVersion()},
		"status":   status,
	})
	if err != nil {
		return err
	}
	if err := r.Status().Patch(ctx, obj, client.RawPatch(types.MergePatchType, patch), client.FieldOwner(entityStatusFieldManager)); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to clear the status someone else set on the entity %s: %w", client.ObjectKeyFromObject(obj), err)
	}
	return nil
}

// applyEntityConditions applies the conditions as the ones the controller sets
// on the entity, when they differ from those it set: the server removes those
// it set and no longer applies, and leaves what someone else set. With force,
// it takes over a condition someone else set; without, it returns a conflict.
// An entity deleted meanwhile is left alone.
func (r *Reconciler) applyEntityConditions(ctx context.Context, obj entityObject, force bool, desired []metav1.Condition) error {
	if !entityConditionsChange(obj, desired) {
		return nil
	}
	gvk, err := apiutil.GVKForObject(obj, r.Scheme())
	if err != nil {
		return err
	}
	conditions := make([]any, 0, len(desired))
	for _, c := range desired {
		u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&c)
		if err != nil {
			return err
		}
		conditions = append(conditions, u)
	}
	// The UID is a precondition: an entity deleted, or deleted and created
	// again, since it was read isn't written; its creation triggers its
	// gateway.
	u := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": obj.GetName(), "namespace": obj.GetNamespace(), "uid": string(obj.GetUID())},
		"status":   map[string]any{"conditions": conditions},
	}}
	u.SetGroupVersionKind(gvk)
	opts := []client.SubResourceApplyOption{client.FieldOwner(entityStatusFieldManager)}
	if force {
		opts = append(opts, client.ForceOwnership)
	}
	if err := r.Status().Apply(ctx, client.ApplyConfigurationFromUnstructured(u), opts...); err != nil {
		if apierrors.IsNotFound(err) || (force && apierrors.IsConflict(err)) {
			// Forced, only the UID precondition conflicts.
			return nil
		}
		return fmt.Errorf("failed to set the conditions of the entity %s: %w", client.ObjectKeyFromObject(obj), err)
	}
	return nil
}

// entityConditionsChange tells whether applying the desired conditions changes
// those the controller set on the entity: one differs, someone else set it, or
// the controller set one it no longer applies.
func entityConditionsChange(obj entityObject, desired []metav1.Condition) bool {
	ownership := entityConditionOwnership(obj)
	for _, cond := range desired {
		existing := apimeta.FindStatusCondition(obj.GetConditions(), cond.Type)
		if _, applied := ownership[cond.Type]; existing == nil || !applied ||
			existing.Status != cond.Status || existing.Reason != cond.Reason || existing.Message != cond.Message ||
			existing.ObservedGeneration != cond.ObservedGeneration || !existing.LastTransitionTime.Equal(&cond.LastTransitionTime) {
			return true
		}
	}
	for conditionType := range ownership {
		if apimeta.FindStatusCondition(desired, conditionType) == nil {
			return true
		}
	}
	return false
}

// otherStatusFields returns the fields set in the status of the entity other
// than its conditions.
func otherStatusFields(obj entityObject) ([]string, error) {
	fields, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to read the status of the entity %s: %w", client.ObjectKeyFromObject(obj), err)
	}
	status, _ := fields["status"].(map[string]any)
	var other []string
	for field := range status {
		if field != "conditions" {
			other = append(other, field)
		}
	}
	slices.Sort(other)
	return other, nil
}
