package configuration_test

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestEventGatewayVirtualClusterConsumePolicyChain(t *testing.T) {
	t.Parallel()

	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, t.Context(), scheme)
	chain := func(policies ...configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef) *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain {
		return &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChainSpec{
				EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "virtual-cluster"},
				},
				APISpec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChainAPISpec{Policies: policies},
			},
		}
	}
	common.TestCasesGroup[*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain]{
		{Name: "explicit empty policy list", TestObject: chain([]configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{}...)},
		{
			Name:       "parent cannot change before programming",
			TestObject: chain([]configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{}...),
			Update: func(obj *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain) {
				obj.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Name = "other"
			},
			ExpectedUpdateErrorMessage: new("spec.eventGatewayVirtualClusterRef is immutable"),
		},
		{
			Name:       "parent namespace cannot change before programming",
			TestObject: chain([]configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{}...),
			Update: func(obj *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain) {
				obj.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new(ns.Name)
			},
			ExpectedUpdateErrorMessage: new("spec.eventGatewayVirtualClusterRef is immutable"),
		},
		{
			Name:       "explicit kind and same namespace",
			TestObject: chain(configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{Name: "policy", Kind: "EventGatewayVirtualClusterConsumePolicy", Namespace: ns.Name}),
		},
		{
			Name: "ordered list can be replaced",
			TestObject: chain(configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{Name: "first"},
				configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{Name: "second"}),
			Update: func(obj *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain) {
				policies := obj.Spec.APISpec.Policies
				policies[0], policies[1] = policies[1], policies[0]
			},
		},
		{
			Name:       "independent ID rejected",
			TestObject: chain([]configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{}...),
			StatusUpdate: func(obj *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain) {
				obj.Status.ID = "00000000-0000-4000-8000-000000000000"
			},
			ExpectedStatusUpdateErrorMessage: new("status.id must be unset because policy chains have no independent Konnect ID"),
		},
		{
			Name:                 "wrong policy kind",
			TestObject:           chain(configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{Name: "policy", Kind: "EventGatewayVirtualClusterProducePolicy"}),
			ExpectedErrorMessage: new(`supported values: "EventGatewayVirtualClusterConsumePolicy"`),
		},
		{
			Name:                 "empty policy name",
			TestObject:           chain(configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{}),
			ExpectedErrorMessage: new("should be at least 1 chars long"),
		},
	}.RunWithConfig(t, cfg, scheme)

	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)
	for _, field := range [][]string{
		{"spec"}, {"spec", "apiSpec"}, {"spec", "apiSpec", "policies"}, {"spec", "eventGatewayVirtualClusterRef"},
	} {
		for _, null := range []bool{false, true} {
			obj := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": configurationv1alpha1.GroupVersion.String(),
				"kind":       "EventGatewayVirtualClusterConsumePolicyChain",
				"metadata":   map[string]any{"namespace": ns.Name, "generateName": "consume-chain-"},
				"spec": map[string]any{
					"eventGatewayVirtualClusterRef": map[string]any{
						"type": "namespacedRef", "namespacedRef": map[string]any{"name": "virtual-cluster"},
					},
					"apiSpec": map[string]any{"policies": []any{}},
				},
			}}
			if null {
				require.NoError(t, unstructured.SetNestedField(obj.Object, nil, field...))
			} else {
				unstructured.RemoveNestedField(obj.Object, field...)
			}
			err := cl.Create(t.Context(), obj)
			require.True(t, apierrors.IsInvalid(err), "field %v null=%t: %v", field, null, err)
			require.ErrorContains(t, err, field[len(field)-1])
		}
	}
	obj := chain([]configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{}...)
	obj.Spec.EventGatewayVirtualClusterRef = commonv1alpha1.ObjectRef{
		Type: commonv1alpha1.ObjectRefTypeKonnectID, KonnectID: new("00000000-0000-4000-8000-000000000000"),
	}
	require.ErrorContains(t, cl.Create(t.Context(), obj), "spec.eventGatewayVirtualClusterRef must use namespacedRef")
}

func TestEventGatewayVirtualClusterConsumePolicyChainSample(t *testing.T) {
	t.Parallel()

	cfg, ns := envtest.Setup(t, t.Context(), scheme.Get())
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)
	file, err := os.Open("../../../config/samples/konnect_eventgateway_virtualcluster_consume_policy_chain.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	decoder := yaml.NewYAMLOrJSONDecoder(file, 4096)
	var resources []string
	for {
		obj := &unstructured.Unstructured{}
		err := decoder.Decode(obj)
		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)
		obj.SetNamespace(ns.Name)
		require.NoError(t, cl.Create(t.Context(), obj), "%s %s", obj.GetKind(), obj.GetName())
		resources = append(resources, obj.GetKind()+"/"+obj.GetName())
	}
	require.ElementsMatch(t, []string{
		"KonnectAPIAuthConfiguration/consume-chain-auth",
		"KonnectEventGateway/consume-chain-gateway",
		"EventGatewayBackendCluster/consume-chain-backend",
		"EventGatewayVirtualCluster/consume-chain-virtual-cluster",
		"EventGatewayVirtualClusterConsumePolicy/consume-chain-parse-first",
		"EventGatewayVirtualClusterConsumePolicy/consume-chain-headers",
		"EventGatewayVirtualClusterConsumePolicy/consume-chain-decrypt",
		"EventGatewayVirtualClusterConsumePolicy/consume-chain-parse-second",
		"EventGatewayVirtualClusterConsumePolicy/consume-chain-skip",
		"EventGatewayVirtualClusterConsumePolicy/consume-chain-root",
		"EventGatewayVirtualClusterConsumePolicyChain/consume-chain-example",
	}, resources)
	var chain configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns.Name, Name: "consume-chain-example"}, &chain))
	require.Equal(t, []configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{
		{Name: "consume-chain-parse-second", Kind: "EventGatewayVirtualClusterConsumePolicy"},
		{Name: "consume-chain-skip", Kind: "EventGatewayVirtualClusterConsumePolicy"},
		{Name: "consume-chain-parse-first", Kind: "EventGatewayVirtualClusterConsumePolicy"},
		{Name: "consume-chain-decrypt", Kind: "EventGatewayVirtualClusterConsumePolicy"},
		{Name: "consume-chain-headers", Kind: "EventGatewayVirtualClusterConsumePolicy"},
		{Name: "consume-chain-root", Kind: "EventGatewayVirtualClusterConsumePolicy"},
	}, chain.Spec.APISpec.Policies, "admission defaults kind and preserves the complete nested-aware order")
	for name, parent := range map[string]string{
		"consume-chain-headers": "consume-chain-parse-first",
		"consume-chain-decrypt": "consume-chain-parse-first",
		"consume-chain-skip":    "consume-chain-parse-second",
	} {
		var child configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns.Name, Name: name}, &child))
		require.Equal(t, parent, child.Spec.APISpec.ParentPolicyRef.Name)
		require.Equal(t, "EventGatewayVirtualClusterConsumePolicy", child.Spec.APISpec.ParentPolicyRef.Kind)
		require.Equal(t, chain.GetParentRef(), child.GetParentRef())
	}
}

func TestEventGatewayVirtualClusterConsumePolicyChainShellFixturesAdmission(t *testing.T) {
	t.Parallel()
	cfg, ns := envtest.Setup(t, t.Context(), scheme.Get())
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)
	dir := t.TempDir()
	shim := filepath.Join(dir, "kubectl")
	require.NoError(t, os.WriteFile(shim, []byte(`#!/bin/bash
set -euo pipefail
test "$1" == apply
cat >> "$APPLIED_FILE"
printf '\n---\n' >> "$APPLIED_FILE"
`), 0o600))
	require.NoError(t, os.Chmod(shim, 0o700))
	fixtures := filepath.Join(dir, "fixtures.yaml")
	cmd := exec.CommandContext(t.Context(), "bash", "-c", `
set -euo pipefail
source ../../e2e/chainsaw/eventgateway/consumepolicychain-lifecycle/scripts/helpers.sh
create_policy root modifyHeaders
create_policy parse-b schemaValidation
create_policy skip skipRecord parse-b
create_policy parse-a schemaValidation
create_policy first modifyHeaders parse-a
create_policy second modifyHeaders parse-a
create_policy decrypted decryptFields parse-a
create_chain chain '["parse-a","decrypted","second","first","root","parse-b","skip"]'
create_chain produce-chain '[]' EventGatewayVirtualClusterProducePolicyChain
create_chain cluster-chain '[]' EventGatewayVirtualClusterPolicyChain
VIRTUAL_CLUSTER_NAME=empty-vc create_chain empty-chain '[]'
`)
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"APPLIED_FILE="+fixtures, "NAMESPACE="+ns.Name, "VIRTUAL_CLUSTER_NAME=chain-vc", "TEST_NAME=consume-fixtures")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	file, err := os.Open(fixtures)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	decoder := yaml.NewYAMLOrJSONDecoder(file, 4096)
	var count int
	for {
		obj := &unstructured.Unstructured{}
		err := decoder.Decode(obj)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if len(obj.Object) == 0 {
			continue
		}
		require.NoError(t, cl.Create(t.Context(), obj), "actual shell fixture: %s/%s", obj.GetKind(), obj.GetName())
		count++
	}
	require.Equal(t, 11, count)
	var chain configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns.Name, Name: "chain"}, &chain))
	require.Len(t, chain.Spec.APISpec.Policies, 7)
	for _, name := range []string{"first", "second", "decrypted", "skip"} {
		var child configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns.Name, Name: name}, &child))
		wantParent := "parse-a"
		if name == "skip" {
			wantParent = "parse-b"
		}
		require.Equal(t, wantParent, child.Spec.APISpec.ParentPolicyRef.Name)
		require.Equal(t, "EventGatewayVirtualClusterConsumePolicy", child.Spec.APISpec.ParentPolicyRef.Kind)
		require.Equal(t, chain.GetParentRef(), child.GetParentRef())
	}
}
