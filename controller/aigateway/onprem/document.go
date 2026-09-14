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
	"fmt"
	"sort"

	"github.com/Kong/ai-deck-converter/aigw"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// buildDocument assembles the aigw.Document for the given OnPremAIGateway: every AIGatewayModel
// pointing at it, translated via AIGatewayModel.ToAIGWModel.
//
// This lists only AIGatewayModel today. The other nine aiconfiguration entity kinds join here as
// they gain their own ToAIGW* conversion; until then the rendered Document (and the dbless
// payload built from it) has dangling model_providers/policies/auth_strategies references and
// ConvertDocumentToDBLessYAML reports them as warnings, not errors.
//
// Reusing IndexFieldAIGatewayModelOnKonnectAIGatewayRef - its extractor
// (internal/utils/index/zz_generated_aigatewaymodel.go) keys purely on namespace/name; it
// doesn't check group or kind. A KonnectAIGateway with the same namespace/name as this
// OnPremAIGateway would collide. Resolves once aiGatewayRef gets group/kind,
// until then the index name is a misnomer for this caller.
//
// NOTE: This will either stay here or be moved to a separate package where translation
// (building the document) will happen asynchronously as it's done for ingress-controller.
func buildDocument(ctx context.Context, cl client.Client, gw *aigatewayv1alpha1.OnPremAIGateway) (*aigw.Document, error) {
	var list aiconfigurationv1alpha1.AIGatewayModelList
	if err := cl.List(ctx, &list, client.MatchingFields{
		index.IndexFieldAIGatewayModelOnKonnectAIGatewayRef: client.ObjectKeyFromObject(gw).String(),
	}); err != nil {
		return nil, fmt.Errorf("listing AIGatewayModels for %s: %w", client.ObjectKeyFromObject(gw), err)
	}

	items := list.Items
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })

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
