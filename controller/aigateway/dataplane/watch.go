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

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// enqueueForAIGatewayControlPlaneRef returns a MapFunc that enqueues reconcile requests
// for all AIGatewayDataPlanes in the same namespace whose
// spec.controlPlaneRef.konnectNamespacedRef.name matches the changed AIGatewayControlPlane.
func enqueueForAIGatewayControlPlaneRef(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		aigwcp, ok := obj.(*konnectv1alpha1.AIGatewayControlPlane)
		if !ok {
			return nil
		}

		aigwdpList := &aigatewayv1alpha1.AIGatewayDataPlaneList{}
		if err := cl.List(ctx, aigwdpList,
			client.MatchingFields{index.IndexFieldAIGatewayDataPlaneOnAIGatewayControlPlane: aigwcp.Namespace + "/" + aigwcp.Name},
		); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "failed to list AIGatewayDataPlanes for AIGatewayControlPlane",
				"AIGatewayControlPlane", aigwcp.Name)
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
