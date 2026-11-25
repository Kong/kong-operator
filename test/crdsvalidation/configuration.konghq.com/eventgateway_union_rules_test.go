package configuration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// TestEventGatewayUnionRules sends raw JSON, as kubectl does.
func TestEventGatewayUnionRules(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get())
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)

	staticKey := map[string]any{"type": "static", "static": map[string]any{"key": map[string]any{"name": "key"}}}
	tests := []struct {
		name    string
		base    string
		path    []string
		value   any
		wantErr string
	}{
		{name: "stray member next to the policy type", base: "encryptPolicy", path: []string{"apiSpec", "modifyHeaders"},
			value:   map[string]any{"name": "h", "config": map[string]any{"actions": []any{map[string]any{"op": "set", "set": map[string]any{"key": "k", "value": "v"}}}}},
			wantErr: "modifyHeaders must be set when type is modifyHeaders, and only then"},
		{name: "policy type without its member", base: "encryptPolicy", path: []string{"apiSpec"},
			value: map[string]any{"type": "encrypt"}, wantErr: "encrypt must be set when type is encrypt, and only then"},
		{name: "stray member in backend cluster authentication", base: "backendCluster", path: []string{"apiSpec", "authentication"},
			value:   map[string]any{"type": "anonymous", "anonymous": map[string]any{}, "saslPlain": map[string]any{"username": "u", "password": "p"}},
			wantErr: "saslPlain must be set when type is saslPlain, and only then"},
		{name: "backend cluster authentication without its member", base: "backendCluster", path: []string{"apiSpec", "authentication"},
			value: map[string]any{"type": "saslPlain"}, wantErr: "saslPlain must be set when type is saslPlain, and only then"},
		{name: "backend cluster anonymous without its empty member", base: "backendCluster", path: []string{"apiSpec", "authentication"},
			value: map[string]any{"type": "anonymous"}},
		{name: "stray member in virtual cluster authentication", base: "virtualCluster", path: []string{"apiSpec", "authentication"},
			value:   []any{map[string]any{"type": "anonymous", "saslScram": map[string]any{"algorithm": "sha256"}}},
			wantErr: "saslScram must be set when type is saslScram, and only then"},
		{name: "virtual cluster anonymous without a member", base: "virtualCluster", path: []string{"apiSpec", "authentication"},
			value: []any{map[string]any{"type": "anonymous"}}},
		{name: "encryption key type aws with a static member", base: "encryptFieldsPolicy", path: []string{"apiSpec", "encryptFields", "config", "encryptFields"},
			value: []any{map[string]any{
				"paths":         map[string]any{"type": "array", "array": []any{map[string]any{"match": "ssn"}}},
				"encryptionKey": map[string]any{"type": "aws", "static": staticKey["static"]},
			}}, wantErr: "aws must be set when type is aws, and only then"},
		{name: "mask strategy type replace with a keepChars member", base: "maskFieldsPolicy", path: []string{"apiSpec", "maskFields", "config", "maskFields"},
			value: []any{map[string]any{
				"paths":    map[string]any{"type": "array", "array": []any{map[string]any{"match": "ssn"}}},
				"strategy": map[string]any{"type": "replace", "replace": map[string]any{"phrase": "*"}, "keepChars": map[string]any{"phrase": "*"}},
			}}, wantErr: "keepChars must be set when type is keepChars, and only then"},
		{name: "paths type expression with an array member", base: "maskFieldsPolicy", path: []string{"apiSpec", "maskFields", "config", "maskFields"},
			value: []any{map[string]any{
				"paths":    map[string]any{"type": "expression", "expression": "{{ ['ssn'] }}", "array": []any{map[string]any{"match": "ssn"}}},
				"strategy": map[string]any{"type": "replace", "replace": map[string]any{"phrase": "*"}},
			}}, wantErr: "array must be set when type is array, and only then"},
		{name: "paths type expression without an expression", base: "maskFieldsPolicy", path: []string{"apiSpec", "maskFields", "config", "maskFields"},
			value: []any{map[string]any{
				"paths":    map[string]any{"type": "expression"},
				"strategy": map[string]any{"type": "replace", "replace": map[string]any{"phrase": "*"}},
			}}, wantErr: "expression must be set when type is expression, and only then"},
		{name: "ACL resource names type dynam without dynam", base: "aclPolicy", path: []string{"apiSpec", "acls", "config", "rules"},
			value: []any{map[string]any{
				"action": "allow", "resourceType": "topic", "operations": []any{map[string]any{"name": "all"}},
				"resourceNames": map[string]any{"type": "dynam"},
			}}, wantErr: "dynam must be set when type is dynam, and only then"},
		{name: "ACL resource names type dynam with dynam", base: "aclPolicy", path: []string{"apiSpec", "acls", "config", "rules"},
			value: []any{map[string]any{
				"action": "allow", "resourceType": "topic", "operations": []any{map[string]any{"name": "all"}},
				"resourceNames": map[string]any{"type": "dynam", "dynam": "{{ context.auth.token.claims['topics'] }}"},
			}}},
		{name: "listener policy type without its member", base: "tlsListenerPolicy", path: []string{"apiSpec"},
			value: map[string]any{"type": "tlsServer"}, wantErr: "tlsServer must be set when type is tlsServer, and only then"},
		{name: "valid policies are admitted", base: "encryptPolicy"},
		{name: "valid TLS listener policy is admitted", base: "tlsListenerPolicy"},
		{name: "valid mask fields policy is admitted", base: "maskFieldsPolicy"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{}
			require.NoError(t, yaml.Unmarshal([]byte(eventGatewayArrayBases[tc.base]), &obj.Object))
			obj.SetAPIVersion("configuration.konghq.com/v1alpha1")
			obj.SetNamespace(ns.Name)
			obj.SetGenerateName("union-")
			if tc.path != nil {
				require.NoError(t, unstructured.SetNestedField(obj.Object, tc.value, append([]string{"spec"}, tc.path...)...))
			}
			err := cl.Create(ctx, obj, client.DryRunAll)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
