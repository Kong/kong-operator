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

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
)

// mapAIGatewayDataPlaneToOnPremAIGateway requeues the OnPremAIGateway for
// changes to the AIGatewayDataPlane resource.
func mapAIGatewayDataPlaneToOnPremAIGateway(_ context.Context, obj client.Object) []reconcile.Request {
	dp, ok := obj.(*aigatewayv1alpha1.AIGatewayDataPlane)
	if !ok || dp.Spec.ControlPlaneRef == nil ||
		dp.Spec.ControlPlaneRef.Type != aigatewayv1alpha1.ControlPlaneRefTypeOnPremNamespacedRef {
		return nil
	}
	ref := dp.Spec.ControlPlaneRef.OnPremNamespacedRef
	if ref == nil {
		return nil
	}
	return []reconcile.Request{
		{
			Namespace: dp.Namespace,
			Name:      ref.Name,
		},
	}
}
