package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// TestEventGatewayReferencesToOnPremEntities verifies that a policy referencing
// an entity attached to an OnPremEventGateway, which never gets a Konnect ID,
// fails with ReferenceNotKonnectManagedError instead of waiting for it to be
// programmed in Konnect.
func TestEventGatewayReferencesToOnPremEntities(t *testing.T) {
	const ns = "default"
	onPremRef := &commonv1alpha1.GatewayRef{Group: "eventgateway.konghq.com", Kind: "OnPremEventGateway", Name: "onprem-gw"}

	newClient := func(t *testing.T) client.Client {
		t.Helper()
		scheme := runtime.NewScheme()
		require.NoError(t, AddToScheme(scheme))
		return fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&EventGatewaySchemaRegistry{Name: "registry", Namespace: ns, Spec: EventGatewaySchemaRegistrySpec{ParentRef: onPremRef}},
			&EventGatewayStaticKey{Name: "key", Namespace: ns, Spec: EventGatewayStaticKeySpec{ParentRef: onPremRef}},
			&EventGatewayTLSTrustBundle{Name: "bundle", Namespace: ns, Spec: EventGatewayTLSTrustBundleSpec{ParentRef: onPremRef}},
		).Build()
	}

	unmarshal := func(t *testing.T, data string, obj any) {
		t.Helper()
		require.NoError(t, yaml.Unmarshal([]byte(data), obj))
	}

	t.Run("consume policy referencing a schema registry", func(t *testing.T) {
		policy := &EventGatewayVirtualClusterConsumePolicy{}
		unmarshal(t, `
metadata: {name: consume, namespace: default}
spec:
  apiSpec:
    type: schemaValidation
    schemaValidation:
      config:
        type: json
        json:
          schemaRegistry: {name: registry}
`, policy)
		require.NotEmpty(t, RefsAtEventGatewayVirtualClusterConsumePolicySchemaValidationConfigJSONSchemaRegistry(policy))
		_, err := resolveEventGatewayVirtualClusterConsumePolicySchemaValidationConfigJSONSchemaRegistry(t.Context(), newClient(t), policy)
		require.ErrorAs(t, err, new(ReferenceNotKonnectManagedError))
		require.ErrorContains(t, err, "EventGatewaySchemaRegistry default/registry is not attached to a Konnect gateway")
	})

	t.Run("produce policy referencing a static key", func(t *testing.T) {
		policy := &EventGatewayVirtualClusterProducePolicy{}
		unmarshal(t, `
metadata: {name: produce, namespace: default}
spec:
  apiSpec:
    type: encrypt
    encrypt:
      config:
        encryptionKey:
          type: static
          static:
            key: {namespacedRef: {name: key}}
`, policy)
		require.NotEmpty(t, RefsAtEventGatewayVirtualClusterProducePolicyEncryptConfigEncryptionKeyStaticKey(policy))
		_, err := resolveEventGatewayVirtualClusterProducePolicyEncryptConfigEncryptionKeyStaticKey(t.Context(), newClient(t), policy)
		require.ErrorAs(t, err, new(ReferenceNotKonnectManagedError))
		require.ErrorContains(t, err, "EventGatewayStaticKey default/key is not attached to a Konnect gateway")
	})

	t.Run("listener policy referencing a TLS trust bundle", func(t *testing.T) {
		policy := &EventGatewayListenerPolicy{}
		unmarshal(t, `
metadata: {name: listener-policy, namespace: default}
spec:
  apiSpec:
    type: tlsServer
    tlsServer:
      config:
        clientAuthentication:
          tlsTrustBundles:
            - namespacedRef: {name: bundle}
`, policy)
		require.NotEmpty(t, RefsAtEventGatewayListenerPolicyTLSServerConfigClientAuthenticationTLSTrustBundles(policy))
		_, err := resolveEventGatewayListenerPolicyTLSServerConfigClientAuthenticationTLSTrustBundles(t.Context(), newClient(t), policy)
		require.ErrorAs(t, err, new(ReferenceNotKonnectManagedError))
		require.ErrorContains(t, err, "EventGatewayTLSTrustBundle default/bundle is not attached to a Konnect gateway")
	})
}
