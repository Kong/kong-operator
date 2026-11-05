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
	"errors"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway/changenotifier"
)

// -----------------------------------------------------------------------------
// KongReferenceGrant - Reconciler
// -----------------------------------------------------------------------------

// KongReferenceGrantReconciler watches the KongReferenceGrants and feeds the
// ChangeNotifier when they change, because the translation reads the grants on
// each render: without this watcher a new or revoked grant has no effect until an
// unrelated entity event arrives. A grant matters in two ways:
//
//   - one in the namespace of a Secret referenced from another namespace decides
//     whether a secretRef resolves, so the entities carrying that secretRef must
//     re-render;
//   - one in the gateway's own namespace decides whether an entity of another
//     namespace may reference that gateway at all, so the gateway's configuration
//     must re-render.
//
// The reconciler runs on the instance's own manager, whose KongReferenceGrant cache
// holds the grants of the gateway's watch namespaces (see
// pkg/multiinstance/aigateway's cacheOpts): that covers both cases, since the
// gateway's namespace is always watched and a cross-namespace secretRef can only
// resolve against a Secret of a watched namespace.
type KongReferenceGrantReconciler struct {
	client.Client

	Log            logr.Logger
	ChangeNotifier *changenotifier.ChangeNotifier
}

// SetupWithManager sets up the controller with the Manager.
func (r *KongReferenceGrantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("aiconfigurationv1alpha1KongReferenceGrant").
		For(&configurationv1alpha1.KongReferenceGrant{}).
		Complete(r)
}

// Reconcile notifies the ChangeNotifier for every on-prem gateway of the grant's
// namespace, and for every configuration entity with a cross-namespace secretRef
// into the grant's namespace. The grant itself is not read: on deletion its spec is
// gone, and the translation decides access anyway.
func (r *KongReferenceGrantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, errors.Join(
		r.notifyGateways(ctx, req.Namespace),
		notifySecretRefEntities(ctx, r.Client, crossNamespaceSecretRefInto(req.Namespace), r.ChangeNotifier, r.Log),
	)
}

// notifyGateways notifies every on-prem gateway of namespace: a grant there may
// allow, or no longer allow, entities of another namespace to reference it.
func (r *KongReferenceGrantReconciler) notifyGateways(ctx context.Context, namespace string) error {
	if r.ChangeNotifier == nil {
		return nil
	}
	// Listed across the whole cache, not with InNamespace: the instance's cache holds
	// the gateway's namespace only, and a list scoped to the grant's namespace fails
	// with "unknown namespace for the cache" for every cross-namespace grant.
	var gateways aigatewayv1alpha1.OnPremAIGatewayList
	if err := r.List(ctx, &gateways); err != nil {
		r.Log.Error(err, "Failed to list OnPremAIGateways of a namespace with a KongReferenceGrant", "namespace", namespace)
		return err
	}
	for i := range gateways.Items {
		gw := &gateways.Items[i]
		if gw.Namespace != namespace {
			continue
		}
		gwNN := client.ObjectKeyFromObject(gw)
		r.ChangeNotifier.NotifyChange(ctx, &gwNN, gw)
	}
	return nil
}

// crossNamespaceSecretRefInto matches the secretRefs that point at a Secret in
// namespace from an entity in a different namespace.
func crossNamespaceSecretRefInto(namespace string) secretRefMatcher {
	return func(entityNamespace string, ref aiconfigurationv1alpha1.SensitiveDataSecretRef) bool {
		return entityNamespace != namespace && secretRefNamespace(entityNamespace, ref) == namespace
	}
}
