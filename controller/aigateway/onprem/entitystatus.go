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
	"slices"
	"strings"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/instances"
)

// watchedNamespacesAnnotation records the namespace set the running instance was
// started with, so a later reconcile sees that the set narrowed. An entity
// outside the set is never listed again by the instance, so its Programmed
// condition would stay whatever it was.
const watchedNamespacesAnnotation = "aigateway.konghq.com/watched-namespaces"

// watchedNamespacesAll is the recorded value for a gateway watching every namespace.
const watchedNamespacesAll = "*"

// outOfScopeEntity is an AI Gateway configuration entity whose Programmed
// condition reports whether it is part of the configuration pushed to a
// gateway's data planes.
type outOfScopeEntity interface {
	client.Object
	GetConditions() []metav1.Condition
	SetConditions([]metav1.Condition)
}

// gatewayTargetedEntity is an outOfScopeEntity that references its AI Gateway
// directly. A credential carries no gateway reference: its gateway is only
// reachable through the AIGatewayConsumer it references.
type gatewayTargetedEntity interface {
	outOfScopeEntity
	GetAIGatewayRef() aiconfigurationv1alpha1.AIGatewayRef
}

// syncWatchedNamespaces records the namespaces the instance watches in the
// gateway's annotations. When the recorded set narrows, it stops the instance
// and clears the Programmed condition of the entities the new set leaves out:
// the instance lists the entities through caches built from the watch set at
// start, so nothing else ever reports them again.
func (r *Reconciler) syncWatchedNamespaces(
	ctx context.Context,
	logger logr.Logger,
	mgrID manager.ID,
	onprem *aigatewayv1alpha1.OnPremAIGateway,
	watchNamespaces []string,
) error {
	value := watchedNamespacesAnnotationValue(watchNamespaces)
	recorded, hadRecord := onprem.Annotations[watchedNamespacesAnnotation]
	if hadRecord && recorded == value {
		return nil
	}

	// A missing record counts as a record of every namespace: a client that replaced
	// the object may have dropped it, and the entities the current set leaves out
	// would otherwise keep their status for good.
	if watchSetShrank(recordedWatchedNamespaces(recorded), watchNamespaces) {
		// Stop the instance before clearing the statuses: it still caches the
		// left-out namespaces, so its next sync would report their entities as
		// Programmed again.
		if err := r.InstancesManager.StopInstance(mgrID); err != nil {
			if _, ok := errors.AsType[instances.InstanceNotFoundError](err); !ok {
				return fmt.Errorf("failed to stop instance: %w", err)
			}
		}
		// The set is recorded only once the statuses are cleared: a failed sweep
		// leaves the record untouched, so the next reconcile retries it.
		if err := r.clearOutOfScopeEntityStatus(ctx, client.ObjectKeyFromObject(onprem), watchNamespaces); err != nil {
			return err
		}
	}

	// Patch a copy: the client decodes the API server's response into the object
	// passed to Patch, which would replace the conditions set earlier in this
	// reconcile with the stored ones.
	updated := onprem.DeepCopy()
	if updated.Annotations == nil {
		updated.Annotations = map[string]string{}
	}
	updated.Annotations[watchedNamespacesAnnotation] = value
	if err := r.Patch(ctx, updated, client.MergeFrom(onprem)); err != nil {
		return fmt.Errorf("recording the watched namespaces: %w", err)
	}
	log.Debug(logger, "recorded the watched namespaces", "watch_namespaces", value)
	return nil
}

// clearOutOfScopeEntityStatus sets the Programmed condition of every entity that
// references the gateway from a namespace it does not watch to False. The
// entities are listed through the cache: those namespaces are exactly the ones
// the instance's caches leave out, and they stay inside the operator's own
// --watch-namespaces scope, which a gateway's watch set cannot exceed. The
// informers of the entity kinds start on the first list, that is, when a gateway
// first narrows its watch set.
func (r *Reconciler) clearOutOfScopeEntityStatus(
	ctx context.Context,
	gwNN k8stypes.NamespacedName,
	watchNamespaces []string,
) error {
	// A nil set means every namespace is watched: no entity is out of scope.
	if watchNamespaces == nil {
		return nil
	}

	for _, list := range onPremAIEntityLists() {
		if err := r.List(ctx, list); err != nil {
			return fmt.Errorf("listing %T to clear the status of out-of-scope entities: %w", list, err)
		}
		items, err := meta.ExtractList(list)
		if err != nil {
			return fmt.Errorf("extracting the items of %T: %w", list, err)
		}
		for _, item := range items {
			ent, ok := item.(outOfScopeEntity)
			if !ok || slices.Contains(watchNamespaces, ent.GetNamespace()) {
				continue
			}
			targets, err := r.entityTargetsOnPremAIGateway(ctx, ent, gwNN)
			if err != nil {
				return err
			}
			if !targets {
				continue
			}
			if err := r.markEntityOutOfScope(ctx, ent, gwNN); err != nil {
				return err
			}
		}
	}
	return nil
}

// markEntityOutOfScope sets the entity's Programmed condition to False, unless
// it already reports it.
func (r *Reconciler) markEntityOutOfScope(
	ctx context.Context,
	ent outOfScopeEntity,
	gwNN k8stypes.NamespacedName,
) error {
	conditions := slices.Clone(ent.GetConditions())
	changed := meta.SetStatusCondition(&conditions, metav1.Condition{
		Type:               string(configurationv1.ConditionProgrammed),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: ent.GetGeneration(),
		Reason:             string(configurationv1.ReasonInvalid),
		Message: fmt.Sprintf(
			"Its namespace %s is not watched by the OnPremAIGateway %s: the entity is not part of the pushed configuration",
			ent.GetNamespace(), gwNN,
		),
	})
	if !changed {
		return nil
	}

	old, ok := ent.DeepCopyObject().(client.Object)
	if !ok {
		return fmt.Errorf("failed to copy %T", ent)
	}
	ent.SetConditions(conditions)
	if err := r.Status().Patch(ctx, ent, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil &&
		!apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to set the conditions of %T %s: %w", ent, client.ObjectKeyFromObject(ent), err)
	}
	return nil
}

// entityTargetsOnPremAIGateway reports whether the entity belongs to the given
// gateway. A credential is resolved through the consumer it references, whose
// reference the on-prem translation requires to live in the credential's own
// namespace.
func (r *Reconciler) entityTargetsOnPremAIGateway(
	ctx context.Context,
	ent outOfScopeEntity,
	gwNN k8stypes.NamespacedName,
) (bool, error) {
	if cred, ok := ent.(*aiconfigurationv1alpha1.AIGatewayConsumerCredential); ok {
		consumerNN, ok := cred.ConsumerNamespacedName()
		if !ok {
			return false, nil
		}
		var consumer aiconfigurationv1alpha1.AIGatewayConsumer
		if err := r.Get(ctx, consumerNN, &consumer); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		return targetsOnPremAIGateway(&consumer, gwNN), nil
	}
	targeted, ok := ent.(gatewayTargetedEntity)
	return ok && targetsOnPremAIGateway(targeted, gwNN), nil
}

// targetsOnPremAIGateway reports whether the entity's aiGatewayRef resolves to
// the given gateway, with the unset reference namespace defaulting to the
// entity's own.
func targetsOnPremAIGateway(ent gatewayTargetedEntity, gwNN k8stypes.NamespacedName) bool {
	ref := ent.GetAIGatewayRef()
	if ref.NamespacedRef == nil || !ref.TargetsOnPremAIGateway() {
		return false
	}
	ns := ent.GetNamespace()
	if ref.NamespacedRef.Namespace != nil && *ref.NamespacedRef.Namespace != "" {
		ns = *ref.NamespacedRef.Namespace
	}
	return ns == gwNN.Namespace && ref.NamespacedRef.Name == gwNN.Name
}

// onPremAIEntityLists returns an empty list of every AI Gateway configuration
// entity kind. Keep in sync with the reconcilers onpremconfig registers.
func onPremAIEntityLists() []client.ObjectList {
	return []client.ObjectList{
		&aiconfigurationv1alpha1.AIGatewayAgentList{},
		&aiconfigurationv1alpha1.AIGatewayAuthStrategyList{},
		&aiconfigurationv1alpha1.AIGatewayCACertificateList{},
		&aiconfigurationv1alpha1.AIGatewayCertificateList{},
		&aiconfigurationv1alpha1.AIGatewayConsumerList{},
		&aiconfigurationv1alpha1.AIGatewayConsumerCredentialList{},
		&aiconfigurationv1alpha1.AIGatewayConsumerGroupList{},
		&aiconfigurationv1alpha1.AIGatewayCustomPolicyList{},
		&aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateList{},
		&aiconfigurationv1alpha1.AIGatewayMCPServerList{},
		&aiconfigurationv1alpha1.AIGatewayModelList{},
		&aiconfigurationv1alpha1.AIGatewayModelProviderList{},
		&aiconfigurationv1alpha1.AIGatewayPolicyList{},
		&aiconfigurationv1alpha1.AIGatewaySNIList{},
		&aiconfigurationv1alpha1.AIGatewayVaultList{},
	}
}

// watchedNamespacesAnnotationValue renders a resolved watch set for the annotation.
func watchedNamespacesAnnotationValue(watchNamespaces []string) string {
	if watchNamespaces == nil {
		return watchedNamespacesAll
	}
	return strings.Join(watchNamespaces, ",")
}

// recordedWatchedNamespaces parses a recorded watch set. A nil result means
// every namespace.
func recordedWatchedNamespaces(recorded string) []string {
	if recorded == watchedNamespacesAll || recorded == "" {
		return nil
	}
	return strings.Split(recorded, ",")
}

// watchSetShrank reports whether the current set leaves out a namespace the
// previous one watched. A nil set means every namespace.
func watchSetShrank(previous, current []string) bool {
	if previous == nil {
		return current != nil
	}
	if current == nil {
		return false
	}
	for _, ns := range previous {
		if !slices.Contains(current, ns) {
			return true
		}
	}
	return false
}
