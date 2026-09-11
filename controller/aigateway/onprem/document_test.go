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
	"testing"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/Kong/ai-deck-converter/convert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

func aiGatewayModelFixture(name string) *aiconfigurationv1alpha1.AIGatewayModel {
	return &aiconfigurationv1alpha1.AIGatewayModel{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayModelSpec{
			AIGatewayRef: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayModelAPISpec{
				AIGatewayModelConfig: &aiconfigurationv1alpha1.AIGatewayModelConfig{
					Type: aiconfigurationv1alpha1.AIGatewayModelConfigTypeModel,
					Model: &aiconfigurationv1alpha1.AIGatewayModelModel{
						Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
						DisplayName: name,
						Formats:     []aiconfigurationv1alpha1.AIGatewayModelFormat{{Type: "openai"}},
						Config: aiconfigurationv1alpha1.AIGatewayModelModelConfig{
							Route: aiconfigurationv1alpha1.AIGatewayModelRouteConfig{Paths: []string{"/" + name}},
						},
					},
				},
			},
		},
	}
}

// TestBuildDocument covers listing, conversion and deterministic ordering: buildDocument sorts
// by k8s object name so the rendered payload (and its hash, which drives the drift loop in
// controller.go) doesn't flap across List calls that return in a different order.
func TestBuildDocument(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// Registered out of sort order to prove buildDocument, not List, does the sorting.
	modelB := aiGatewayModelFixture("model-b")
	modelA := aiGatewayModelFixture("model-a")

	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, modelB, modelA)
	for _, opt := range index.OptionsForAIGatewayModel() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, err := buildDocument(t.Context(), cl, gw)
	require.NoError(t, err)
	require.Len(t, doc.Models, 2)
	require.Equal(t, "model-a", doc.Models[0].Name)
	require.Equal(t, "model-b", doc.Models[1].Name)

	// Non-strict rendering must not fail even once references outside the fixture (dangling
	// model_providers/policies/auth_strategies) are introduced by the next slice - pinned here
	// with a target that references a provider this test never creates.
	doc.Models[0].TargetModels = []aigw.TargetModel{{Name: "t", Provider: "does-not-exist"}}
	payload, warnings, err := convert.ConvertDocumentToDBLessYAML(doc, convert.Options{Strict: false})
	require.NoError(t, err)
	require.NotEmpty(t, payload)
	require.NotEmpty(t, warnings)
}

// TestBuildDocument_NoModels covers the empty case: an OnPremAIGateway with no AIGatewayModels
// yet must not error.
func TestBuildDocument_NoModels(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}

	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw)
	for _, opt := range index.OptionsForAIGatewayModel() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, err := buildDocument(t.Context(), cl, gw)
	require.NoError(t, err)
	require.Empty(t, doc.Models)
}
