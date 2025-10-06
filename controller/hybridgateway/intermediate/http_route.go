package intermediate

import (
	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

// Rule represents a single HTTPRoute rule with its associated matches and backend references.
// It encapsulates the routing logic for a specific rule within an HTTPRoute.
type Rule struct {
	Name
	Matches     map[string]Match
	Filters     map[string]Filter
	BackendRefs map[string]BackendRef
}

// Match represents a single HTTPRoute match condition.
// It contains the match criteria that determine when a request should be processed by this match.
type Match struct {
	Name
	Match gwtypes.HTTPRouteMatch
}

// BackendRef represents a backend reference within an HTTPRoute rule.
// It defines the target backend service that should handle requests matching this reference.
type BackendRef struct {
	Name
	BackendRef gwtypes.HTTPBackendRef
}

// Filter represents a filter applied to an HTTPRoute rule.
// It defines transformations or actions to be performed on requests that match the rule.
type Filter struct {
	Name
	Filter gwtypes.HTTPRouteFilter
}

// ControlPlaneRef represents a control plane reference for Kong configuration.
// It associates Kong entities with their corresponding control plane instance.
type ControlPlaneRef struct {
	Name
	ControlPlaneRef *commonv1alpha1.ControlPlaneRef
}

// ParentReference represents a parent reference in an HTTPRoute.
// It defines the Gateway or other parent resource that this HTTPRoute is attached to.
type ParentReference struct {
	Name
	ParentReference gwtypes.ParentReference
}

// Hostnames represents a collection of hostnames associated with an HTTPRoute.
type Hostnames struct {
	Name
	Hostnames []string
}

// HTTPRouteRepresentation provides an intermediate representation of an HTTPRoute resource.
// It organizes HTTPRoute data into a structured format that facilitates conversion to Kong entities.
// This representation includes rules, hostnames, control plane references, parent references, and routing options.
type HTTPRouteRepresentation struct {
	Rules            map[string]Rule
	Hostnames        map[string]Hostnames
	ControlPlaneRefs map[string]ControlPlaneRef
	ParentRefs       map[string]ParentReference
	StripPath        bool
}

func newRule(name Name) Rule {
	return Rule{
		Name:        name,
		Matches:     make(map[string]Match),
		Filters:     make(map[string]Filter),
		BackendRefs: make(map[string]BackendRef),
	}
}

// NewHTTPRouteRepresentation creates a new HTTPRouteRepresentation from an HTTPRoute resource.
// It initializes all the internal maps and extracts configuration like strip-path from annotations.
func NewHTTPRouteRepresentation(route *gwtypes.HTTPRoute) *HTTPRouteRepresentation {
	repr := HTTPRouteRepresentation{
		Rules:            map[string]Rule{},
		Hostnames:        map[string]Hostnames{},
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
			for k, filter := range rule.Filters {
				filterName := NameFromHTTPRoute(route, "", i, j, k)
				repr.AddFilterForRule(ruleName, Filter{
					Name:   filterName,
					Filter: filter,
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

// NameFromHTTPRoute creates a Name instance from an HTTPRoute resource with optional prefix and indexes.
// The prefix defaults to "httproute" if empty. Indexes are limited to 3 elements maximum.
// This function is used to generate consistent names for Kong entities derived from HTTPRoute resources.
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

// AddMatchForRule adds a match condition to a specific rule in the HTTPRoute representation.
// If the rule doesn't exist, it creates a new rule with the provided name.
// The match is stored using its name as the key for easy retrieval.
func (t *HTTPRouteRepresentation) AddMatchForRule(rName Name, match Match) {
	if t.Rules == nil {
		t.Rules = make(map[string]Rule)
	}
	ruleKey := rName.String()
	rule, exists := t.Rules[ruleKey]
	if !exists {
		rule = newRule(rName)
	}
	if rule.Matches == nil {
		rule.Matches = make(map[string]Match)
	}
	matchKey := match.String()
	rule.Matches[matchKey] = match
	t.Rules[ruleKey] = rule
}

// AddFilterForRule adds a filter to a specific rule in the HTTPRoute representation.
// If the rule doesn't exist, it creates a new rule with the provided name.
// The filter is stored using its name as the key for easy retrieval.
func (t *HTTPRouteRepresentation) AddFilterForRule(rName Name, filter Filter) {
	if t.Rules == nil {
		t.Rules = make(map[string]Rule)
	}
	ruleKey := rName.String()
	rule, exists := t.Rules[ruleKey]
	if !exists {
		rule = newRule(rName)
	}
	if rule.Filters == nil {
		rule.Filters = make(map[string]Filter)
	}
	filterKey := filter.String()
	rule.Filters[filterKey] = filter
	t.Rules[ruleKey] = rule
}

// AddBackenRefForRule adds a backend reference to a specific rule in the HTTPRoute representation.
// If the rule doesn't exist, it creates a new rule with the provided name.
// The backend reference is stored using its name as the key for easy retrieval.
func (t *HTTPRouteRepresentation) AddBackenRefForRule(rName Name, backendRef BackendRef) {
	if t.Rules == nil {
		t.Rules = make(map[string]Rule)
	}
	ruleKey := rName.String()
	rule, exists := t.Rules[ruleKey]
	if !exists {
		rule = newRule(rName)
	}
	if rule.BackendRefs == nil {
		rule.BackendRefs = make(map[string]BackendRef)
	}
	backendRefKey := backendRef.String()
	rule.BackendRefs[backendRefKey] = backendRef
	t.Rules[ruleKey] = rule
}

// AddParentRef adds a parent reference to the HTTPRoute representation.
// Parent references define the Gateway or other parent resource that this HTTPRoute is attached to.
func (t *HTTPRouteRepresentation) AddParentRef(parentRef ParentReference) {
	if t.ParentRefs == nil {
		t.ParentRefs = make(map[string]ParentReference)
	}
	t.ParentRefs[parentRef.String()] = parentRef
}

// GetParentRefByName retrieves a parent reference by name from the HTTPRoute representation.
// It normalizes the name by keeping only the parent reference index to ensure proper matching.
// Returns nil if no parent reference is found with the given name.
func (t *HTTPRouteRepresentation) GetParentRefByName(name Name) *gwtypes.ParentReference {
	if t.ParentRefs == nil {
		return nil
	}

	// Since the name for the parentRef is in the format prefix.<namespace>.<name>[.<parentRefIndex>]
	// while all the other resources have one more index (to identify different rules) remove the last
	// index before the lookup.
	name.indexes = name.indexes[:1]

	if pRef, ok := t.ParentRefs[name.String()]; ok {
		return &pRef.ParentReference
	}
	return nil
}

// AddHostnames adds a hostname to the HTTPRoute representation.
func (t *HTTPRouteRepresentation) AddHostnames(hostnames Hostnames) {
	if t.Hostnames == nil {
		t.Hostnames = make(map[string]Hostnames)
	}
	t.Hostnames[hostnames.String()] = hostnames
}

// GetHostnamesByName retrieves hostnames by name from the HTTPRoute representation.
// Returns nil if no hostnames are found with the given name.
func (t *HTTPRouteRepresentation) GetHostnamesByName(name Name) *Hostnames {
	if t.Hostnames == nil {
		return nil
	}

	// Since the name for the Hostnames is in the format prefix.<namespace>.<name>[.<parentRefIndex>]
	// while all the other resources have one more index (to identify different rules) remove the last
	// index before the lookup.
	name.indexes = name.indexes[:1]
	if h, ok := t.Hostnames[name.String()]; ok {
		return &h
	}
	return nil
}

// AddControlPlaneRef adds a control plane reference to the HTTPRoute representation.
// Control plane references associate Kong entities with their corresponding control plane instance.
func (t *HTTPRouteRepresentation) AddControlPlaneRef(cpr ControlPlaneRef) {
	if t.ControlPlaneRefs == nil {
		t.ControlPlaneRefs = make(map[string]ControlPlaneRef)
	}

	t.ControlPlaneRefs[cpr.String()] = cpr
}

// GetControlPlaneRefByName retrieves a control plane reference by name from the HTTPRoute representation.
// It normalizes the name by keeping only the parent reference index to ensure proper matching.
// Returns nil if no control plane reference is found with the given name.
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
