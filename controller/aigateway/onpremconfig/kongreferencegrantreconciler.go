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

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway/changenotifier"
)

// -----------------------------------------------------------------------------
// KongReferenceGrant - Reconciler
// -----------------------------------------------------------------------------

// KongReferenceGrantReconciler watches the KongReferenceGrants that allow
// cross-namespace secretRefs and feeds the ChangeNotifier when they change. The
// translation checks the grants on each render, so without this watcher a new or
// revoked grant has no effect until an unrelated entity event arrives.
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

// Reconcile notifies the ChangeNotifier for every configuration entity with a
// cross-namespace secretRef into the grant's namespace. The grant is not fetched:
// on deletion its spec is gone, and the translation decides access anyway.
func (r *KongReferenceGrantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, notifySecretRefEntities(ctx, r.Client, crossNamespaceSecretRefInto(req.Namespace), r.ChangeNotifier, r.Log)
}

// crossNamespaceSecretRefInto matches the secretRefs that point at a Secret in
// namespace from an entity in a different namespace.
func crossNamespaceSecretRefInto(namespace string) secretRefMatcher {
	return func(entityNamespace string, ref aiconfigurationv1alpha1.SensitiveDataSecretRef) bool {
		return entityNamespace != namespace && secretRefNamespace(entityNamespace, ref) == namespace
	}
}
