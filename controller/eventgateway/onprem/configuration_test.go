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

package onprem

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/yaml"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
)

// emptyConfiguration returns the configuration of a gateway without entities.
func emptyConfiguration() configuration {
	key, err := comparisonKey(noEntities())
	if err != nil {
		panic(err)
	}
	return configuration{entities: noEntities(), objects: map[string]string{}, pushedKey: key}
}

// fromYAML returns an object of type T from its manifest, with a version 4
// UID and a creation time, as the API server sets them.
func fromYAML[T any, TPtr interface {
	*T
	client.Object
}](t *testing.T, created time.Time, manifest string) TPtr {
	t.Helper()
	var obj T
	require.NoError(t, yaml.Unmarshal([]byte(manifest), &obj))
	ptr := TPtr(&obj)
	if ptr.GetNamespace() == "" {
		ptr.SetNamespace(ns)
	}
	ptr.SetUID(types.UID(uuid.NewString()))
	ptr.SetCreationTimestamp(metav1.NewTime(created))
	return ptr
}

func TestBuildConfiguration(t *testing.T) {
	gateway := &eventgatewayv1alpha1.OnPremEventGateway{Name: "gateway", Namespace: ns}
	gateway.SetUID(types.UID(uuid.NewString()))
	// Not on whole seconds: gw-admin requires them.
	created := time.Date(2026, 10, 16, 10, 0, 0, 500_000_000, time.UTC)

	backendCluster := fromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, created, `
metadata: {name: backend-cluster}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: backend-cluster
    description: Backend cluster
    authentication: {type: anonymous, anonymous: {}}
    bootstrapServers: ["kafka1:9092", "kafka2:9092"]
    insecureAllowAnonymousVirtualClusterAuth: Enabled
    labels: {app: test}
    metadataUpdateIntervalSeconds: 30
    tls:
      enabled: Enabled
      tlsVersions: [tls13]
      caBundle: ca-bundle
      clientIdentity:
        certificate: {type: secretRef, secretRef: {name: backend-cluster-tls, key: tls.crt}}
        key: {type: secretRef, secretRef: {name: backend-cluster-tls, key: tls.key}}
`)
	backendClusterSecret := &corev1.Secret{
		Name: "backend-cluster-tls", Namespace: ns,
		Data: map[string][]byte{"tls.crt": []byte("client-cert"), "tls.key": []byte("client-key")},
	}
	newBackendCluster := func(name, apiName string) *configurationv1alpha1.EventGatewayBackendCluster {
		return fromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, created, `
metadata: {name: `+name+`}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: `+apiName+`
    authentication: {type: anonymous, anonymous: {}}
    bootstrapServers: ["kafka:9092"]
    tls: {enabled: Disabled}
`)
	}

	// The same name as backendCluster, created later: left out.
	duplicate := newBackendCluster("duplicate", "backend-cluster")
	duplicate.SetCreationTimestamp(metav1.NewTime(created.Add(time.Minute)))
	// In another namespace than the gateway: left out.
	otherNamespace := newBackendCluster("other-namespace", "other-namespace")
	otherNamespace.SetNamespace("other")
	otherNamespace.Spec.ParentRef.Namespace = new(ns)
	// With a UID gw-admin doesn't accept: left out.
	notV4 := newBackendCluster("not-v4", "not-v4")
	notV4.SetUID("11111111-1111-1111-1111-111111111111")
	// With a missing Secret: left out.
	missingSecret := newBackendCluster("missing-secret", "missing-secret")
	missingSecret.Spec.APISpec.TLS.ClientIdentity = backendCluster.Spec.APISpec.TLS.ClientIdentity
	missingSecret.Spec.APISpec.TLS.ClientIdentity.Certificate.SecretRef = &configurationv1alpha1.SensitiveDataSecretRef{Name: "missing", Key: "tls.crt"}
	// Being deleted: neither sent nor reported.
	deleting := newBackendCluster("deleting", "deleting")
	deleting.SetDeletionTimestamp(new(metav1.Now()))
	deleting.SetFinalizers([]string{"example.com/hold"})
	// Of a KonnectEventGateway: not part of the gateway.
	konnect := newBackendCluster("konnect", "konnect")
	konnect.Spec.ParentRef = konnectGatewayRef("konnect-gw")

	// Reading a Secret of another namespace, which no KongReferenceGrant
	// allows: left out.
	notGranted := newBackendCluster("not-granted", "not-granted")
	notGranted.Spec.APISpec.TLS.ClientIdentity = backendCluster.Spec.APISpec.TLS.ClientIdentity
	notGranted.Spec.APISpec.TLS.ClientIdentity.Certificate.SecretRef = &configurationv1alpha1.SensitiveDataSecretRef{Name: "tls", Namespace: new("secrets"), Key: "tls.crt"}

	cl := newFakeClient(t, backendCluster, backendClusterSecret, duplicate, otherNamespace, notV4, missingSecret, deleting, konnect, notGranted)

	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)

	t.Run("the backend cluster is converted", func(t *testing.T) {
		require.Len(t, cfg.entities.BackendClusters, 1)
		got, err := json.Marshal(cfg.entities.BackendClusters[0])
		require.NoError(t, err)
		assert.JSONEq(t, `{
			"id": "`+string(backendCluster.UID)+`",
			"created_at": "2026-10-16T10:00:00Z",
			"name": "backend-cluster",
			"description": "Backend cluster",
			"authentication": {"type": "anonymous"},
			"bootstrap_servers": ["kafka1:9092", "kafka2:9092"],
			"insecure_allow_anonymous_virtual_cluster_auth": true,
			"labels": {"app": "test"},
			"metadata_update_interval_seconds": 30,
			"tls": {
				"enabled": true,
				"insecure_skip_verify": false,
				"tls_versions": ["tls13"],
				"ca_bundle": "ca-bundle",
				"client_identity": {"certificate": "client-cert", "key": "client-key"}
			}
		}`, string(got))
		assert.Equal(t, map[string]string{
			"backend_clusters.0": "EventGatewayBackendCluster " + client.ObjectKeyFromObject(backendCluster).String(),
		}, cfg.objects)
	})

	t.Run("the entities are encoded once to compare them with gw-admin", func(t *testing.T) {
		key, err := comparisonKey(withGWAdminDefaults(cfg.entities))
		require.NoError(t, err)
		assert.Equal(t, key, cfg.pushedKey)
	})

	t.Run("the other kinds are empty lists, not null", func(t *testing.T) {
		got, err := json.Marshal(cfg.entities)
		require.NoError(t, err)
		var lists map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(got, &lists))
		for _, key := range []string{"listeners", "schema_registries", "static_keys", "tls_trust_bundles", "virtual_clusters"} {
			assert.JSONEq(t, `[]`, string(lists[key]), key)
		}
	})

	t.Run("entities that can't be sent are left out with a reason", func(t *testing.T) {
		type outcome struct {
			reason  kcfgconsts.ConditionReason
			message string
		}
		excluded := make(map[string]outcome, len(cfg.excluded))
		for _, e := range cfg.excluded {
			excluded[client.ObjectKeyFromObject(e.obj).String()] = outcome{e.reason, e.message}
		}
		assert.Len(t, excluded, 5, "excluded: %v", cfg.excluded)
		invalid := eventgatewayv1alpha1.OnPremEntityInvalidReason
		assert.Equal(t, invalid, excluded["default/duplicate"].reason)
		assert.Contains(t, excluded["default/duplicate"].message, `another backend cluster of the gateway is named "backend-cluster"`)
		assert.Equal(t, invalid, excluded["other/other-namespace"].reason)
		assert.Contains(t, excluded["other/other-namespace"].message, "must be in the namespace of the OnPremEventGateway, default")
		assert.Equal(t, invalid, excluded["default/not-v4"].reason)
		assert.Contains(t, excluded["default/not-v4"].message, "is not a version 4 UUID")
		assert.Equal(t, invalid, excluded["default/missing-secret"].reason)
		assert.Contains(t, excluded["default/missing-secret"].message, "default/missing")
		assert.Equal(t, eventgatewayv1alpha1.OnPremEntityRefNotPermittedReason, excluded["default/not-granted"].reason)
		assert.Equal(t, "no KongReferenceGrant allows it to read the Secret secrets/tls", excluded["default/not-granted"].message)

		require.Len(t, cfg.included, 1)
		assert.Equal(t, client.ObjectKeyFromObject(backendCluster), client.ObjectKeyFromObject(cfg.included[0].obj))
		assertSecretRefValid(t, cfg.included[0], metav1.ConditionTrue, "The Secrets it reads are resolved")
		assert.False(t, cfg.included[0].kept)
		assert.Equal(t, map[types.UID]any{backendCluster.UID: cfg.entities.BackendClusters[0]}, cfg.built)
		missing := slices.IndexFunc(cfg.excluded, func(e excludedEntity) bool { return e.obj.GetName() == "missing-secret" })
		require.GreaterOrEqual(t, missing, 0)
		assertSecretRefValid(t, cfg.excluded[missing].entityStatus, metav1.ConditionFalse, "the Secret default/missing doesn't exist")
		notGrantedAt := slices.IndexFunc(cfg.excluded, func(e excludedEntity) bool { return e.obj.GetName() == "not-granted" })
		require.GreaterOrEqual(t, notGrantedAt, 0)
		assertSecretRefValid(t, cfg.excluded[notGrantedAt].entityStatus, metav1.ConditionFalse,
			"The Secrets it reads aren't read: no KongReferenceGrant allows it to read the Secret secrets/tls")
	})

	t.Run("the same entities produce the same configuration", func(t *testing.T) {
		again, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		a, err := json.Marshal(cfg.entities)
		require.NoError(t, err)
		b, err := json.Marshal(again.entities)
		require.NoError(t, err)
		assert.JSONEq(t, string(a), string(b))
		assert.Equal(t, cfg.excluded, again.excluded)
	})

	t.Run("a Secret that doesn't exist tells how to make it visible", func(t *testing.T) {
		cfg, err := buildConfiguration(t.Context(), newFakeClient(t, missingSecret), gateway, " (hint)", nil)
		require.NoError(t, err)
		require.Len(t, cfg.excluded, 1)
		assert.True(t, strings.HasSuffix(cfg.excluded[0].message, " (hint)"), cfg.excluded[0].message)
	})

	t.Run("an entity whose Secret is gone is sent as it was last", func(t *testing.T) {
		withSecret, err := buildConfiguration(t.Context(), newFakeClient(t, backendCluster, backendClusterSecret), gateway, "", nil)
		require.NoError(t, err)
		require.Len(t, withSecret.entities.BackendClusters, 1)

		gone, err := buildConfiguration(t.Context(), newFakeClient(t, backendCluster), gateway, " (hint)", withSecret.built)
		require.NoError(t, err)
		assert.Empty(t, gone.excluded)
		assert.Equal(t, withSecret.entities.BackendClusters, gone.entities.BackendClusters)
		assert.Equal(t, withSecret.built, gone.built, "kept for the next configuration too")
		require.Len(t, gone.included, 1)
		assert.True(t, gone.included[0].kept)
		assertSecretRefValid(t, gone.included[0], metav1.ConditionFalse, "the Secret default/backend-cluster-tls doesn't exist (hint)")
	})

	t.Run("a Secret lacking the key read is reported, the entity kept as it was last", func(t *testing.T) {
		withSecret, err := buildConfiguration(t.Context(), newFakeClient(t, backendCluster, backendClusterSecret), gateway, "", nil)
		require.NoError(t, err)
		noKey := backendClusterSecret.DeepCopy()
		delete(noKey.Data, "tls.key")

		withoutLast, err := buildConfiguration(t.Context(), newFakeClient(t, backendCluster, noKey), gateway, " (hint)", nil)
		require.NoError(t, err)
		require.Len(t, withoutLast.excluded, 1)
		assert.Equal(t, `the Secret default/backend-cluster-tls has no key "tls.key"`, withoutLast.excluded[0].message)
		assertSecretRefValid(t, withoutLast.excluded[0].entityStatus, metav1.ConditionFalse, `the Secret default/backend-cluster-tls has no key "tls.key"`)

		kept, err := buildConfiguration(t.Context(), newFakeClient(t, backendCluster, noKey), gateway, "", withSecret.built)
		require.NoError(t, err)
		assert.Empty(t, kept.excluded)
		assert.Equal(t, withSecret.entities.BackendClusters, kept.entities.BackendClusters)
		require.Len(t, kept.included, 1)
		assert.True(t, kept.included[0].kept)
		assertSecretRefValid(t, kept.included[0], metav1.ConditionFalse, `the Secret default/backend-cluster-tls has no key "tls.key"`)
	})

	t.Run("a Secret of another namespace a KongReferenceGrant allows is read", func(t *testing.T) {
		grant := &configurationv1alpha1.KongReferenceGrant{
			Name: "grant", Namespace: "secrets",
			Spec: configurationv1alpha1.KongReferenceGrantSpec{
				From: []configurationv1alpha1.ReferenceGrantFrom{{Group: "configuration.konghq.com", Kind: "EventGatewayBackendCluster", Namespace: ns}},
				To:   []configurationv1alpha1.ReferenceGrantTo{{Group: "core", Kind: "Secret"}},
			},
		}
		secret := backendClusterSecret.DeepCopy()
		secret.Name, secret.Namespace = "tls", "secrets"
		// The certificate and the key of the same Secret: one check.
		bothFromSecret := notGranted.DeepCopy()
		bothFromSecret.Spec.APISpec.TLS.ClientIdentity.Key.SecretRef = &configurationv1alpha1.SensitiveDataSecretRef{Name: "tls", Namespace: new("secrets"), Key: "tls.key"}
		var grantLists int
		cl := newFakeClientWithInterceptor(t, interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*configurationv1alpha1.KongReferenceGrantList); ok {
					grantLists++
				}
				return cl.List(ctx, list, opts...)
			},
		}, bothFromSecret, grant, secret)
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		assert.Equal(t, 1, grantLists)
		assert.Empty(t, cfg.excluded)
		require.Len(t, cfg.entities.BackendClusters, 1)
		assert.Equal(t, "client-cert", cfg.entities.BackendClusters[0].Tls.ClientIdentity.Certificate)
	})

	t.Run("a Secret that can't be read is returned, not left out", func(t *testing.T) {
		cl := newFakeClientWithInterceptor(t, interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*corev1.Secret); ok {
					return apierrors.NewServiceUnavailable("cache not synced")
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}, backendCluster, backendClusterSecret)
		_, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.ErrorContains(t, err, "failed to read a reference of EventGatewayBackendCluster default/backend-cluster")
		require.True(t, apierrors.IsServiceUnavailable(err))
	})

	t.Run("a list that fails is returned", func(t *testing.T) {
		cl := newFakeClientWithInterceptor(t, interceptor.Funcs{
			List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return errors.New("list failed")
			},
		})
		_, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.ErrorContains(t, err, "failed to list the EventGatewayBackendClusters of the gateway: list failed")
	})
}

// assertSecretRefValid asserts the SecretRefValid condition of the entity has
// that status and message.
func assertSecretRefValid(t *testing.T, e entityStatus, status metav1.ConditionStatus, message string) {
	t.Helper()
	if assert.NotNil(t, e.secretRefValid) {
		assert.Equal(t, status, e.secretRefValid.Status)
		assert.Equal(t, message, e.secretRefValid.Message)
	}
}

func TestBuildConfigurationGatewayEntities(t *testing.T) {
	gateway := &eventgatewayv1alpha1.OnPremEventGateway{Name: "gateway", Namespace: ns}
	gateway.SetUID(types.UID(uuid.NewString()))
	created := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)

	bundle := fromYAML[configurationv1alpha1.EventGatewayTLSTrustBundle](t, created, `
metadata: {name: bundle}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: bundle
    config: {trustedCa: {type: inline, value: ca-pem}}
`)
	registry := fromYAML[configurationv1alpha1.EventGatewaySchemaRegistry](t, created, `
metadata: {name: registry}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    type: confluent
    confluent:
      name: registry
      config: {endpoint: "https://registry.example.com", schemaType: json}
`)
	key := fromYAML[configurationv1alpha1.EventGatewayStaticKey](t, created, `
metadata: {name: key}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: key
    value: {type: secretRef, secretRef: {name: key-secret, key: key}}
`)
	keySecret := &corev1.Secret{Name: "key-secret", Namespace: ns, Data: map[string][]byte{"key": []byte("c2VjcmV0LWtleS0xMjM0NTY3ODkwMTIzNDU2")}}
	// The same name as registry, created later: left out.
	duplicateRegistry := fromYAML[configurationv1alpha1.EventGatewaySchemaRegistry](t, created.Add(time.Minute), `
metadata: {name: duplicate-registry}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    type: confluent
    confluent:
      name: registry
      config: {endpoint: "https://other.example.com", schemaType: json}
`)
	// Its Secret doesn't exist, and nothing was built for it before: left out.
	missingSecret := fromYAML[configurationv1alpha1.EventGatewayStaticKey](t, created, `
metadata: {name: missing-secret}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: missing-secret
    value: {type: secretRef, secretRef: {name: missing, key: key}}
`)

	cl := newFakeClient(t, bundle, registry, key, keySecret, duplicateRegistry, missingSecret)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)

	t.Run("each kind is converted, under the UID of its object", func(t *testing.T) {
		require.Len(t, cfg.entities.TlsTrustBundles, 1)
		assert.Equal(t, string(bundle.UID), cfg.entities.TlsTrustBundles[0].Id.String())
		assert.Equal(t, "bundle", cfg.entities.TlsTrustBundles[0].Name)

		require.Len(t, cfg.entities.SchemaRegistries, 1)
		assert.Equal(t, "registry", schemaRegistryName(cfg.entities.SchemaRegistries[0]))
		got, err := json.Marshal(cfg.entities.SchemaRegistries[0])
		require.NoError(t, err)
		assert.Contains(t, string(got), `"id":"`+string(registry.UID)+`"`)
		assert.Contains(t, string(got), `"endpoint":"https://registry.example.com"`)

		require.Len(t, cfg.entities.StaticKeys, 1)
		assert.Equal(t, string(key.UID), cfg.entities.StaticKeys[0].Id.String())
		assert.Equal(t, "key", cfg.entities.StaticKeys[0].Name)

		assert.Equal(t, map[string]string{
			"tls_trust_bundles.0": "EventGatewayTLSTrustBundle " + client.ObjectKeyFromObject(bundle).String(),
			"schema_registries.0": "EventGatewaySchemaRegistry " + client.ObjectKeyFromObject(registry).String(),
			"static_keys.0":       "EventGatewayStaticKey " + client.ObjectKeyFromObject(key).String(),
		}, cfg.objects)
	})

	t.Run("those that can't be sent are left out with a reason", func(t *testing.T) {
		reasons := map[string]string{}
		for _, e := range cfg.excluded {
			reasons[e.obj.GetName()] = e.message
		}
		assert.Len(t, reasons, 2, "excluded: %v", cfg.excluded)
		assert.Contains(t, reasons["duplicate-registry"], `another schema registry of the gateway is named "registry"`)
		assert.Contains(t, reasons["missing-secret"], "the Secret default/missing doesn't exist")
	})

	t.Run("the static key reading a Secret reports it", func(t *testing.T) {
		statuses := map[string]entityStatus{}
		for _, e := range cfg.included {
			statuses[e.obj.GetName()] = e
		}
		assertSecretRefValid(t, statuses["key"], metav1.ConditionTrue, "The Secrets it reads are resolved")
		assert.Nil(t, statuses["bundle"].secretRefValid, "an inline value is no Secret")
	})
}

func TestDescribeField(t *testing.T) {
	cfg := configuration{objects: map[string]string{"backend_clusters.0": "EventGatewayBackendCluster ns/bc"}}
	assert.Equal(t, "EventGatewayBackendCluster ns/bc (backend_clusters.0.tls.ca_bundle)", cfg.describeField("backend_clusters.0.tls.ca_bundle"))
	assert.Equal(t, "EventGatewayBackendCluster ns/bc (backend_clusters.0)", cfg.describeField("backend_clusters.0"))
	assert.Equal(t, "backend_clusters.1.name", cfg.describeField("backend_clusters.1.name"), "no object at that path")
	assert.Equal(t, "listeners", cfg.describeField("listeners"))
}

func TestConvertEntity(t *testing.T) {
	obj := &configurationv1alpha1.EventGatewayStaticKey{}
	obj.SetCreationTimestamp(metav1.NewTime(time.Date(2026, 10, 16, 10, 0, 0, 500_000_000, time.UTC)))
	id := uuid.New()
	convert := func(from string) (gwadminapi.EventGatewayStaticKeyOnPrem, error) {
		var payload map[string]any
		require.NoError(t, json.Unmarshal([]byte(from), &payload))
		var entity gwadminapi.EventGatewayStaticKeyOnPrem
		return entity, convertEntity(payload, obj, id, &entity)
	}

	t.Run("the id and the creation time are set, a null field left out", func(t *testing.T) {
		entity, err := convert(`{"name": "key", "value": "v", "description": null, "labels": {"a": null}}`)
		require.NoError(t, err)
		assert.Equal(t, "key", entity.Name)
		assert.Equal(t, id, entity.Id)
		assert.Equal(t, time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC), entity.CreatedAt)
		assert.Nil(t, entity.Description)
	})
	t.Run("a field gw-admin doesn't know is refused, naming it", func(t *testing.T) {
		_, err := convert(`{"name": "key", "value": "v", "rotation": {"days": 30}}`)
		require.EqualError(t, err, "it sets the field rotation, which gw-admin doesn't support")
	})
	t.Run("a field gw-admin doesn't know set to its zero value is left out", func(t *testing.T) {
		_, err := convert(`{"name": "key", "value": "v", "rotation": {}, "enabled": false}`)
		require.NoError(t, err)
	})
	t.Run("a value of the wrong type", func(t *testing.T) {
		_, err := convert(`{"name": 1, "value": "v"}`)
		require.ErrorContains(t, err, "failed to convert to *gwadminapi.EventGatewayStaticKeyOnPrem")
	})
}

func TestUnknownField(t *testing.T) {
	sent := map[string]any{
		"a": map[string]any{"b": "x", "c": "y"},
		"l": []any{map[string]any{"d": "x"}, map[string]any{"d": "x", "e": json.Number("1")}},
	}
	assert.Equal(t, "a.c", unknownField(sent, map[string]any{"a": map[string]any{"b": "x"}, "l": sent["l"]}, ""))
	assert.Equal(t, "l.1.e", unknownField(sent, map[string]any{"a": sent["a"], "l": []any{map[string]any{"d": "x"}, map[string]any{"d": "x"}}}, ""))
	assert.Empty(t, unknownField(sent, sent, ""))
}

func TestWithoutNulls(t *testing.T) {
	v := map[string]any{"a": map[string]any{"x": nil}, "b": []any{nil, map[string]any{"c": 1.0, "d": nil}}, "e": nil}
	withoutNulls(v)
	assert.Equal(t,
		map[string]any{"a": map[string]any{}, "b": []any{nil, map[string]any{"c": 1.0}}}, v,
		"a null item of a list is kept: it isn't a field")
}

// TestSemanticDeepEqualPanicsOnEntities pins why sameEntities compares the
// JSON encoding of the entities: equality.Semantic.DeepEqual panics on the
// unexported fields of the times they hold.
func TestSemanticDeepEqualPanicsOnEntities(t *testing.T) {
	created := time.Now()
	held := gwadminapi.GatewayEntities{BackendClusters: []gwadminapi.EventGatewayBackendClusterOnPrem{{CreatedAt: created}}}
	pushed := gwadminapi.GatewayEntities{BackendClusters: []gwadminapi.EventGatewayBackendClusterOnPrem{{CreatedAt: created}}}
	assert.Panics(t, func() { equality.Semantic.DeepEqual(held, pushed) })
}

func TestSameEntities(t *testing.T) {
	created := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	bc := gwadminapi.EventGatewayBackendClusterOnPrem{Id: uuid.New(), Name: "bc", CreatedAt: created}
	pushed := noEntities()
	pushed.BackendClusters = []gwadminapi.EventGatewayBackendClusterOnPrem{bc}
	same := func(held, pushed gwadminapi.GatewayEntities) bool {
		t.Helper()
		key, err := comparisonKey(withGWAdminDefaults(pushed))
		require.NoError(t, err)
		ok, err := sameEntities(held, key)
		require.NoError(t, err)
		return ok
	}

	// gw-admin returns the TLS versions it defaults to when they are left
	// out.
	heldBC := bc
	heldBC.Tls.TlsVersions = &[]gwadminapi.BackendClusterTLSTlsVersions{gwadminapi.Tls12, gwadminapi.Tls13}
	held := gwadminapi.GatewayEntities{BackendClusters: []gwadminapi.EventGatewayBackendClusterOnPrem{heldBC}}
	assert.True(t, same(held, pushed), "a missing list and an empty one are the same, and so are a field left out and its default")
	assert.Nil(t, pushed.BackendClusters[0].Tls.TlsVersions, "the entities pushed are left as they are")

	held.BackendClusters[0].Tls.TlsVersions = &[]gwadminapi.BackendClusterTLSTlsVersions{gwadminapi.Tls13}
	assert.False(t, same(held, pushed), "TLS versions other than the default")

	held.BackendClusters[0] = heldBC
	held.BackendClusters[0].CreatedAt = created.Add(time.Second)
	assert.False(t, same(held, pushed))
	assert.False(t, same(noEntities(), pushed))

	other := gwadminapi.EventGatewayBackendClusterOnPrem{Id: uuid.New(), Name: "other", CreatedAt: created}
	pushed.BackendClusters = []gwadminapi.EventGatewayBackendClusterOnPrem{bc, other}
	heldOther := other
	heldOther.Tls.TlsVersions = heldBC.Tls.TlsVersions
	held.BackendClusters = []gwadminapi.EventGatewayBackendClusterOnPrem{heldOther, heldBC}
	assert.True(t, same(held, pushed), "in another order")
	assert.Equal(t, []gwadminapi.EventGatewayBackendClusterOnPrem{bc, other}, pushed.BackendClusters, "the entities pushed are left as they are")

	// gw-admin drops the empty description of a TLS trust bundle and of a
	// static key.
	bundle := gwadminapi.EventGatewayTLSTrustBundleOnPrem{Id: uuid.New(), Name: "bundle", CreatedAt: created, Description: new("")}
	key := gwadminapi.EventGatewayStaticKeyOnPrem{Id: uuid.New(), Name: "key", CreatedAt: created, Description: new("")}
	otherKey := gwadminapi.EventGatewayStaticKeyOnPrem{Id: uuid.New(), Name: "other-key", CreatedAt: created}
	pushed = noEntities()
	pushed.TlsTrustBundles = []gwadminapi.EventGatewayTLSTrustBundleOnPrem{bundle}
	pushed.StaticKeys = []gwadminapi.EventGatewayStaticKeyOnPrem{key, otherKey}
	heldBundle, heldKey := bundle, key
	heldBundle.Description, heldKey.Description = nil, nil
	held = gwadminapi.GatewayEntities{
		TlsTrustBundles: []gwadminapi.EventGatewayTLSTrustBundleOnPrem{heldBundle},
		StaticKeys:      []gwadminapi.EventGatewayStaticKeyOnPrem{otherKey, heldKey},
	}
	assert.True(t, same(held, pushed), "an empty description left out, in another order")
	assert.Equal(t, new(""), pushed.StaticKeys[0].Description, "the entities pushed are left as they are")
}
