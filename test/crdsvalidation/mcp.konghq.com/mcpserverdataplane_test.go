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

package crdsvalidation_test

import (
	"fmt"
	"testing"

	mcpv1alpha1 "github.com/kong/kong-operator/v2/api/mcp/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validMCPServerDataPlane(ns string) *mcpv1alpha1.MCPServerDataPlane {
	return &mcpv1alpha1.MCPServerDataPlane{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: mcpv1alpha1.MCPServerDataPlaneSpec{
			MCPServerRef: mcpv1alpha1.MCPServerRef{
				Type: mcpv1alpha1.MCPServerRefTypeKonnectNamespacedRef,
				KonnectNamespacedRef: &mcpv1alpha1.KonnectNamespacedRef{
					Name: "my-mcp-server",
				},
			},
		},
	}
}

// stringMapOfSize returns a map[string]string with n distinct keys, useful for
// exercising +kubebuilder:validation:MaxProperties boundaries.
func stringMapOfSize(n int) map[string]string {
	m := make(map[string]string, n)
	for i := range n {
		m[fmt.Sprintf("key-%d", i)] = "value"
	}
	return m
}

func TestMCPServerDataPlane(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("deployment labels and annotations", func(t *testing.T) {
		common.TestCasesGroup[*mcpv1alpha1.MCPServerDataPlane]{
			{
				Name: "labels and annotations are accepted",
				TestObject: func() *mcpv1alpha1.MCPServerDataPlane {
					obj := validMCPServerDataPlane(ns.Name)
					obj.Spec.Deployment = &mcpv1alpha1.DeploymentOptions{
						Labels:      map[string]string{"team": "platform"},
						Annotations: map[string]string{"team-contact": "platform@konghq.com"},
					}
					return obj
				}(),
			},
			{
				Name: "exactly 64 labels is accepted",
				TestObject: func() *mcpv1alpha1.MCPServerDataPlane {
					obj := validMCPServerDataPlane(ns.Name)
					obj.Spec.Deployment = &mcpv1alpha1.DeploymentOptions{
						Labels: stringMapOfSize(64),
					}
					return obj
				}(),
			},
			{
				Name: "65 labels is rejected",
				TestObject: func() *mcpv1alpha1.MCPServerDataPlane {
					obj := validMCPServerDataPlane(ns.Name)
					obj.Spec.Deployment = &mcpv1alpha1.DeploymentOptions{
						Labels: stringMapOfSize(65),
					}
					return obj
				}(),
				ExpectedErrorMessage: new("spec.deployment.labels: Too many: 65: must have at most 64 items"),
			},
			{
				Name: "65 annotations is rejected",
				TestObject: func() *mcpv1alpha1.MCPServerDataPlane {
					obj := validMCPServerDataPlane(ns.Name)
					obj.Spec.Deployment = &mcpv1alpha1.DeploymentOptions{
						Annotations: stringMapOfSize(65),
					}
					return obj
				}(),
				ExpectedErrorMessage: new("spec.deployment.annotations: Too many: 65: must have at most 64 items"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("deployment replicas", func(t *testing.T) {
		common.TestCasesGroup[*mcpv1alpha1.MCPServerDataPlane]{
			{
				Name: "replicas can be changed from default 1 to 3",
				TestObject: func() *mcpv1alpha1.MCPServerDataPlane {
					obj := validMCPServerDataPlane(ns.Name)
					obj.Spec.Deployment = &mcpv1alpha1.DeploymentOptions{
						Replicas: new(int32(3)),
					}
					return obj
				}(),
			},
			{
				Name: "replicas cannot be set to negative value",
				TestObject: func() *mcpv1alpha1.MCPServerDataPlane {
					obj := validMCPServerDataPlane(ns.Name)
					obj.Spec.Deployment = &mcpv1alpha1.DeploymentOptions{
						Replicas: new(int32(-1)),
					}
					return obj
				}(),
				ExpectedErrorMessage: new("Invalid value: -1: should be a non-negative integer"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
