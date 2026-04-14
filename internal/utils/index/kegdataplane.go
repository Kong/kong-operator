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

package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
)

const (
	// IndexFieldKegDataPlaneOnKonnectEventGateway is the index field for
	// KegDataPlane -> KonnectEventGateway (via spec.controlPlaneRef.konnectNamespacedRef.name).
	IndexFieldKegDataPlaneOnKonnectEventGateway = "kegDataPlaneKonnectEventGatewayRef"
)

// OptionsForKegDataPlane returns required Index options for the KegDataPlane controller.
func OptionsForKegDataPlane() []Option {
	return []Option{
		{
			Object:         &eventgatewayv1alpha1.KegDataPlane{},
			Field:          IndexFieldKegDataPlaneOnKonnectEventGateway,
			ExtractValueFn: kegDataPlaneKonnectNamespacedRef,
		},
	}
}

func kegDataPlaneKonnectNamespacedRef(object client.Object) []string {
	egdp, ok := object.(*eventgatewayv1alpha1.KegDataPlane)
	if !ok {
		return nil
	}
	if egdp.Spec.ControlPlaneRef.KonnectNamespacedRef == nil {
		return nil
	}
	return []string{egdp.Namespace + "/" + egdp.Spec.ControlPlaneRef.KonnectNamespacedRef.Name}
}
