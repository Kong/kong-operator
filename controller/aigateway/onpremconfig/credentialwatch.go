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

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
)

// mapCredentialToConsumer maps an AIGatewayConsumerCredential to the AIGatewayConsumer whose
// document embeds it. The credential has no aiGatewayRef and no reconciler of its own, so the
// consumer is what must re-render when a credential appears, changes, is repointed or is
// deleted.
//
// The request is derived from obj alone, never from state kept elsewhere: controller-runtime
// maps an update event for both the old and the new object, so a credential repointed at
// another consumer re-renders the consumer it left as well.
func (r *AIGatewayConsumerReconciler) mapCredentialToConsumer(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	cred, ok := obj.(*aiconfigurationv1alpha1.AIGatewayConsumerCredential)
	if !ok {
		return nil
	}
	ref := cred.GetAIGatewayConsumerRef().NamespacedRef
	// The CRD also admits type: konnectID, which carries no namespacedRef.
	if ref == nil {
		return nil
	}
	consumerNN := types.NamespacedName{Namespace: cred.GetNamespace(), Name: ref.Name}
	if ref.Namespace != nil && *ref.Namespace != "" {
		consumerNN.Namespace = *ref.Namespace
	}
	// The translation only renders credentials living in the consumer's namespace.
	if consumerNN.Namespace != cred.GetNamespace() {
		return nil
	}

	var consumer aiconfigurationv1alpha1.AIGatewayConsumer
	if err := r.Get(ctx, consumerNN, &consumer); err != nil {
		// The consumer is gone, or the cache does not serve it yet: nothing to re-render.
		return nil
	}
	// Konnect-targeted consumers are owned by the Konnect reconciler. The For predicate does
	// not apply to a Watches source, so it is enforced here: reconciling one would report an
	// on-prem configuration status on a consumer this controller does not own.
	gwRef := consumer.GetAIGatewayRef()
	if gwRef.NamespacedRef == nil || !gwRef.TargetsOnPremAIGateway() {
		return nil
	}
	return []reconcile.Request{{NamespacedName: consumerNN}}
}
