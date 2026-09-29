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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// appendEntities lists every entity of one kind pointing at the given OnPremAIGateway (via the
// kind's generated OnOnPremAIGatewayRef index), sorts the items by namespace/name so the
// rendered document (and the payload hash derived from it) doesn't flap across List calls that
// return in a different order, converts each item and appends it to the document.
//
// convert and appendTo are the only kind-specific parts: the ToAIGW* method name and the
// aigw.Document field the result lands in.
//
// Entity is the CRD entity's value type (as returned by the generated GetItems); its method
// set lives on the pointer, hence the metav1.Object assertions below — infallible for every
// aiconfiguration list item.
func appendEntities[Entity any, AIGWEntity any, List interface {
	client.ObjectList
	GetItems() []Entity
}](
	ctx context.Context,
	cl client.Client,
	gw types.NamespacedName,
	list List,
	indexField string,
	doc *aigw.Document,
	convert func(context.Context, client.Client, *Entity) (AIGWEntity, error),
	appendTo func(*aigw.Document, AIGWEntity),
) error {
	if err := cl.List(ctx, list, client.MatchingFields{indexField: gw.String()}); err != nil {
		return fmt.Errorf("listing %T for %s: %w", list, gw, err)
	}

	items := list.GetItems()
	slices.SortFunc(items, func(a, b Entity) int {
		aObj, bObj := any(&a).(metav1.Object), any(&b).(metav1.Object)
		return cmp.Or(
			cmp.Compare(aObj.GetNamespace(), bObj.GetNamespace()),
			cmp.Compare(aObj.GetName(), bObj.GetName()),
		)
	})

	for i := range items {
		aigwEntity, err := convert(ctx, cl, &items[i])
		if err != nil {
			obj := any(&items[i]).(client.Object)
			return fmt.Errorf("converting %T %s: %w", obj, client.ObjectKeyFromObject(obj), err)
		}
		appendTo(doc, aigwEntity)
	}
	return nil
}

// BuildDocument assembles the aigw.Document for the given OnPremAIGateway, translating every
// aiconfiguration entity kind pointing at it.
//
// Entity kinds join here as they gain their own ToAIGW* conversion; until then the rendered
// Document (and the dbless payload built from it) has dangling references and
// ConvertDocumentToDBLessYAML reports them as warnings, not errors.
//
// The OnOnPremAIGatewayRef index extractor only matches entities whose aiGatewayRef resolves
// to an OnPremAIGateway, so a KonnectAIGateway sharing namespace/name with this
// OnPremAIGateway never collides.
//
// NOTE: This will either stay here or be moved to a separate package where translation
// (building the document) will happen asynchronously as it's done for ingress-controller.
func BuildDocument(ctx context.Context, cl client.Client, gw types.NamespacedName) (*aigw.Document, error) {
	doc := &aigw.Document{}

	if err := appendEntities(ctx, cl, gw, &aiconfigurationv1alpha1.AIGatewayModelList{},
		index.IndexFieldAIGatewayModelOnOnPremAIGatewayRef, doc,
		func(ctx context.Context, cl client.Client, m *aiconfigurationv1alpha1.AIGatewayModel) (*aigw.Model, error) {
			return m.ToAIGWModel(ctx, cl)
		},
		func(d *aigw.Document, m *aigw.Model) { d.Models = append(d.Models, *m) },
	); err != nil {
		return nil, err
	}

	if err := appendEntities(ctx, cl, gw, &aiconfigurationv1alpha1.AIGatewayModelProviderList{},
		index.IndexFieldAIGatewayModelProviderOnOnPremAIGatewayRef, doc,
		func(ctx context.Context, cl client.Client, p *aiconfigurationv1alpha1.AIGatewayModelProvider) (*aigw.Provider, error) {
			return p.ToAIGWProvider(ctx, cl)
		},
		func(d *aigw.Document, p *aigw.Provider) { d.ModelProviders = append(d.ModelProviders, *p) },
	); err != nil {
		return nil, err
	}

	if err := appendEntities(ctx, cl, gw, &aiconfigurationv1alpha1.AIGatewayPolicyList{},
		index.IndexFieldAIGatewayPolicyOnOnPremAIGatewayRef, doc,
		func(ctx context.Context, cl client.Client, p *aiconfigurationv1alpha1.AIGatewayPolicy) (*aigw.Policy, error) {
			return p.ToAIGWPolicy(ctx, cl)
		},
		func(d *aigw.Document, p *aigw.Policy) { d.Policies = append(d.Policies, *p) },
	); err != nil {
		return nil, err
	}

	return doc, nil
}
