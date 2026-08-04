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

	mcpv1alpha1 "github.com/kong/kong-operator/v2/api/mcp/v1alpha1"
)

const (
	// IndexFieldMCPServerOnMCPServerDataPlane is the index field for
	// MCPServerDataPlane -> MCPServer (via spec.controlPlaneRef.konnectNamespacedRef.name).
	IndexFieldMCPServerOnMCPServerDataPlane = "mcpServerDataPlaneMCPServerRef"
)

// OptionsForMCPServerDataPlane returns required Index options for the MCPServerDataPlane controller.
func OptionsForMCPServerDataPlane() []Option {
	return []Option{
		{
			Object:         &mcpv1alpha1.MCPServerDataPlane{},
			Field:          IndexFieldMCPServerOnMCPServerDataPlane,
			ExtractValueFn: mcpServerDataPlaneMCPServerRef,
		},
	}
}

func mcpServerDataPlaneMCPServerRef(object client.Object) []string {
	mcpdp, ok := object.(*mcpv1alpha1.MCPServerDataPlane)
	if !ok {
		return nil
	}
	if mcpdp.Spec.MCPServerRef.KonnectNamespacedRef == nil {
		return nil
	}
	return []string{mcpdp.Namespace + "/" + mcpdp.Spec.MCPServerRef.KonnectNamespacedRef.Name}
}
