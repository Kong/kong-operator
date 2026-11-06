package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"
)

// policyOverride gates a single policy and its binding behind their own Helm value
// and templates the binding's validationActions from that value.
type policyOverride struct {
	policyName    string
	helmValueName string
}

type policyDocument struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		PolicyName string `json:"policyName"`
	} `json:"spec"`
}

func (d policyDocument) policyName() string {
	if d.Kind == "ValidatingAdmissionPolicyBinding" {
		return d.Spec.PolicyName
	}
	return d.Metadata.Name
}

var validationActionsRe = regexp.MustCompile(`(?m)^  validationActions:\n(?:  - .*(?:\n|$))+`)

// renderChartTemplate turns the kustomize output of a validating policies folder into a chart template.
func renderChartTemplate(kustomized, helmValueName string, overrides []policyOverride) (string, error) {
	if len(overrides) == 0 {
		return wrapInIfEnabled(helmValueName, kustomized), nil
	}

	var (
		rest       []string
		overridden = make([][]string, len(overrides))
	)
	for doc := range strings.SplitSeq(strings.TrimSuffix(kustomized, "\n"), "\n---\n") {
		var d policyDocument
		if err := yaml.Unmarshal([]byte(doc), &d); err != nil {
			return "", fmt.Errorf("failed to parse document: %w", err)
		}
		i := slices.IndexFunc(overrides, func(o policyOverride) bool { return o.policyName == d.policyName() })
		if i < 0 {
			rest = append(rest, doc)
			continue
		}
		if d.Kind == "ValidatingAdmissionPolicyBinding" {
			templated, err := templateValidationActions(doc, overrides[i].helmValueName, overrides[i].policyName)
			if err != nil {
				return "", err
			}
			doc = templated
		}
		overridden[i] = append(overridden[i], doc)
	}

	var blocks []string
	if len(rest) > 0 {
		if helmValueName == "" {
			return "", fmt.Errorf("documents not covered by overrides but no folder Helm value set")
		}
		blocks = append(blocks, wrapBlockInIfEnabled(helmValueName, rest))
	}
	for i, o := range overrides {
		if len(overridden[i]) == 0 {
			return "", fmt.Errorf("policy %q not found in kustomize output", o.policyName)
		}
		blocks = append(blocks, wrapBlockInIfEnabled(o.helmValueName, overridden[i]))
	}
	return strings.Join(blocks, "---\n"), nil
}

// wrapBlockInIfEnabled is like wrapInIfEnabled but keeps the trailing newline,
// so that blocks can be joined with document separators.
func wrapBlockInIfEnabled(valueName string, docs []string) string {
	return fmt.Sprintf("{{- if .Values.global.validatingPolicies.%s.enabled }}\n%s\n{{- end }}\n",
		valueName, strings.Join(docs, "\n---\n"))
}

func templateValidationActions(doc, valueName, policyName string) (string, error) {
	if !validationActionsRe.MatchString(doc) {
		return "", fmt.Errorf("validationActions not found in binding for %q", policyName)
	}
	return validationActionsRe.ReplaceAllStringFunc(doc, func(m string) string {
		r := fmt.Sprintf("  validationActions:\n  {{- toYaml .Values.global.validatingPolicies.%s.validationActions | nindent 2 }}", valueName)
		if strings.HasSuffix(m, "\n") {
			r += "\n"
		}
		return r
	}), nil
}
