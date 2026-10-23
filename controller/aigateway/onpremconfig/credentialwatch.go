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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// mapConsumerToCredentials maps an AIGatewayConsumer to the AIGatewayConsumerCredentials that
// reference it. A deleted consumer's credentials are in no sync's status set — Report only
// republishes the current sync's entities — so nothing else would re-trigger them, and their
// Programmed condition would keep claiming a rendering that no longer exists.
func (r *AIGatewayConsumerCredentialReconciler) mapConsumerToCredentials(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	consumer, ok := obj.(*aiconfigurationv1alpha1.AIGatewayConsumer)
	if !ok {
		return nil
	}
	var creds aiconfigurationv1alpha1.AIGatewayConsumerCredentialList
	if err := r.List(ctx, &creds, client.MatchingFields{
		index.IndexFieldAIGatewayConsumerCredentialOnAIGatewayConsumerRef: client.ObjectKeyFromObject(consumer).String(),
	}); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(creds.Items))
	for i := range creds.Items {
		reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&creds.Items[i])})
	}
	return reqs
}

// mapCredentialToConsumer maps an AIGatewayConsumerCredential to the AIGatewayConsumer whose
// document embeds it. The credential has no aiGatewayRef, so the consumer is what must
// re-render when a credential appears, changes, is repointed or is deleted.
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
	consumer := credentialConsumer(ctx, r.Client, cred)
	if consumer == nil {
		return nil
	}
	// Konnect-targeted consumers are owned by the Konnect reconciler. The For predicate does
	// not apply to a Watches source, so it is enforced here: reconciling one would report an
	// on-prem configuration status on a consumer this controller does not own.
	if !consumerTargetsOnPremAIGateway(consumer) {
		return nil
	}
	return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(consumer)}}
}

// credentialConsumer returns the AIGatewayConsumer a credential references, or nil when the
// reference cannot be resolved: an unresolvable one either points at another namespace (the
// translation only renders credentials living in the consumer's namespace), carries no
// namespacedRef (the CRD also admits type: konnectID), or names a consumer that is gone, or
// that the cache does not serve yet.
func credentialConsumer(
	ctx context.Context,
	cl client.Client,
	cred *aiconfigurationv1alpha1.AIGatewayConsumerCredential,
) *aiconfigurationv1alpha1.AIGatewayConsumer {
	ref := cred.GetAIGatewayConsumerRef().NamespacedRef
	if ref == nil {
		return nil
	}
	consumerNN := types.NamespacedName{Namespace: cred.GetNamespace(), Name: ref.Name}
	if ref.Namespace != nil && *ref.Namespace != "" {
		consumerNN.Namespace = *ref.Namespace
	}
	if consumerNN.Namespace != cred.GetNamespace() {
		return nil
	}

	var consumer aiconfigurationv1alpha1.AIGatewayConsumer
	if err := cl.Get(ctx, consumerNN, &consumer); err != nil {
		return nil
	}
	return &consumer
}

// consumerTargetsOnPremAIGateway reports whether the consumer's aiGatewayRef resolves to an
// OnPremAIGateway. CRD validation requires the namespacedRef, but a cached copy can predate a
// default, so it is checked here too.
func consumerTargetsOnPremAIGateway(consumer *aiconfigurationv1alpha1.AIGatewayConsumer) bool {
	gwRef := consumer.GetAIGatewayRef()
	return gwRef.NamespacedRef != nil && gwRef.TargetsOnPremAIGateway()
}

// credentialOnPremManaged reports whether the credential is rendered by a consumer targeting
// an OnPremAIGateway, i.e. whether the on-prem machinery owns its configuration status.
func credentialOnPremManaged(
	ctx context.Context,
	cl client.Client,
	cred *aiconfigurationv1alpha1.AIGatewayConsumerCredential,
) bool {
	consumer := credentialConsumer(ctx, cl, cred)
	return consumer != nil && consumerTargetsOnPremAIGateway(consumer)
}

// credentialOrphaned reports whether the credential's consumer is gone. It is deliberately
// narrower than "not managed by on-prem": a credential whose ref is not a namespacedRef, or
// points at another namespace, may be Konnect-managed, and the Konnect reconciler owns
// correcting the status of those. Only a consumer that should exist in the credential's own
// namespace and does not leaves the credential rendered by nothing.
//
// Only a definitive NotFound counts as gone: a transient read failure (the cache does not
// serve a just recreated consumer yet) must not strip a valid condition, since Report
// republishes the credential only when its status or message changes.
func credentialOrphaned(
	ctx context.Context,
	cl client.Client,
	cred *aiconfigurationv1alpha1.AIGatewayConsumerCredential,
) bool {
	ref := cred.GetAIGatewayConsumerRef().NamespacedRef
	if ref == nil || (ref.Namespace != nil && *ref.Namespace != "" && *ref.Namespace != cred.GetNamespace()) {
		return false
	}
	var consumer aiconfigurationv1alpha1.AIGatewayConsumer
	return apierrors.IsNotFound(cl.Get(ctx, types.NamespacedName{
		Namespace: cred.GetNamespace(),
		Name:      ref.Name,
	}, &consumer))
}
