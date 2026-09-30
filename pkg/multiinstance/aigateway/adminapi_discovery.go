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

package aigateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	dataplaneconsts "github.com/kong/kong-operator/v2/controller/aigateway/dataplane"
	adminapidiscovery "github.com/kong/kong-operator/v2/internal/adminapi"
)

// ControllerNameAdminAPIEndpoints is the name used for logging and event recording
// by the Admin API endpoints discovery controller.
const ControllerNameAdminAPIEndpoints = "onprem-aigateway-adminapi-endpoints"

// AdminAPIEndpointsReconciler discovers the Admin API endpoints of all the
// AIGatewayDataPlanes referencing the Reconciler's OnPremAIGateway and notifies
// about the discovered set through OnDiscovery.
//
// It reconciles whole data planes rather than individual EndpointSlices: on
// every relevant event it re-discovers the endpoints of all the referencing
// data planes' Admin API Services, so the notified set always reflects the
// complete state.
type AdminAPIEndpointsReconciler struct {
	client.Client

	// GatewayNN is the OnPremAIGateway whose referencing AIGatewayDataPlanes'
	// Admin API endpoints are discovered.
	GatewayNN types.NamespacedName

	// Discoverer performs the EndpointSlice -> Admin API endpoint discovery.
	Discoverer *adminapidiscovery.Discoverer

	// Log is the logger used by the reconciler.
	Log logr.Logger

	// OnDiscovery is called with the Admin APIs discovered for all the
	// AIGatewayDataPlanes referencing GatewayNN. It is called on every
	// successful reconciliation, including when the set is empty.
	OnDiscovery func(ctx context.Context, adminAPIs sets.Set[adminapidiscovery.DiscoveredAdminAPI])
}

// SetupWithManager sets up the controller with the Manager.
func (r *AdminAPIEndpointsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(ControllerNameAdminAPIEndpoints).
		For(
			&aigatewayv1alpha1.AIGatewayDataPlane{},
			builder.WithPredicates(r.gatewayDataPlanePredicate()),
		).
		Watches(
			&discoveryv1.EndpointSlice{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(r.adminAPIEndpointSlicePredicate()),
		).
		Complete(r)
}

// gatewayDataPlanePredicate filters AIGatewayDataPlane events to only those
// referencing the Reconciler's OnPremAIGateway.
func (r *AdminAPIEndpointsReconciler) gatewayDataPlanePredicate() predicate.Predicate {
	preds := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		dp, ok := obj.(*aigatewayv1alpha1.AIGatewayDataPlane)
		if !ok {
			return false
		}
		return referencesOnPremAIGateway(dp, r.GatewayNN)
	})
	// Accept updates where the old object referenced the gateway too: a data
	// plane that stops referencing it must trigger rediscovery, or the
	// discovered set keeps its stale endpoints.
	preds.UpdateFunc = func(e event.UpdateEvent) bool {
		oldDP, okOld := e.ObjectOld.(*aigatewayv1alpha1.AIGatewayDataPlane)
		newDP, okNew := e.ObjectNew.(*aigatewayv1alpha1.AIGatewayDataPlane)
		return (okOld && referencesOnPremAIGateway(oldDP, r.GatewayNN)) ||
			(okNew && referencesOnPremAIGateway(newDP, r.GatewayNN))
	}
	return preds
}

// adminAPIEndpointSlicePredicate filters EndpointSlice events to only those
// that may belong to an Admin API Service of an AIGatewayDataPlane: the ones
// in the gateway's namespace whose service name uses the Admin API Service
// name suffix. Whether the underlying Service actually belongs to a data
// plane referencing the gateway is determined during reconciliation.
func (r *AdminAPIEndpointsReconciler) adminAPIEndpointSlicePredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		endpoints, ok := obj.(*discoveryv1.EndpointSlice)
		if !ok {
			return false
		}
		if endpoints.Namespace != r.GatewayNN.Namespace {
			return false
		}
		serviceName := endpoints.Labels[discoveryv1.LabelServiceName]
		return strings.HasSuffix(serviceName, dataplaneconsts.AdminServiceNameSuffix)
	})
}

// Reconcile discovers the Admin API endpoints of all the AIGatewayDataPlanes
// referencing the Reconciler's OnPremAIGateway and notifies about the result.
// The reconcile.Request is ignored because we reconcile whole data planes
// instead of individual EndpointSlices: any relevant event (a data plane
// added or removed, an EndpointSlice changed) invalidates the complete
// discovered set, so re-discovering all of it is the only correct response.
// This is cheap - just a couple of cached list calls - and guarantees the
// notified set is always complete, with no state to keep in sync.
func (r *AdminAPIEndpointsReconciler) Reconcile(ctx context.Context, _ reconcile.Request) (ctrl.Result, error) {
	var dpList aigatewayv1alpha1.AIGatewayDataPlaneList
	if err := r.List(ctx, &dpList, client.InNamespace(r.GatewayNN.Namespace)); err != nil {
		return ctrl.Result{}, err
	}

	adminAPIs := sets.New[adminapidiscovery.DiscoveredAdminAPI]()
	for i := range dpList.Items {
		dp := &dpList.Items[i]
		if !referencesOnPremAIGateway(dp, r.GatewayNN) {
			continue
		}
		serviceNN := types.NamespacedName{
			Namespace: dp.Namespace,
			Name:      dp.Name + dataplaneconsts.AdminServiceNameSuffix,
		}
		discovered, err := r.Discoverer.GetAdminAPIsForService(ctx, r.Client, serviceNN)
		if err != nil {
			// A partial set must never replace the last known complete
			// one: skip the notification and requeue, so the next
			// reconciliation rediscovers everything.
			r.Log.Error(err, "failed to discover Admin API endpoints",
				"dataplane", client.ObjectKeyFromObject(dp))
			return ctrl.Result{}, err
		}
		adminAPIs = adminAPIs.Union(withTLSServerName(discovered, serviceNN))
	}

	r.Log.V(1).Info(
		"Discovered Admin API endpoints for the OnPremAIGateway's data planes",
		"namespace", r.GatewayNN.Namespace,
		"name", r.GatewayNN.Name,
		"count", adminAPIs.Len(),
	)
	r.OnDiscovery(ctx, adminAPIs)

	return ctrl.Result{}, nil
}

// referencesOnPremAIGateway returns true if the AIGatewayDataPlane references
// the provided OnPremAIGateway via spec.controlPlaneRef.
func referencesOnPremAIGateway(
	dp *aigatewayv1alpha1.AIGatewayDataPlane,
	gatewayNN types.NamespacedName,
) bool {
	ref := dp.Spec.ControlPlaneRef
	if ref == nil ||
		ref.Type != aigatewayv1alpha1.ControlPlaneRefTypeOnPremNamespacedRef ||
		ref.OnPremNamespacedRef == nil {
		return false
	}
	// The onpremNamespacedRef references an OnPremAIGateway in the same
	// namespace as the AIGatewayDataPlane.
	return dp.Namespace == gatewayNN.Namespace && ref.OnPremNamespacedRef.Name == gatewayNN.Name
}

// adminAPIServiceTLSServerName returns the TLS ServerName used to verify the
// Admin API server certificate of the given Admin API Service. It is the
// Service's plain in-cluster DNS name, which must equal the AIGatewayDataPlane
// Admin API certificate subject (AdminAPIConfig.certificateSubject,
// <name>-admin.<namespace>.svc).
//
// The shared discovery (internal/adminapi) emits a pod.<service>.<namespace>.svc
// server name instead, which is covered only by the wildcard certificate of the
// regular DataPlane. The AIGatewayDataPlane certificate is issued for the plain
// Service DNS name, so the on-prem push path overrides it.
func adminAPIServiceTLSServerName(serviceNN types.NamespacedName) string {
	return fmt.Sprintf("%s.%s.svc", serviceNN.Name, serviceNN.Namespace)
}

// withTLSServerName returns a copy of the set with every endpoint's
// TLSServerName replaced by the plain in-cluster DNS name of the Admin API
// Service the endpoints belong to (see adminAPIServiceTLSServerName).
func withTLSServerName(
	apis sets.Set[adminapidiscovery.DiscoveredAdminAPI],
	serviceNN types.NamespacedName,
) sets.Set[adminapidiscovery.DiscoveredAdminAPI] {
	overridden := make(sets.Set[adminapidiscovery.DiscoveredAdminAPI], apis.Len())
	for api := range apis {
		api.TLSServerName = adminAPIServiceTLSServerName(serviceNN)
		overridden.Insert(api)
	}
	return overridden
}
