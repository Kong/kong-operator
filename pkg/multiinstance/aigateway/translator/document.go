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

// EntityStatus is the per-entity outcome of the document translation: Err is
// nil for entities that were translated and included in the document, and the
// conversion error for the ones that were not (they are excluded from the
// document so that a single broken entity does not block the rest).
type EntityStatus struct {
	Obj client.Object
	Err error
}

// appendEntities lists every entity of one kind pointing at the given OnPremAIGateway (via the
// kind's generated OnOnPremAIGatewayRef index), sorts the items by namespace/name so the
// rendered document (and the payload hash derived from it) doesn't flap across List calls that
// return in a different order, converts each item and appends it to the document.
//
// A per-entity conversion failure does not abort the whole translation: the failing entity is
// excluded from the document and reported in the returned statuses, so that the remaining
// entities still render and get pushed (mirroring KIC's continue-on-error translation).
// A failure to list the entities of a kind is a different beast: the whole kind's contribution
// is missing from the document, so it aborts the translation (and no status is reported).
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
) ([]EntityStatus, error) {
	if err := cl.List(ctx, list, client.MatchingFields{indexField: gw.String()}); err != nil {
		return nil, fmt.Errorf("listing %T for %s: %w", list, gw, err)
	}

	items := list.GetItems()
	slices.SortFunc(items, func(a, b Entity) int {
		aObj, bObj := any(&a).(metav1.Object), any(&b).(metav1.Object)
		return cmp.Or(
			cmp.Compare(aObj.GetNamespace(), bObj.GetNamespace()),
			cmp.Compare(aObj.GetName(), bObj.GetName()),
		)
	})

	statuses := make([]EntityStatus, 0, len(items))
	for i := range items {
		obj := any(&items[i]).(client.Object)
		aigwEntity, err := convert(ctx, cl, &items[i])
		if err != nil {
			statuses = append(statuses, EntityStatus{
				Obj: obj,
				Err: fmt.Errorf("converting %T %s: %w", obj, client.ObjectKeyFromObject(obj), err),
			})
			continue
		}
		appendTo(doc, aigwEntity)
		statuses = append(statuses, EntityStatus{Obj: obj})
	}
	return statuses, nil
}

// BuildDocument assembles the aigw.Document for the given OnPremAIGateway, translating every
// aiconfiguration entity kind pointing at it. Alongside the document it returns the per-entity
// translation status (see EntityStatus), so that the caller can report each entity's outcome on
// the entity's status.
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
func BuildDocument(
	ctx context.Context,
	cl client.Client,
	gw types.NamespacedName,
) (*aigw.Document, []EntityStatus, error) {
	doc := &aigw.Document{}
	var statuses []EntityStatus

	// TODO: dedup the per-kind appendEntities blocks below, tracked in
	// https://github.com/Kong/kong-operator/issues/5909.

	s, err := appendEntities(ctx, cl, gw, &aiconfigurationv1alpha1.AIGatewayModelList{},
		index.IndexFieldAIGatewayModelOnOnPremAIGatewayRef, doc,
		func(ctx context.Context, cl client.Client, m *aiconfigurationv1alpha1.AIGatewayModel) (*aigw.Model, error) {
			return m.ToAIGWModel(ctx, cl)
		},
		func(d *aigw.Document, m *aigw.Model) { d.Models = append(d.Models, *m) },
	)
	if err != nil {
		return nil, nil, err
	}
	statuses = append(statuses, s...)

	s, err = appendEntities(ctx, cl, gw, &aiconfigurationv1alpha1.AIGatewayModelProviderList{},
		index.IndexFieldAIGatewayModelProviderOnOnPremAIGatewayRef, doc,
		func(ctx context.Context, cl client.Client, p *aiconfigurationv1alpha1.AIGatewayModelProvider) (*aigw.Provider, error) {
			return p.ToAIGWProvider(ctx, cl)
		},
		func(d *aigw.Document, p *aigw.Provider) { d.ModelProviders = append(d.ModelProviders, *p) },
	)
	if err != nil {
		return nil, nil, err
	}
	statuses = append(statuses, s...)

	s, err = appendEntities(ctx, cl, gw, &aiconfigurationv1alpha1.AIGatewayPolicyList{},
		index.IndexFieldAIGatewayPolicyOnOnPremAIGatewayRef, doc,
		func(ctx context.Context, cl client.Client, p *aiconfigurationv1alpha1.AIGatewayPolicy) (*aigw.Policy, error) {
			return p.ToAIGWPolicy(ctx, cl)
		},
		func(d *aigw.Document, p *aigw.Policy) { d.Policies = append(d.Policies, *p) },
	)
	if err != nil {
		return nil, nil, err
	}
	statuses = append(statuses, s...)

	s, err = appendEntities(ctx, cl, gw, &aiconfigurationv1alpha1.AIGatewayConsumerGroupList{},
		index.IndexFieldAIGatewayConsumerGroupOnOnPremAIGatewayRef, doc,
		func(ctx context.Context, cl client.Client, g *aiconfigurationv1alpha1.AIGatewayConsumerGroup) (*aigw.ConsumerGroup, error) {
			return g.ToAIGWConsumerGroup(ctx, cl)
		},
		func(d *aigw.Document, g *aigw.ConsumerGroup) { d.ConsumerGroups = append(d.ConsumerGroups, *g) },
	)
	if err != nil {
		return nil, nil, err
	}
	statuses = append(statuses, s...)

	s, err = appendEntities(ctx, cl, gw, &aiconfigurationv1alpha1.AIGatewayAuthStrategyList{},
		index.IndexFieldAIGatewayAuthStrategyOnOnPremAIGatewayRef, doc,
		func(ctx context.Context, cl client.Client, a *aiconfigurationv1alpha1.AIGatewayAuthStrategy) (*aigw.AuthStrategy, error) {
			return a.ToAIGWAuthStrategy(ctx, cl)
		},
		func(d *aigw.Document, a *aigw.AuthStrategy) { d.AuthStrategies = append(d.AuthStrategies, *a) },
	)
	if err != nil {
		return nil, nil, err
	}
	statuses = append(statuses, s...)

	return doc, statuses, nil
}
