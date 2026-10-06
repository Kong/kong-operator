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
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// extraWatches registers the KegDataPlane-specific watches that the shared
// reconciler does not provide: user-referenced certificate Secrets (Manual
// provisioning) must trigger reconciliation of every KegDataPlane
// referencing them.
func extraWatches(blder *builder.Builder, mgr ctrl.Manager) *builder.Builder {
	return blder.Watches(
		&corev1.Secret{},
		handler.EnqueueRequestsFromMapFunc(shareddataplane.EnqueueDataPlanesForCertificateSecret(
			mgr.GetClient(),
			func() client.ObjectList { return &eventgatewayv1alpha1.KegDataPlaneList{} },
			index.IndexFieldKegDataPlaneOnCertificateSecret,
			"KegDataPlane",
		)),
	)
}
