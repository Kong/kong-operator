package konnect

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// listSyncsForSecret maps a Secret to every KonnectConfigStoreSync referencing
// it, so Secret rotations propagate without waiting for the periodic resync.
func (r *KonnectConfigStoreSyncReconciler) listSyncsForSecret(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	var list konnectv1alpha1.KonnectConfigStoreSyncList
	if err := r.List(ctx, &list, client.MatchingFields{
		index.IndexFieldKonnectConfigStoreSyncOnSecret: obj.GetNamespace() + "/" + obj.GetName(),
	}); err != nil {
		return nil
	}
	return reconcileRequestsForSyncs(list.Items)
}

// listSyncsForConfigStore maps a KonnectConfigStore to every sync referencing
// it, so a store becoming programmed (or deleted) triggers the syncs waiting
// on it.
func (r *KonnectConfigStoreSyncReconciler) listSyncsForConfigStore(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	var list konnectv1alpha1.KonnectConfigStoreSyncList
	if err := r.List(ctx, &list, client.MatchingFields{
		index.IndexFieldKonnectConfigStoreSyncOnConfigStore: obj.GetNamespace() + "/" + obj.GetName(),
	}); err != nil {
		return nil
	}
	return reconcileRequestsForSyncs(list.Items)
}

// listSyncsForReferenceGrant maps a KongReferenceGrant to every sync whose
// resolved Secret or Config Store reference points into the grant's
// namespace: a grant change can permit or revoke a cross-namespace reference.
// It over-enqueues (every sync referencing the namespace, granted or not);
// the reconcile re-checks the grant, so this is safe.
func (r *KonnectConfigStoreSyncReconciler) listSyncsForReferenceGrant(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	var list konnectv1alpha1.KonnectConfigStoreSyncList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for i := range list.Items {
		sync := &list.Items[i]
		secretNN := resolveNamespacedRef(sync.Namespace, sync.Spec.SecretRef)
		storeNN := resolveNamespacedRef(sync.Namespace, sync.Spec.ConfigStoreRef)
		if secretNN.Namespace == obj.GetNamespace() || storeNN.Namespace == obj.GetNamespace() {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(sync),
			})
		}
	}
	return requests
}

func reconcileRequestsForSyncs(syncs []konnectv1alpha1.KonnectConfigStoreSync) []reconcile.Request {
	requests := make([]reconcile.Request, 0, len(syncs))
	for i := range syncs {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&syncs[i]),
		})
	}
	return requests
}
