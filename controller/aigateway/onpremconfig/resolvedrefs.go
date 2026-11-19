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

package onpremconfig

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway/translator"
)

// -----------------------------------------------------------------------------
// ResolvedRefs condition
// -----------------------------------------------------------------------------

// ensureResolvedRefsCondition sets the ResolvedRefs condition of an entity with
// cross-namespace references, with the same type and reasons as the Konnect path
// (controller/konnect reconciler_secretref.go handleSecretRef and
// reconciler_generic_handle_parentref.go). The cross-namespace references are the
// aiGatewayRef to an OnPremAIGateway of another namespace, the secretRefs to
// Secrets of another namespace, the consumer group ACL references to an
// AIGatewayConsumerGroup of another namespace, and the AIGatewayConsumerRef of an
// AIGatewayConsumerCredential to a consumer of another namespace. The condition is
// removed when the entity has none, so that a switch to same-namespace references
// does not keep a stale condition.
//
// The condition only reports the KongReferenceGrant checks, done with the same
// functions as the translation. The aiGatewayRef is checked first: when it is
// denied, the whole entity is left out of the configuration. The other failures
// of the entity (for example a missing Secret) are reported by the Programmed
// condition.
//
// It returns true when conditions changed. An error other than a denied
// reference (for example a failed list of the KongReferenceGrants) leaves
// conditions unchanged.
func ensureResolvedRefsCondition(
	ctx context.Context,
	cl client.Client,
	obj client.Object,
	generation int64,
	conditions *[]metav1.Condition,
) (bool, error) {
	gw, crossNamespaceGateway := onPremAIGatewayOf(obj)
	crossNamespaceGateway = crossNamespaceGateway && gw.Namespace != obj.GetNamespace()
	secretRefsOwner, ok := obj.(aiconfigurationv1alpha1.SecretRefsOwner)
	crossNamespaceSecrets := ok && hasCrossNamespaceSecretRefs(secretRefsOwner)
	cred, _ := obj.(*aiconfigurationv1alpha1.AIGatewayConsumerCredential)
	crossNamespaceConsumer := cred != nil && cred.HasCrossNamespaceConsumerRef()
	crossNamespaceACLs := aiconfigurationv1alpha1.HasCrossNamespaceConsumerGroupRefs(obj)
	if !crossNamespaceGateway && !crossNamespaceSecrets && !crossNamespaceConsumer && !crossNamespaceACLs {
		return meta.RemoveStatusCondition(conditions, configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs), nil
	}

	condition := metav1.Condition{
		Type:               configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs,
		Status:             metav1.ConditionTrue,
		Reason:             configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
		Message:            "KongReferenceGrants allow the cross-namespace references",
		ObservedGeneration: generation,
	}
	var err error
	if crossNamespaceGateway {
		err = translator.CheckCrossNamespaceRef(ctx, cl, obj, gw)
	}
	if err == nil && crossNamespaceSecrets {
		err = aiconfigurationv1alpha1.CheckCrossNamespaceSecretRefsGranted(ctx, cl, secretRefsOwner)
	}
	if err == nil && crossNamespaceConsumer {
		err = aiconfigurationv1alpha1.CheckCrossNamespaceConsumerRefGranted(ctx, cl, cred)
	}
	if err == nil && crossNamespaceACLs {
		err = aiconfigurationv1alpha1.CheckCrossNamespaceConsumerGroupRefsGranted(ctx, cl, obj)
	}
	if err != nil {
		if !crossnamespace.IsReferenceNotGranted(err) {
			return false, err
		}
		condition.Status = metav1.ConditionFalse
		condition.Reason = configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted
		condition.Message = err.Error()
	}
	return meta.SetStatusCondition(conditions, condition), nil
}

// onPremAIGatewayOf returns the OnPremAIGateway the entity's aiGatewayRef
// targets. It returns false for an entity without an aiGatewayRef (for example
// an AIGatewayConsumerCredential) or one that does not target an OnPremAIGateway.
func onPremAIGatewayOf(obj client.Object) (types.NamespacedName, bool) {
	getter, ok := obj.(aiGatewayRefGetter)
	if !ok {
		return types.NamespacedName{}, false
	}
	ref := getter.GetAIGatewayRef()
	if ref.NamespacedRef == nil || !ref.TargetsOnPremAIGateway() {
		return types.NamespacedName{}, false
	}
	gw := types.NamespacedName{Namespace: obj.GetNamespace(), Name: ref.NamespacedRef.Name}
	if ns := ref.NamespacedRef.Namespace; ns != nil && *ns != "" {
		gw.Namespace = *ns
	}
	return gw, true
}

// hasCrossNamespaceSecretRefs reports whether one of the entity's secretRefs
// points at a Secret in another namespace.
func hasCrossNamespaceSecretRefs(obj aiconfigurationv1alpha1.SecretRefsOwner) bool {
	for _, ref := range obj.GetSensitiveDataSecretRefs() {
		if secretRefNamespace(obj.GetNamespace(), ref) != obj.GetNamespace() {
			return true
		}
	}
	return false
}

// mapKongReferenceGrantToEntities returns a map function that enqueues the
// entities of one kind with a cross-namespace reference that a
// KongReferenceGrant in its namespace governs, so that their ResolvedRefs
// condition follows a grant that is created, changed or deleted: an aiGatewayRef
// to an OnPremAIGateway of the grant's namespace, a secretRef to a Secret of
// the grant's namespace, a consumer group ACL reference to an
// AIGatewayConsumerGroup of the grant's namespace, or an AIGatewayConsumerRef to a
// consumer of the grant's namespace. The grant itself is not read: on deletion its
// spec is gone, and the reconciler checks the grants again.
//
// For kinds with an aiGatewayRef, only the entities that target an
// OnPremAIGateway are enqueued: the For predicate of the reconciler does not
// apply to a Watches source, and the entities of a KonnectAIGateway are owned
// by the Konnect reconciler.
func mapKongReferenceGrantToEntities[
	TList interface {
		GetItems() []T
	},
	TListPtr interface {
		*TList
		client.ObjectList
		GetItems() []T
	},
	T any,
	TT interface {
		*T
		client.Object
	},
](cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		if _, ok := obj.(*configurationv1alpha1.KongReferenceGrant); !ok {
			return nil
		}
		var (
			l    TList
			lPtr TListPtr = &l
		)
		// A failed list enqueues nothing: the next grant event or the status
		// queue reconciles the entities again.
		if err := cl.List(ctx, lPtr); err != nil {
			return nil
		}
		grantNamespace := obj.GetNamespace()
		var requests []reconcile.Request
		items := lPtr.GetItems()
		for i := range items {
			ent := TT(&items[i])
			if governedByGrantsIn(ent, grantNamespace) {
				requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(ent)})
			}
		}
		return requests
	}
}

// governedByGrantsIn reports whether the KongReferenceGrants of namespace decide
// one of the entity's cross-namespace references.
func governedByGrantsIn(ent client.Object, namespace string) bool {
	if getter, ok := ent.(aiGatewayRefGetter); ok && !getter.GetAIGatewayRef().TargetsOnPremAIGateway() {
		return false
	}
	gw, ok := onPremAIGatewayOf(ent)
	if ok && gw.Namespace == namespace && ent.GetNamespace() != namespace {
		return true
	}
	if aiconfigurationv1alpha1.HasCrossNamespaceConsumerGroupRefsInto(ent, namespace) {
		return true
	}
	if cred, ok := ent.(*aiconfigurationv1alpha1.AIGatewayConsumerCredential); ok &&
		cred.HasCrossNamespaceConsumerRefInto(namespace) {
		return true
	}
	owner, ok := ent.(aiconfigurationv1alpha1.SecretRefsOwner)
	if !ok {
		return false
	}
	match := crossNamespaceSecretRefInto(namespace)
	for _, ref := range owner.GetSensitiveDataSecretRefs() {
		if match(ent.GetNamespace(), ref) {
			return true
		}
	}
	return false
}
