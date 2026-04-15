/*
Copyright 2025 Kong, Inc.

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

package dataplane

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// resolveKonnectEventGateway resolves the KonnectEventGateway referenced by the
// DataPlane. It sets the KonnectEventGatewayResolved condition on the
// EGDP and returns the resolved KonnectEventGateway if successful.
func (r *Reconciler) resolveKonnectEventGateway(
	ctx context.Context,
	logger logr.Logger,
	egdp *eventgatewayv1alpha1.KegDataPlane,
) (*konnectv1alpha1.KonnectEventControlPlane, error) {
	keg := &konnectv1alpha1.KonnectEventControlPlane{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      egdp.Spec.ControlPlaneRef.KonnectNamespacedRef.Name,
		Namespace: egdp.Namespace,
	}, keg)

	if apierrors.IsNotFound(err) {
		log.Debug(logger, "referenced KonnectEventGateway not found",
			"ref", egdp.Spec.ControlPlaneRef.KonnectNamespacedRef.Name)

		k8sutils.SetCondition(metav1.Condition{
			Type:               string(eventgatewayv1alpha1.KonnectEventGatewayResolvedType),
			Status:             metav1.ConditionFalse,
			Reason:             string(eventgatewayv1alpha1.KonnectEventGatewayNotFoundReason),
			Message:            eventgatewayv1alpha1.KonnectEventGatewayNotFoundMessage,
			ObservedGeneration: egdp.Generation,
			LastTransitionTime: metav1.Now(),
		}, egdp)

		return nil, err
	}
	if err != nil {
		return nil, err
	}

	// Check that the KonnectEventGateway is Programmed (i.e. exists in Konnect).
	if !apimeta.IsStatusConditionTrue(keg.Status.Conditions, konnectv1alpha1.KonnectEntityProgrammedConditionType) {
		log.Debug(logger, "referenced KonnectEventGateway is not yet Programmed",
			"ref", egdp.Spec.ControlPlaneRef.KonnectNamespacedRef.Name)

		k8sutils.SetCondition(metav1.Condition{
			Type:               string(eventgatewayv1alpha1.KonnectEventGatewayResolvedType),
			Status:             metav1.ConditionFalse,
			Reason:             string(eventgatewayv1alpha1.KonnectEventGatewayNotProgrammedReason),
			Message:            eventgatewayv1alpha1.KonnectEventGatewayNotProgrammedMessage,
			ObservedGeneration: egdp.Generation,
			LastTransitionTime: metav1.Now(),
		}, egdp)

		return nil, fmt.Errorf("referenced KonnectEventControlPlane %q is not yet Programmed",
			egdp.Spec.ControlPlaneRef.KonnectNamespacedRef.Name)
	}

	k8sutils.SetCondition(metav1.Condition{
		Type:               string(eventgatewayv1alpha1.KonnectEventGatewayResolvedType),
		Status:             metav1.ConditionTrue,
		Reason:             string(eventgatewayv1alpha1.KonnectEventGatewayResolvedReason),
		Message:            eventgatewayv1alpha1.KonnectEventGatewayResolvedMessage,
		ObservedGeneration: egdp.Generation,
		LastTransitionTime: metav1.Now(),
	}, egdp)

	return keg, nil
}
