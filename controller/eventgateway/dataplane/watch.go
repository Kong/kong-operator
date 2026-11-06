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

package dataplane

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// extraWatches registers the KegDataPlane-specific watches that the shared
// reconciler does not provide: user-referenced certificate Secrets (Manual
// provisioning) must trigger reconciliation of every KegDataPlane
// referencing them, and so must a GWAdmin, or one of its Secrets, e.g. a
// rotated certificate authority, trigger the data planes of its
// OnPremEventGateways, when the OnPremEventGateway kind is enabled (onPrem).
func extraWatches(blder *builder.Builder, mgr ctrl.Manager, onPrem bool) *builder.Builder {
	cl := mgr.GetClient()
	blder = blder.
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(shareddataplane.EnqueueDataPlanesByIndex(
				cl,
				func() client.ObjectList { return &eventgatewayv1alpha1.KegDataPlaneList{} },
				index.IndexFieldKegDataPlaneOnCertificateSecret,
				"KegDataPlane",
				"Secret",
			)),
		)
	if !onPrem {
		return blder
	}
	return blder.
		Watches(
			&operatorv1alpha1.GWAdmin{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return kegDataPlanesOfGWAdmin(ctx, cl, client.ObjectKeyFromObject(obj))
			}),
		).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return kegDataPlanesOfGWAdminSecret(ctx, cl, obj)
			}),
		).
		Watches(
			&configurationv1alpha1.KongReferenceGrant{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return kegDataPlanesOfGrant(ctx, cl, obj)
			}),
		)
}

// kegDataPlanesOfGWAdminSecret returns a request for each KegDataPlane of the
// OnPremEventGateways that reference the GWAdmin the Secret belongs to, if
// any.
func kegDataPlanesOfGWAdminSecret(ctx context.Context, cl client.Client, secret client.Object) []reconcile.Request {
	gwAdmin, ok := gwadminref.OfSecret(secret)
	if !ok {
		return nil
	}
	return kegDataPlanesOfGWAdmin(ctx, cl, gwAdmin)
}

// kegDataPlanesOfGrant returns a request for each KegDataPlane of the
// OnPremEventGateways that reference a GWAdmin of the namespace of the
// KongReferenceGrant: it decides whether the gateways of other namespaces may
// use them.
func kegDataPlanesOfGrant(ctx context.Context, cl client.Client, obj client.Object) []reconcile.Request {
	// A grant that doesn't concern GWAdmins, e.g. for the Secrets of an entity,
	// changes nothing for the data planes.
	grant, ok := obj.(*configurationv1alpha1.KongReferenceGrant)
	if !ok || len(gwadminref.NamespacesGrantedBy(grant)) == 0 {
		return nil
	}
	gwAdmins, err := gwadminref.OfGrant(ctx, cl, grant)
	if err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "failed to list the GWAdmins of a KongReferenceGrant", "grant", client.ObjectKeyFromObject(grant))
		return nil
	}
	var requests []reconcile.Request
	for _, gwAdmin := range gwAdmins {
		requests = append(requests, kegDataPlanesOfGWAdmin(ctx, cl, gwAdmin)...)
	}
	return requests
}

// kegDataPlanesOfGWAdmin returns a request for each KegDataPlane of the
// OnPremEventGateways that reference the GWAdmin, in any namespace.
func kegDataPlanesOfGWAdmin(ctx context.Context, cl client.Client, gwAdmin client.ObjectKey) []reconcile.Request {
	logger := ctrl.LoggerFrom(ctx)
	gateways, err := gwadminref.GatewaysOf(ctx, cl, gwAdmin)
	if err != nil {
		logger.Error(err, "failed to list the OnPremEventGateways of a GWAdmin", "gwAdmin", gwAdmin)
		return nil
	}
	dataPlanesOf := shareddataplane.EnqueueDataPlanesByIndex(cl,
		func() client.ObjectList { return &eventgatewayv1alpha1.KegDataPlaneList{} },
		index.IndexFieldKegDataPlaneOnOnPremEventGateway, "KegDataPlane", eventgatewayv1alpha1.OnPremEventGatewayKind)
	var requests []reconcile.Request
	for i := range gateways {
		requests = append(requests, dataPlanesOf(ctx, &gateways[i])...)
	}
	return requests
}
