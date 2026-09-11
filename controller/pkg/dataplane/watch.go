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

	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// EnqueueDataPlanesForControlPlane returns a MapFunc that enqueues reconcile
// requests for all DataPlanes in the same namespace whose control plane
// reference matches the changed control plane object.
func EnqueueDataPlanesForControlPlane(
	cl client.Client,
	newObjectList func() client.ObjectList,
	controlPlaneRefIndexField string,
	kind string,
	controlPlaneKind string,
) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cp := obj

		dpList := newObjectList()
		if err := cl.List(ctx, dpList,
			client.MatchingFields{controlPlaneRefIndexField: cp.GetNamespace() + "/" + cp.GetName()},
		); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "failed to list "+kind+"s for "+controlPlaneKind,
				controlPlaneKind, cp.GetName())
			return nil
		}

		items, err := meta.ExtractList(dpList)
		if err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "failed to extract "+kind+" list items for "+controlPlaneKind,
				controlPlaneKind, cp.GetName())
			return nil
		}

		requests := make([]reconcile.Request, 0, len(items))
		for _, item := range items {
			dp, ok := item.(client.Object)
			if !ok {
				continue
			}
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(dp),
			})
		}
		return requests
	}
}
