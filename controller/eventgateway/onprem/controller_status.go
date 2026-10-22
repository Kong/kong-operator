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
	"strings"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
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
		message := "Waiting for the gw-admin pods not serving yet: " + podNames(result.pending)
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
		message += "; waiting for the gw-admin pods not serving yet: " + podNames(result.pending)
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
