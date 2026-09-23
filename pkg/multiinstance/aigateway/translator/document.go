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

package translator

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/Kong/ai-deck-converter/aigw"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// BuildDocument assembles the aigw.Document for the given OnPremAIGateway: every AIGatewayModel
// pointing at it, translated via AIGatewayModel.ToAIGWModel.
//
// This lists only AIGatewayModel today. The other aiconfiguration entity kinds join here as
// they gain their own ToAIGW* conversion; until then the rendered Document (and the dbless
// payload built from it) has dangling model_providers/policies/auth_strategies references and
// ConvertDocumentToDBLessYAML reports them as warnings, not errors.
//
// The OnOnPremAIGatewayRef index extractor only matches entities whose aiGatewayRef resolves
// to an OnPremAIGateway, so a KonnectAIGateway sharing namespace/name with this
// OnPremAIGateway never collides.
//
// NOTE: This will either stay here or be moved to a separate package where translation
// (building the document) will happen asynchronously as it's done for ingress-controller.
func BuildDocument(ctx context.Context, cl client.Client, gw types.NamespacedName) (*aigw.Document, error) {
	var list aiconfigurationv1alpha1.AIGatewayModelList
	if err := cl.List(ctx, &list, client.MatchingFields{
		index.IndexFieldAIGatewayModelOnOnPremAIGatewayRef: gw.String(),
	}); err != nil {
		return nil, fmt.Errorf("listing AIGatewayModels for %s: %w", gw, err)
	}

	items := list.Items
	slices.SortFunc(items, func(a, b aiconfigurationv1alpha1.AIGatewayModel) int {
		return cmp.Or(
			cmp.Compare(a.Namespace, b.Namespace),
			cmp.Compare(a.Name, b.Name),
		)
	})

	doc := &aigw.Document{}
	for i := range items {
		model, err := items[i].ToAIGWModel(ctx, cl)
		if err != nil {
			return nil, fmt.Errorf("converting AIGatewayModel %s: %w", client.ObjectKeyFromObject(&items[i]), err)
		}
		doc.Models = append(doc.Models, *model)
	}
	return doc, nil
}
