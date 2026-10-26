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
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
)

// onPremAIGatewayKind is the kind the WatchNamespaceGrant's "from" entries refer
// to when they grant an OnPremAIGateway permission to watch a namespace.
const onPremAIGatewayKind = "OnPremAIGateway"

// resolveWatchNamespaces maps spec.watchNamespaces to the concrete list of
// namespaces the control plane instance watches for configuration entities and
// the Secrets they reference. The list is sorted and deduplicated, so it is
// stable for the instance config hash. A nil result means every namespace.
func resolveWatchNamespaces(onprem *aigatewayv1alpha1.OnPremAIGateway) ([]string, error) {
	wn := onprem.Spec.WatchNamespaces
	// The CRD defaults the field to "own", and a nil field means "own" here too
	// for objects written before the default existed.
	if wn == nil || wn.Type == aigatewayv1alpha1.OnPremAIGatewayWatchNamespacesTypeOwn {
		return []string{onprem.Namespace}, nil
	}

	switch wn.Type {
	case aigatewayv1alpha1.OnPremAIGatewayWatchNamespacesTypeAll:
		return nil, nil
	case aigatewayv1alpha1.OnPremAIGatewayWatchNamespacesTypeList:
		// The gateway's own namespace is always watched: it hosts the
		// AIGatewayDataPlanes and the Admin API client certificate Secret.
		namespaces := append(slices.Clone(wn.List), onprem.Namespace)
		slices.Sort(namespaces)
		return slices.Compact(namespaces), nil
	default:
		return nil, fmt.Errorf("unexpected spec.watchNamespaces.type: %q", wn.Type)
	}
}

// validateWatchNamespacesScope validates the gateway's watch namespaces against
// the operator's own watch scope. The instance runs its own manager, but it cannot
// watch what the operator itself is not allowed to see: a gateway must not widen
// the operator's scope. An empty operator list means the operator watches every
// namespace.
func validateWatchNamespacesScope(onprem *aigatewayv1alpha1.OnPremAIGateway, operatorWatchNamespaces []string) error {
	if len(operatorWatchNamespaces) == 0 {
		return nil
	}

	wn := onprem.Spec.WatchNamespaces
	if wn != nil && wn.Type == aigatewayv1alpha1.OnPremAIGatewayWatchNamespacesTypeAll {
		return fmt.Errorf(
			"spec.watchNamespaces is set to 'all', but the operator watches only: %v",
			operatorWatchNamespaces,
		)
	}

	namespaces, err := resolveWatchNamespaces(onprem)
	if err != nil {
		return err
	}
	for _, ns := range namespaces {
		if !slices.Contains(operatorWatchNamespaces, ns) {
			return fmt.Errorf(
				"spec.watchNamespaces requests namespace %q, but the operator watches only: %v",
				ns, operatorWatchNamespaces,
			)
		}
	}
	return nil
}

// requiresWatchNamespaceGrants reports whether the gateway watches any namespace
// other than its own. Those namespaces need a WatchNamespaceGrant. Watching the
// own namespace and watching every namespace need none, mirroring the
// ControlPlane's rule.
func requiresWatchNamespaceGrants(watchNamespaces []string, ownNamespace string) bool {
	return len(watchNamespaces) != 0 &&
		(len(watchNamespaces) != 1 || watchNamespaces[0] != ownNamespace)
}

// validateWatchNamespaceGrants makes sure that every namespace the gateway
// watches outside its own namespace grants it permission with a
// WatchNamespaceGrant.
func (r *Reconciler) validateWatchNamespaceGrants(
	ctx context.Context,
	onprem *aigatewayv1alpha1.OnPremAIGateway,
	watchNamespaces []string,
) error {
	for _, ns := range watchNamespaces {
		if ns == onprem.Namespace {
			continue
		}
		var grants operatorv1alpha1.WatchNamespaceGrantList
		if err := r.List(ctx, &grants, client.InNamespace(ns)); err != nil {
			return fmt.Errorf("listing WatchNamespaceGrants in namespace %s: %w", ns, err)
		}
		if !hasGrantForOnPremAIGateway(grants.Items, onprem.Namespace) {
			return fmt.Errorf(
				"WatchNamespaceGrant in namespace %s for OnPremAIGateway in namespace %s not found",
				ns, onprem.Namespace,
			)
		}
	}
	return nil
}

// hasGrantForOnPremAIGateway reports whether any of the grants lets an
// OnPremAIGateway from the given namespace watch the grants' namespace.
func hasGrantForOnPremAIGateway(grants []operatorv1alpha1.WatchNamespaceGrant, gatewayNamespace string) bool {
	for _, grant := range grants {
		for _, from := range grant.Spec.From {
			if from.Group != aigatewayv1alpha1.GroupName ||
				from.Kind != onPremAIGatewayKind {
				continue
			}
			if from.Namespace == gatewayNamespace {
				return true
			}
		}
	}
	return false
}

// mapWatchNamespaceGrantToOnPremAIGateways enqueues the OnPremAIGateways a
// WatchNamespaceGrant refers to, so that a grant created after the gateway
// reconciles it without waiting for an unrelated event.
func mapWatchNamespaceGrantToOnPremAIGateways(cl client.Client, logger logr.Logger) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		grant, ok := obj.(*operatorv1alpha1.WatchNamespaceGrant)
		if !ok {
			logger.Error(nil, "failed to map WatchNamespaceGrant to OnPremAIGateways", "object", obj)
			return nil
		}

		var requests []reconcile.Request
		for _, from := range grant.Spec.From {
			if from.Group != aigatewayv1alpha1.GroupName ||
				from.Kind != onPremAIGatewayKind {
				continue
			}
			var gateways aigatewayv1alpha1.OnPremAIGatewayList
			if err := cl.List(ctx, &gateways, client.InNamespace(from.Namespace)); err != nil {
				logger.Error(err, "failed to list OnPremAIGateways for WatchNamespaceGrant",
					"namespace", from.Namespace,
				)
				continue
			}
			for i := range gateways.Items {
				requests = append(requests, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(&gateways.Items[i]),
				})
			}
		}
		return requests
	}
}

// watchNamespaceGrantHandler enqueues the OnPremAIGateways a WatchNamespaceGrant
// affects. An update enqueues the referents of both the old and the new grant:
// dropping the "from" entry that matched a gateway must re-reconcile it, and the
// new object no longer names it.
func watchNamespaceGrantHandler(cl client.Client, logger logr.Logger) handler.EventHandler {
	mapFn := mapWatchNamespaceGrantToOnPremAIGateways(cl, logger)
	enqueue := func(
		ctx context.Context,
		q workqueue.TypedRateLimitingInterface[reconcile.Request],
		obj client.Object,
	) {
		for _, req := range mapFn(ctx, obj) {
			q.Add(req)
		}
	}

	return handler.Funcs{
		CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(ctx, q, e.Object)
		},
		UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(ctx, q, e.ObjectOld)
			enqueue(ctx, q, e.ObjectNew)
		},
		DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(ctx, q, e.Object)
		},
	}
}
