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

// translateKind returns a func that lists every entity of one kind pointing at the
// given OnPremAIGateway (via the kind's generated OnOnPremAIGatewayRef index), sorts
// the items by namespace/name so the rendered document (and the payload hash derived
// from it) doesn't flap across List calls that return in a different order, converts
// each item with convert and appends it to dest.
//
// Only TList is named at the call site: T is inferred from the list's GetItems and
// TListPtr from the *TList core type (same pattern as
// enqueueObjectForKonnectGatewayControlPlane in controller/konnect/watch.go).
//
// A per-entity conversion failure does not abort the whole translation: the failing entity is
// excluded from the document and reported in the returned statuses, so that the remaining
// entities still render and get pushed (mirroring KIC's continue-on-error translation).
// A failure to list the entities of a kind is a different beast: the whole kind's contribution
// is missing from the document, so it aborts the translation (and no status is reported).
//
// T's method set lives on the pointer, hence the metav1.Object assertions below — infallible
// for every aiconfiguration list item.
func translateKind[
	TList interface {
		GetItems() []T
	},
	TListPtr interface {
		*TList
		client.ObjectList
		GetItems() []T
	},
	T any,
	AIGWEntity any,
](
	cl client.Client,
	gw types.NamespacedName,
	indexField string,
	convert func(*T, context.Context, client.Client) (*AIGWEntity, error),
	dest *[]AIGWEntity,
) func(context.Context) ([]EntityStatus, error) {
	return func(ctx context.Context) ([]EntityStatus, error) {
		var (
			l    TList
			lPtr TListPtr = &l
		)

		if err := cl.List(ctx, lPtr, client.MatchingFields{indexField: gw.String()}); err != nil {
			return nil, fmt.Errorf("listing %T for %s: %w", lPtr, gw, err)
		}
		items := lPtr.GetItems()
		slices.SortFunc(items, func(a, b T) int {
			aObj, bObj := any(&a).(metav1.Object), any(&b).(metav1.Object)
			return cmp.Or(
				cmp.Compare(aObj.GetNamespace(), bObj.GetNamespace()),
				cmp.Compare(aObj.GetName(), bObj.GetName()),
			)
		})
		statuses := make([]EntityStatus, 0, len(items))
		for i := range items {
			obj := any(&items[i]).(client.Object)
			// The OnOnPremAIGatewayRef index matches entities regardless of their namespace, but
			// the on-prem path is same-namespace only (consistent with resolveEntityName's
			// entity references and rejectCrossNamespaceSecretRefs): an entity in another
			// namespace is listed here only to be rejected with a clear per-entity error,
			// instead of failing later on its Secrets being invisible to the gateway-namespace
			// scoped cache.
			// TODO: support cross-namespace references, tracked in
			// https://github.com/Kong/kong-operator/issues/5957.
			if obj.GetNamespace() != gw.Namespace {
				statuses = append(statuses, EntityStatus{
					Obj: obj,
					Err: fmt.Errorf(
						"cross-namespace reference to OnPremAIGateway %s is not supported on-prem: the entity must live in the gateway's namespace %s",
						gw, gw.Namespace,
					),
				})
				continue
			}

			aigwEntity, err := convert(&items[i], ctx, cl)
			if err != nil {
				statuses = append(statuses, EntityStatus{
					Obj: obj,
					Err: fmt.Errorf("converting %T %s: %w", obj, client.ObjectKeyFromObject(obj), err),
				})
				continue
			}
			*dest = append(*dest, *aigwEntity)
			statuses = append(statuses, EntityStatus{Obj: obj})
		}
		return statuses, nil
	}
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
	// Entity names of the certificates that translated successfully, checked when translating
	// the SNIs referencing them: resolveReferencedCertificate only checks the referenced
	// certificate's existence and same-gateway membership, so without this a certificate that
	// failed its own translation (e.g. a missing or unlabeled secretRef Secret) would leave its
	// SNI reporting success while the converter drops the SNI from the pushed payload.
	translatedCertificates := make(map[string]struct{})

	for _, translate := range []func(context.Context) ([]EntityStatus, error){
		translateKind[aiconfigurationv1alpha1.AIGatewayModelList](
			cl, gw, index.IndexFieldAIGatewayModelOnOnPremAIGatewayRef,
			(*aiconfigurationv1alpha1.AIGatewayModel).ToAIGWModel, &doc.Models),
		translateKind[aiconfigurationv1alpha1.AIGatewayModelProviderList](
			cl, gw, index.IndexFieldAIGatewayModelProviderOnOnPremAIGatewayRef,
			(*aiconfigurationv1alpha1.AIGatewayModelProvider).ToAIGWProvider, &doc.ModelProviders),
		translateKind[aiconfigurationv1alpha1.AIGatewayPolicyList](
			cl, gw, index.IndexFieldAIGatewayPolicyOnOnPremAIGatewayRef,
			(*aiconfigurationv1alpha1.AIGatewayPolicy).ToAIGWPolicy, &doc.Policies),
		translateKind[aiconfigurationv1alpha1.AIGatewayConsumerGroupList](
			cl, gw, index.IndexFieldAIGatewayConsumerGroupOnOnPremAIGatewayRef,
			(*aiconfigurationv1alpha1.AIGatewayConsumerGroup).ToAIGWConsumerGroup, &doc.ConsumerGroups),
		translateKind[aiconfigurationv1alpha1.AIGatewayConsumerList](
			cl, gw, index.IndexFieldAIGatewayConsumerOnOnPremAIGatewayRef,
			// The consumer's credential lookup uses this package's index for
			// AIGatewayConsumerCredential -> AIGatewayConsumer: the field name is passed in
			// because the api package cannot import internal/utils/index without a cycle.
			func(c *aiconfigurationv1alpha1.AIGatewayConsumer, ctx context.Context, cl client.Client) (*aigw.Consumer, error) {
				return c.ToAIGWConsumer(ctx, cl, index.IndexFieldAIGatewayConsumerCredentialOnAIGatewayConsumerRef)
			},
			&doc.Consumers),
		translateKind[aiconfigurationv1alpha1.AIGatewayAuthStrategyList](
			cl, gw, index.IndexFieldAIGatewayAuthStrategyOnOnPremAIGatewayRef,
			(*aiconfigurationv1alpha1.AIGatewayAuthStrategy).ToAIGWAuthStrategy, &doc.AuthStrategies),
		translateKind[aiconfigurationv1alpha1.AIGatewayCertificateList](
			cl, gw, index.IndexFieldAIGatewayCertificateOnOnPremAIGatewayRef,
			func(c *aiconfigurationv1alpha1.AIGatewayCertificate, ctx context.Context, cl client.Client) (*aigw.Certificate, error) {
				cert, err := c.ToAIGWCertificate(ctx, cl)
				if err != nil {
					return nil, err
				}
				translatedCertificates[c.GetKonnectName()] = struct{}{}
				return cert, nil
			},
			&doc.Certificates),
		translateKind[aiconfigurationv1alpha1.AIGatewaySNIList](
			cl, gw, index.IndexFieldAIGatewaySNIOnOnPremAIGatewayRef,
			func(s *aiconfigurationv1alpha1.AIGatewaySNI, ctx context.Context, cl client.Client) (*aigw.SNI, error) {
				sni, err := s.ToAIGWSNI(ctx, cl)
				if err != nil {
					return nil, err
				}
				if _, ok := translatedCertificates[sni.Certificate]; !ok {
					return nil, fmt.Errorf(
						"referenced AIGatewayCertificate %q was excluded from the document: its own translation failed",
						sni.Certificate,
					)
				}
				return sni, nil
			},
			&doc.SNIs),
	} {
		s, err := translate(ctx)
		if err != nil {
			return nil, nil, err
		}
		statuses = append(statuses, s...)
	}

	return doc, statuses, nil
}
