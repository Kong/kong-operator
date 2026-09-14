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

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
)

// mapAIGatewayModelToOnPremAIGateway requeues the OnPremAIGateway an AIGatewayModel's
// aiGatewayRef names, so config changes on the model are picked up without waiting for the
// OnPremAIGateway's own resync.
func mapAIGatewayModelToOnPremAIGateway(_ context.Context, obj client.Object) []reconcile.Request {
	model, ok := obj.(*aiconfigurationv1alpha1.AIGatewayModel)
	if !ok || model.Spec.AIGatewayRef.NamespacedRef == nil {
		return nil
	}
	ns := model.Namespace
	ref := model.Spec.AIGatewayRef.NamespacedRef
	if ref.Namespace != nil && *ref.Namespace != "" {
		ns = *ref.Namespace
	}
	return []reconcile.Request{
		{
			Namespace: ns,
			Name:      ref.Name,
		},
	}
}
