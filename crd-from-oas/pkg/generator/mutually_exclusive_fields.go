package generator

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// markerAtMostOneOf renders the controller-gen type marker that allows at most
// one of the given JSON fields to be set.
func markerAtMostOneOf(fields []string) string {
	return "+kubebuilder:validation:AtMostOneOf=" + strings.Join(fields, ";")
}

// mutuallyExclusiveFieldsRules returns the indexes of the configured
// MutuallyExclusiveFields rules that apply to the generated struct type goName.
// A rule applies when one of its schema patterns matches goName and the struct
// emits every listed field as an optional field. Schemas missing a field are
// skipped so the marker never names a field that isn't there. Schemas
// requiring a field are skipped because controller-gen rejects AtMostOneOf on
// required fields, and a required field leaves the others nothing to exclude.
func (g *Generator) mutuallyExclusiveFieldsRules(
	goName, refName string,
	schema *parser.Schema,
	unionMemberDiscriminators map[string]map[string]struct{},
) []int {
	if len(g.config.MutuallyExclusiveFields) == 0 || schema == nil || len(schema.Properties) == 0 {
		return nil
	}

	var optional map[string]struct{}
	var matched []int
	for i, rule := range g.config.MutuallyExclusiveFields {
		if !matchesAnySchemaPattern(rule.Schemas, goName) {
			continue
		}
		if optional == nil {
			optional = make(map[string]struct{}, len(schema.Properties))
			for _, prop := range schema.Properties {
				if g.shouldSkipSchemaProperty(goName, prop) || shouldSkipUnionMemberDiscriminator(refName, prop, unionMemberDiscriminators) {
					continue
				}
				// Mirrors the requiredness KubebuilderTags emits.
				if prop.Required && !prop.Nullable {
					continue
				}
				optional[jsonTagForProperty(prop)] = struct{}{}
			}
		}
		hasAll := true
		for _, field := range rule.Fields {
			if _, ok := optional[field]; !ok {
				hasAll = false
				break
			}
		}
		if hasAll {
			matched = append(matched, i)
		}
	}
	return matched
}

// mutuallyExclusiveFieldsMarkers returns the AtMostOneOf markers for the
// generated struct type goName.
func (g *Generator) mutuallyExclusiveFieldsMarkers(
	goName, refName string,
	schema *parser.Schema,
	unionMemberDiscriminators map[string]map[string]struct{},
) []string {
	rules := g.mutuallyExclusiveFieldsRules(goName, refName, schema, unionMemberDiscriminators)
	markers := make([]string, 0, len(rules))
	for _, i := range rules {
		markers = append(markers, markerAtMostOneOf(g.config.MutuallyExclusiveFields[i].Fields))
	}
	return markers
}

// validateMutuallyExclusiveFields fails when a configured rule applies to none
// of the emitted schema struct types, e.g. after an upstream OAS rename or a
// typo in the config.
func (g *Generator) validateMutuallyExclusiveFields(referencedSchemas map[string]bool, parsed *parser.ParsedSpec) error {
	if len(g.config.MutuallyExclusiveFields) == 0 {
		return nil
	}

	unionMemberDiscriminators := collectUnionMemberDiscriminators(parsed)
	used := make([]bool, len(g.config.MutuallyExclusiveFields))
	for refName := range referencedSchemas {
		schema, ok := parsed.Schemas[refName]
		if !ok {
			continue
		}
		for _, i := range g.mutuallyExclusiveFieldsRules(fixInitialisms(refName), refName, schema, unionMemberDiscriminators) {
			used[i] = true
		}
	}

	var unused []string
	for i, ok := range used {
		if ok {
			continue
		}
		rule := g.config.MutuallyExclusiveFields[i]
		unused = append(unused, fmt.Sprintf(
			"mutuallyExclusiveFields[%d] (schemas %v, fields %v)", i, rule.Schemas, rule.Fields,
		))
	}
	if len(unused) > 0 {
		sort.Strings(unused)
		return fmt.Errorf("no generated schema type has all fields as optional fields for %s", strings.Join(unused, ", "))
	}
	return nil
}

func matchesAnySchemaPattern(patterns []string, goName string) bool {
	for _, pattern := range patterns {
		if ok, err := path.Match(pattern, goName); err == nil && ok {
			return true
		}
	}
	return false
}
