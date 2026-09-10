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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// enqueueForKonnectAIGatewayRef returns a MapFunc that enqueues reconcile requests
// for all AIGatewayDataPlanes in the same namespace whose
// spec.controlPlaneRef.konnectNamespacedRef.name matches the changed KonnectAIGateway.
func enqueueForKonnectAIGatewayRef(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		aigwcp, ok := obj.(*konnectv1alpha1.KonnectAIGateway)
		if !ok {
			return nil
		}

		aigwdpList := &aigatewayv1alpha1.AIGatewayDataPlaneList{}
		if err := cl.List(ctx, aigwdpList,
			client.MatchingFields{index.IndexFieldAIGatewayDataPlaneOnKonnectAIGateway: aigwcp.Namespace + "/" + aigwcp.Name},
		); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "failed to list AIGatewayDataPlanes for KonnectAIGateway",
				"KonnectAIGateway", aigwcp.Name)
			return nil
		}

		requests := make([]reconcile.Request, 0, len(aigwdpList.Items))
		for _, aigwdp := range aigwdpList.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&aigwdp),
			})
		}
		return requests
	}
}

// enqueueForAIGatewayDataPlaneCertificateSecretRef returns a MapFunc that
// enqueues reconcile requests for all AIGatewayDataPlanes in the same
// namespace as the changed Secret whose spec.certificateSecret.secretRef
// resolves to it. Operator-owned (automatically-provisioned) certificate
// Secrets are already covered by the controller's Owns(&corev1.Secret{});
// this watch exists solely so edits to a manually-referenced, user-owned
// Secret also trigger a reconcile.
func enqueueForAIGatewayDataPlaneCertificateSecretRef(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}

		aigwdpList := &aigatewayv1alpha1.AIGatewayDataPlaneList{}
		if err := cl.List(ctx, aigwdpList,
			client.MatchingFields{index.IndexFieldAIGatewayDataPlaneOnCertificateSecret: secret.Namespace + "/" + secret.Name},
		); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "failed to list AIGatewayDataPlanes for certificate Secret",
				"Secret", secret.Name)
			return nil
		}

		requests := make([]reconcile.Request, 0, len(aigwdpList.Items))
		for _, aigwdp := range aigwdpList.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&aigwdp),
			})
		}
		return requests
	}
}
