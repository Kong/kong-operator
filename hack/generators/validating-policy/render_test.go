package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	portsPolicy = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: ports.example
spec: {}`
	portsBinding = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: binding-ports.example
spec:
  policyName: ports.example
  validationActions:
  - Deny`
	pdbPolicy = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: pdb.example
spec: {}`
	pdbBinding = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: binding-pdb.example
spec:
  policyName: pdb.example
  validationActions:
  - Warn
  - Audit`
	pdbBindingTemplated = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: binding-pdb.example
spec:
  policyName: pdb.example
  validationActions:
  {{- toYaml .Values.global.validatingPolicies.pdbValue.validationActions | nindent 2 }}`
)

func TestRenderChartTemplate(t *testing.T) {
	testCases := []struct {
		name          string
		kustomized    string
		helmValueName string
		overrides     []policyOverride
		expected      string
		expectedErr   string
	}{
		{
			name:          "no overrides wraps everything in folder toggle",
			kustomized:    portsPolicy + "\n---\n" + portsBinding + "\n",
			helmValueName: "portsValue",
			expected:      wrapInIfEnabled("portsValue", portsPolicy+"\n---\n"+portsBinding+"\n"),
		},
		{
			name:          "override gets its own toggle and templated actions",
			kustomized:    pdbPolicy + "\n---\n" + portsPolicy + "\n---\n" + pdbBinding + "\n---\n" + portsBinding + "\n",
			helmValueName: "portsValue",
			overrides:     []policyOverride{{policyName: "pdb.example", helmValueName: "pdbValue"}},
			expected: "{{- if .Values.global.validatingPolicies.portsValue.enabled }}\n" +
				portsPolicy + "\n---\n" + portsBinding + "\n" +
				"{{- end }}\n" +
				"---\n" +
				"{{- if .Values.global.validatingPolicies.pdbValue.enabled }}\n" +
				pdbPolicy + "\n---\n" + pdbBindingTemplated + "\n" +
				"{{- end }}\n",
		},
		{
			name:       "only overridden policies need no folder toggle",
			kustomized: pdbPolicy + "\n---\n" + pdbBinding + "\n",
			overrides:  []policyOverride{{policyName: "pdb.example", helmValueName: "pdbValue"}},
			expected: "{{- if .Values.global.validatingPolicies.pdbValue.enabled }}\n" +
				pdbPolicy + "\n---\n" + pdbBindingTemplated + "\n" +
				"{{- end }}\n",
		},
		{
			name:          "override policy missing from kustomize output",
			kustomized:    portsPolicy + "\n",
			overrides:     []policyOverride{{policyName: "pdb.example", helmValueName: "pdbValue"}},
			helmValueName: "portsValue",
			expectedErr:   `policy "pdb.example" not found`,
		},
		{
			name:        "not overridden documents without folder toggle",
			kustomized:  portsPolicy + "\n---\n" + pdbPolicy + "\n---\n" + pdbBinding + "\n",
			overrides:   []policyOverride{{policyName: "pdb.example", helmValueName: "pdbValue"}},
			expectedErr: "no folder Helm value",
		},
		{
			name:        "overridden binding without validationActions",
			kustomized:  pdbPolicy + "\n---\napiVersion: admissionregistration.k8s.io/v1\nkind: ValidatingAdmissionPolicyBinding\nmetadata:\n  name: binding-pdb.example\nspec:\n  policyName: pdb.example\n",
			overrides:   []policyOverride{{policyName: "pdb.example", helmValueName: "pdbValue"}},
			expectedErr: "validationActions not found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := renderChartTemplate(tc.kustomized, tc.helmValueName, tc.overrides)
			if tc.expectedErr != "" {
				require.ErrorContains(t, err, tc.expectedErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, out)
		})
	}
}
