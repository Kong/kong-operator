package intermediate

import (
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/internal/types"
	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
)

type Rule struct {
	Name
	Matches     map[string]Match
	BackendRefs map[string]BackendRef
}

type Match struct {
	Name
	Match gwtypes.HTTPRouteMatch
}

type BackendRef struct {
	Name
	BackendRef gwtypes.HTTPBackendRef
}

type ControlPlaneRef struct {
	Name
	ControlPlaneRef *commonv1alpha1.ControlPlaneRef
}

type ParentReference struct {
	Name
	ParentReference gwtypes.ParentReference
}

type HTTPRouteRepresentation struct {
	Rules            map[string]Rule
	Hostnames        map[string][]string
	ControlPlaneRefs map[string]ControlPlaneRef
	ParentRefs       map[string]ParentReference
	StripPath        bool
}

func NewHTTPRouteRepresentation(route *gwtypes.HTTPRoute) *HTTPRouteRepresentation {
	repr := HTTPRouteRepresentation{
		Rules:            map[string]Rule{},
		Hostnames:        map[string][]string{},
		ControlPlaneRefs: map[string]ControlPlaneRef{},
		ParentRefs:       map[string]ParentReference{},
		StripPath:        metadata.ExtractStripPath(route.Annotations),
	}

	for i := range route.Spec.ParentRefs {
		repr.AddParentRef(ParentReference{
			Name:            NameFromHTTPRoute(route, "", i),
			ParentReference: route.Spec.ParentRefs[i],
		})
		for j, rule := range route.Spec.Rules {
			ruleName := NameFromHTTPRoute(route, "", i, j)
			for k, match := range rule.Matches {
				matchName := NameFromHTTPRoute(route, "", i, j, k)
				repr.AddMatchForRule(ruleName, Match{
					Name:  matchName,
					Match: match,
				})
			}
			for k, bRef := range rule.BackendRefs {
				bRefName := NameFromHTTPRoute(route, "", i, j, k)
				repr.AddBackenRefForRule(ruleName, BackendRef{
					Name:       bRefName,
					BackendRef: bRef,
				})
			}
		}
	}

	return &repr
}

func NameFromHTTPRoute(route *gwtypes.HTTPRoute, prefix string, indexes ...int) Name {
	if prefix == "" {
		prefix = "httproute"
	}
	// Only take up to 3 indexes
	if len(indexes) > 3 {
		indexes = indexes[:3]
	}
	return Name{
		prefix:    prefix,
		namespace: route.Namespace,
		name:      route.Name,
		indexes:   indexes,
	}
}

func (t *HTTPRouteRepresentation) AddMatchForRule(rName Name, match Match) {
	if t.Rules == nil {
		t.Rules = make(map[string]Rule)
	}
	ruleKey := rName.String()
	rule, exists := t.Rules[ruleKey]
	if !exists {
		rule = Rule{
			Name:    rName,
			Matches: make(map[string]Match),
		}
	}
	matchKey := match.Name.String()
	rule.Matches[matchKey] = match
	t.Rules[ruleKey] = rule
}

func (t *HTTPRouteRepresentation) AddBackenRefForRule(rName Name, backendRef BackendRef) {
	if t.Rules == nil {
		t.Rules = make(map[string]Rule)
	}
	ruleKey := rName.String()
	rule, exists := t.Rules[ruleKey]
	if !exists {
		rule = Rule{
			Name:        rName,
			Matches:     make(map[string]Match),
			BackendRefs: make(map[string]BackendRef),
		}
	}
	if rule.BackendRefs == nil {
		rule.BackendRefs = make(map[string]BackendRef)
	}
	backendRefKey := backendRef.Name.String()
	rule.BackendRefs[backendRefKey] = backendRef
	t.Rules[ruleKey] = rule
}

func (t *HTTPRouteRepresentation) AddParentRef(parentRef ParentReference) {
	if t.ParentRefs == nil {
		t.ParentRefs = make(map[string]ParentReference)
	}
	t.ParentRefs[parentRef.Name.String()] = parentRef
}

func (t *HTTPRouteRepresentation) GetParentRefByName(name Name) *gwtypes.ParentReference {
	if t.ParentRefs == nil {
		return nil
	}

	// Since the name for the parentRef is in the format prefix.<namespace>.<name>[.<parentRefIndex>]
	// we make sure that when we receive a name from a different resource for which we want to get the parentRef we
	// remove the unneeded indexes.
	name.indexes = name.indexes[:1]

	if pRef, ok := t.ParentRefs[name.String()]; ok {
		return &pRef.ParentReference
	}
	return nil
}

func (t *HTTPRouteRepresentation) AddControlPlaneRef(cpr ControlPlaneRef) {
	if t.ControlPlaneRefs == nil {
		t.ControlPlaneRefs = make(map[string]ControlPlaneRef)
	}

	t.ControlPlaneRefs[cpr.Name.String()] = cpr
}

func (t *HTTPRouteRepresentation) GetControlPlaneRefByName(name Name) *commonv1alpha1.ControlPlaneRef {
	if t.ControlPlaneRefs == nil {
		return nil
	}

	// Since the name for the controlPlaneRef is in the format prefix.<namespace>.<name>[.<parentRefIndex>]
	// we make sure that when we receive a name from a different resources for which we want to get the controlPlaneRef we
	// remove the unneeded indexes.
	name.indexes = name.indexes[:1]

	if cpr, ok := t.ControlPlaneRefs[name.String()]; ok {
		return cpr.ControlPlaneRef
	}
	return nil
}
