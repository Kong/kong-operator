/*
Copyright 2025 Kong, Inc.

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

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// enqueueForKonnectEventGatewayRef returns a MapFunc that enqueues reconcile requests
// for all DataPlanes in the same namespace whose
// spec.konnectEventGatewayRef.name matches the changed KonnectEventGateway.
func enqueueForKonnectEventGatewayRef(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		keg, ok := obj.(*konnectv1alpha1.KonnectEventGateway)
		if !ok {
			return nil
		}

		egdpList := &eventgatewayv1alpha1.KegDataPlaneList{}
		if err := cl.List(ctx, egdpList,
			client.MatchingFields{index.IndexFieldKegDataPlaneOnKonnectEventGateway: keg.Namespace + "/" + keg.Name},
		); err != nil {
			return nil
		}

		requests := make([]reconcile.Request, 0, len(egdpList.Items))
		for _, egdp := range egdpList.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&egdp),
			})
		}
		return requests
	}
}
