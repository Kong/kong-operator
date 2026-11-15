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
	"net/http"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync"
)

func TestStaticKeyID(t *testing.T) {
	key := client.ObjectKey{Namespace: "keys", Name: "key"}
	id := staticKeyID(key)
	assert.Equal(t, uuid.Version(4), id.Version(), "gw-admin requires version 4 UUIDs")
	assert.Equal(t, uuid.RFC4122, id.Variant())
	assert.Equal(t, "694607a4-0432-4931-9eb5-71563b7ffa35", id.String(),
		"changing how the id is derived changes the id of every key: the records they encrypted could no longer be decrypted")
	assert.Equal(t, id, staticKeyID(key), "the same key, e.g. of a recreated object, keeps the id")
	assert.NotEqual(t, id, staticKeyID(client.ObjectKey{Namespace: "keys", Name: "other"}))
	assert.NotEqual(t, id, staticKeyID(client.ObjectKey{Namespace: "other", Name: "key"}))
	assert.NotEqual(t, staticKeyID(client.ObjectKey{Namespace: "a", Name: "b/c"}), staticKeyID(client.ObjectKey{Namespace: "a/b", Name: "c"}),
		"namespace and name can't be mixed up")
	assert.NotEqual(t, id, hashedEntityID(configurationv1alpha1.GroupVersion.WithKind("EventGatewaySchemaRegistry").GroupKind(), key),
		"objects of other kinds get other ids")
	assert.NotEqual(t, id, hashedEntityID(schema.GroupKind{Group: "other.konghq.com", Kind: "EventGatewayStaticKey"}, key),
		"objects of other groups get other ids")
}

// staticKeyObjects returns a gateway with a virtual cluster whose produce
// policy encrypts with the static key key, of the Secret key-secret.
func staticKeyObjects(t *testing.T) (*eventgatewayv1alpha1.OnPremEventGateway, *configurationv1alpha1.EventGatewayStaticKey, *corev1.Secret, []client.Object) {
	t.Helper()
	created := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	gateway := newTestGateway("gateway")
	backend := fromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, created, `
metadata: {name: backend}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: backend
    authentication: {type: anonymous, anonymous: {}}
    bootstrapServers: ["kafka:9092"]
    insecureAllowAnonymousVirtualClusterAuth: Enabled
    tls: {enabled: Disabled}
`)
	vc := fromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, created, `
metadata: {name: vc}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: backend}}
  apiSpec:
    name: vc
    aclMode: passthrough
    authentication: [{type: anonymous}]
    dnsLabel: vc
`)
	key := fromYAML[configurationv1alpha1.EventGatewayStaticKey](t, created, `
metadata: {name: key}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    name: key
    value: {type: secretRef, secretRef: {name: key-secret, key: key}}
`)
	secret := &corev1.Secret{Name: "key-secret", Namespace: ns, Data: map[string][]byte{"key": []byte("c2VjcmV0LWtleS0xMjM0NTY3ODkwMTIzNDU2")}}
	encrypt := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, created, `
metadata: {name: encrypt}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    type: encrypt
    encrypt:
      name: encrypt
      config:
        failureMode: error
        partOfRecord: [value]
        encryptionKey: {type: static, static: {key: {name: key}}}
`)
	return gateway, key, secret, []client.Object{backend, vc, encrypt}
}

// encryptionKeyID returns the id of the static key the produce policy of the
// configuration encrypts with.
func encryptionKeyID(t *testing.T, cfg configuration) string {
	t.Helper()
	require.Len(t, cfg.entities.VirtualClusters, 1)
	require.Len(t, cfg.entities.VirtualClusters[0].ProducePolicies, 1)
	data, err := json.Marshal(cfg.entities.VirtualClusters[0].ProducePolicies[0])
	require.NoError(t, err)
	var policy struct {
		Config struct {
			EncryptionKey struct {
				Key struct {
					ID string `json:"id"`
				} `json:"key"`
			} `json:"encryption_key"`
		} `json:"config"`
	}
	require.NoError(t, json.Unmarshal(data, &policy))
	return policy.Config.EncryptionKey.Key.ID
}

// TestBuildConfigurationStaticKeyRecreated checks the id of a static key, and
// of the references to it, survives the recreation of its object with the
// same value, and that the policies using it still hold it.
func TestBuildConfigurationStaticKeyRecreated(t *testing.T) {
	gateway, key, secret, objects := staticKeyObjects(t)
	cl := newFakeClient(t, append(objects, key, secret)...)
	build := func(previous map[types.UID]any) configuration {
		t.Helper()
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", previous)
		require.NoError(t, err)
		require.Empty(t, cfg.excluded)
		require.Len(t, cfg.entities.StaticKeys, 1)
		return cfg
	}
	held := func(cfg configuration, k *configurationv1alpha1.EventGatewayStaticKey) []reportedEntity {
		return cfg.entitiesInUse[reportedEntity{typ: reflect.TypeOf(k), key: client.ObjectKeyFromObject(k)}]
	}
	encrypt := reportedEntity{typ: producePolicyType, key: client.ObjectKey{Namespace: ns, Name: "encrypt"}}

	first := build(nil)
	id := first.entities.StaticKeys[0].Id
	assert.NotEqual(t, string(key.UID), id.String())
	assert.Equal(t, id.String(), encryptionKeyID(t, first), "the policy references the key by its id")
	assert.Equal(t, []reportedEntity{encrypt}, held(first, key))

	// The object is recreated, with the same Secret: another UID.
	require.NoError(t, cl.Delete(t.Context(), key))
	recreated := key.DeepCopy()
	recreated.UID, recreated.ResourceVersion = types.UID(uuid.NewString()), ""
	require.NoError(t, cl.Create(t.Context(), recreated))
	second := build(first.built)
	assert.Equal(t, id, second.entities.StaticKeys[0].Id, "a recreated key keeps its id: the records it encrypted are still decrypted")
	assert.Equal(t, id.String(), encryptionKeyID(t, second))
	assert.Equal(t, []reportedEntity{encrypt}, held(second, recreated), "the policy still holds the key")
	assert.Equal(t, []reportedEntity{encrypt}, entitiesInUseBySent(second.entities, second)[reportedEntity{typ: staticKeyType, key: client.ObjectKeyFromObject(recreated)}],
		"the configuration sent holds the key by its id too")

	// Its Secret gone, it is sent as it was last, under the same id.
	require.NoError(t, cl.Delete(t.Context(), secret))
	third, err := buildConfiguration(t.Context(), cl, gateway, "", second.built)
	require.NoError(t, err)
	require.Len(t, third.entities.StaticKeys, 1)
	assert.Equal(t, id, third.entities.StaticKeys[0].Id, "a key kept as it was keeps its id")
	assert.Equal(t, id.String(), encryptionKeyID(t, third))
	assert.Equal(t, []reportedEntity{encrypt}, held(third, recreated))

	// Its value changed: the id stays, as it tells nothing of the value.
	changed := secret.DeepCopy()
	changed.ResourceVersion = ""
	changed.Data = map[string][]byte{"key": []byte("b3RoZXItc2VjcmV0LWtleS0xMjM0NTY3ODkw")}
	require.NoError(t, cl.Create(t.Context(), changed))
	fourth := build(third.built)
	assert.Equal(t, id, fourth.entities.StaticKeys[0].Id)
	assert.Equal(t, id.String(), encryptionKeyID(t, fourth))
}

// TestBuildConfigurationStaticKeyValueChanged checks a static key whose value
// changed since it was pushed stops the push, so that gw-admin keeps the value
// the records were encrypted with, until the value is put back or the key is
// created again.
func TestBuildConfigurationStaticKeyValueChanged(t *testing.T) {
	gateway, key, secret, objects := staticKeyObjects(t)
	cl := newFakeClient(t, append(objects, key, secret)...)
	build := func() (configuration, *configurationv1alpha1.EventGatewayStaticKey) {
		t.Helper()
		cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
		require.NoError(t, err)
		var k configurationv1alpha1.EventGatewayStaticKey
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "key"}, &k))
		return cfg, &k
	}
	setValue := func(value string) {
		t.Helper()
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(secret), secret))
		secret.Data = map[string][]byte{"key": []byte(value)}
		require.NoError(t, cl.Update(t.Context(), secret))
	}
	excluded := func(cfg configuration) map[string]excludedEntity {
		out := map[string]excludedEntity{}
		for _, e := range cfg.excluded {
			out[reflect.TypeOf(e.obj).Elem().Name()+" "+e.obj.GetName()] = e
		}
		return out
	}
	const pushedValue = "c2VjcmV0LWtleS0xMjM0NTY3ODkwMTIzNDU2"

	cfg, k := build()
	require.Empty(t, cfg.excluded)
	inputs := cfg.keyInputs[k.UID]
	require.NotEmpty(t, inputs.hash)
	assert.False(t, inputs.changed)

	t.Run("not recorded before the push", func(t *testing.T) {
		hash, cond := keyInputsStatus(k, k.GetCreateInputsHash(), inputs, false)
		assert.Empty(t, hash)
		assert.Nil(t, cond)
	})
	t.Run("recorded once pushed", func(t *testing.T) {
		hash, cond := keyInputsStatus(k, k.GetCreateInputsHash(), inputs, true)
		assert.Equal(t, inputs.hash, hash)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, konnectv1alpha1.CreateInputsReasonApplied, cond.Reason)
	})

	k.SetCreateInputsHash(inputs.hash)
	require.NoError(t, cl.Status().Update(t.Context(), k))

	t.Run("unchanged, it is sent", func(t *testing.T) {
		cfg, k := build()
		require.Empty(t, cfg.excluded)
		hash, cond := keyInputsStatus(k, k.GetCreateInputsHash(), cfg.keyInputs[k.UID], true)
		assert.Equal(t, inputs.hash, hash)
		require.NotNil(t, cond)
		assert.Equal(t, konnectv1alpha1.CreateInputsReasonUnchanged, cond.Reason)
	})

	t.Run("changed, it stops the push", func(t *testing.T) {
		setValue("b3RoZXItc2VjcmV0LWtleS0xMjM0NTY3ODkw")
		defer setValue(pushedValue)
		cfg, k := build()
		out := excluded(cfg)
		require.Contains(t, out, "EventGatewayStaticKey key")
		assert.True(t, out["EventGatewayStaticKey key"].blocks, "left out, gw-admin would drop the key")
		assert.Equal(t, "its value "+keyValueChanged, out["EventGatewayStaticKey key"].message)
		assert.Equal(t, eventgatewayv1alpha1.OnPremEventGatewayInvalidEntitiesReason, failureReason(cfg.blocked()))
		hash, cond := keyInputsStatus(k, k.GetCreateInputsHash(), cfg.keyInputs[k.UID], false)
		assert.Equal(t, inputs.hash, hash, "the fingerprint of the value pushed is kept")
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, konnectv1alpha1.CreateInputsReasonValueChanged, cond.Reason)
	})

	t.Run("put back, it is sent again", func(t *testing.T) {
		cfg, _ := build()
		assert.Empty(t, cfg.excluded)
	})

	t.Run("its Secret gone, it can't be compared", func(t *testing.T) {
		require.NoError(t, cl.Delete(t.Context(), secret))
		defer func() {
			restored := secret.DeepCopy()
			restored.ResourceVersion = ""
			require.NoError(t, cl.Create(t.Context(), restored))
		}()
		cfg, k := build()
		hash, cond := keyInputsStatus(k, k.GetCreateInputsHash(), cfg.keyInputs[k.UID], false)
		assert.Equal(t, inputs.hash, hash)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionUnknown, cond.Status)
		assert.Equal(t, konnectv1alpha1.CreateInputsReasonInputsUnavailable, cond.Reason)
	})

	t.Run("created again with another value, it is sent", func(t *testing.T) {
		setValue("b3RoZXItc2VjcmV0LWtleS0xMjM0NTY3ODkw")
		_, k := build()
		require.NoError(t, cl.Delete(t.Context(), k))
		recreated := key.DeepCopy()
		recreated.UID, recreated.ResourceVersion = types.UID(uuid.NewString()), ""
		require.NoError(t, cl.Create(t.Context(), recreated))
		cfg, _ := build()
		assert.Empty(t, cfg.excluded, "a new object starts with no fingerprint")
	})
}

// TestBuildConfigurationStaticKeyValueChangedBeingDeleted checks a static key
// whose value changed, held while being deleted, is sent as it is: stopping
// the push, it would keep the configuration sent, which holds it, and could
// never be deleted.
func TestBuildConfigurationStaticKeyValueChangedBeingDeleted(t *testing.T) {
	gateway, key, secret, objects := staticKeyObjects(t)
	cfg, err := buildConfiguration(t.Context(), newFakeClient(t, append(objects, key, secret)...), gateway, "", nil)
	require.NoError(t, err)
	pushed := cfg.keyInputs[key.UID].hash
	require.NotEmpty(t, pushed)

	deleted := key.DeepCopy()
	deleted.SetCreateInputsHash(pushed)
	deleted.Finalizers = []string{FinalizerInUse}
	deleted.DeletionTimestamp = &metav1.Time{Time: time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)}
	changed := secret.DeepCopy()
	changed.Data = map[string][]byte{"key": []byte("b3RoZXItc2VjcmV0LWtleS0xMjM0NTY3ODkw")}
	excluded := func(cfg configuration) map[string]excludedEntity {
		out := map[string]excludedEntity{}
		for _, e := range cfg.excluded {
			out[reflect.TypeOf(e.obj).Elem().Name()+" "+e.obj.GetName()] = e
		}
		return out
	}

	for name, objs := range map[string][]client.Object{
		// The encrypt policy deleted: only the backend and virtual clusters.
		"its policies gone":       {objects[0], objects[1], deleted, changed},
		"a policy still using it": append(slices.Clone(objects), deleted, changed),
	} {
		t.Run(name+", the push goes on", func(t *testing.T) {
			cfg, err := buildConfiguration(t.Context(), newFakeClient(t, objs...), gateway, "", nil)
			require.NoError(t, err)
			assert.Empty(t, excluded(cfg))
			require.NoError(t, cfg.blocked())
			require.Len(t, cfg.entities.StaticKeys, 1)
			hash, cond := keyInputsStatus(deleted, deleted.GetCreateInputsHash(), cfg.keyInputs[deleted.UID], true)
			assert.Equal(t, pushed, hash, "the fingerprint stays until the key is gone")
			require.NotNil(t, cond)
			assert.Equal(t, konnectv1alpha1.CreateInputsReasonValueChanged, cond.Reason)
		})
	}
}

// TestBuildConfigurationStaticKeyHashNotCachedYet checks a static key read
// from a cache that doesn't show the fingerprint just recorded yet is checked
// against the one recorded: its value changed meanwhile, it stops the push,
// and the fingerprint of the value pushed is kept.
func TestBuildConfigurationStaticKeyHashNotCachedYet(t *testing.T) {
	gateway, key, secret, objects := staticKeyObjects(t)
	cfg, err := buildConfiguration(t.Context(), newFakeClient(t, append(objects, key, secret)...), gateway, "", nil)
	require.NoError(t, err)
	pushed := cfg.keyInputs[key.UID].hash
	require.NotEmpty(t, pushed)

	// The cache shows the key without its fingerprint, and the new value.
	changed := secret.DeepCopy()
	changed.Data = map[string][]byte{"key": []byte("b3RoZXItc2VjcmV0LWtleS0xMjM0NTY3ODkw")}
	var recorded sync.Map
	recorded.Store(client.ObjectKeyFromObject(key), recordedKeyHash{uid: key.UID, hash: pushed})
	cfg, err = buildConfigurationWith(t.Context(), newFakeClient(t, append(objects, key, changed)...), gateway, "", nil, nil, &recorded)
	require.NoError(t, err)
	require.Error(t, cfg.blocked(), "the value changed since it was pushed")
	hash, cond := keyInputsStatus(key, cfg.recordedHash(key), cfg.keyInputs[key.UID], false)
	assert.Equal(t, pushed, hash, "the fingerprint of the value pushed is kept")
	require.NotNil(t, cond)
	assert.Equal(t, konnectv1alpha1.CreateInputsReasonValueChanged, cond.Reason)
}

// TestReconcilerRecordedInputsHash checks the controller uses the fingerprint
// it recorded while the cache doesn't show it, for that key only, and forgets
// it once the cache shows one, or once the key is deleted: it holds at most
// one by static key, and none for a static key gone.
func TestReconcilerRecordedInputsHash(t *testing.T) {
	gateway, key, _, _ := staticKeyObjects(t)
	cl := newFakeClient(t, gateway, key)
	r := &Reconciler{Client: cl}
	assert.Empty(t, r.recordedInputsHash(key))
	remembered := func() bool {
		_, ok := r.inputsHashes.Load(client.ObjectKeyFromObject(key))
		return ok
	}

	r.inputsHashes.Store(client.ObjectKeyFromObject(key), recordedKeyHash{uid: key.UID, hash: "sha256:recorded"})
	assert.Equal(t, "sha256:recorded", r.recordedInputsHash(key), "the cache doesn't show it yet")

	recreated := key.DeepCopy()
	recreated.UID = types.UID(uuid.NewString())
	assert.Empty(t, r.recordedInputsHash(recreated), "the key created again isn't the one it was recorded for")
	assert.False(t, remembered(), "forgotten then")
	r.inputsHashes.Store(client.ObjectKeyFromObject(key), recordedKeyHash{uid: key.UID, hash: "sha256:recorded"})

	sameName := &configurationv1alpha1.EventGatewayTLSTrustBundle{Namespace: key.Namespace, Name: key.Name, UID: "other"}
	assert.Empty(t, r.recordedInputsHash(sameName), "another kind of entity has none")
	assert.True(t, remembered(), "one with the name of the key leaves its fingerprint")

	shown := key.DeepCopy()
	shown.SetCreateInputsHash("sha256:recorded")
	assert.Equal(t, "sha256:recorded", r.recordedInputsHash(shown))
	assert.False(t, remembered(), "forgotten once the cache shows it")

	r.inputsHashes.Store(client.ObjectKeyFromObject(key), recordedKeyHash{uid: key.UID, hash: "sha256:recorded"})
	require.NoError(t, cl.Delete(t.Context(), key))
	require.NoError(t, r.reportUnreachable(t.Context(), client.ObjectKeyFromObject(key), &configurationv1alpha1.EventGatewayStaticKey{}))
	assert.False(t, remembered(), "forgotten once the key is deleted")

	other := &configurationv1alpha1.EventGatewayTLSTrustBundle{}
	assert.Empty(t, r.recordedInputsHash(other), "only static keys have one")
}

// TestStatusAppliesKeepRecordedInputsHash checks every status write of a static
// key read from a cache that doesn't show its fingerprint yet applies the one
// recorded: the field manager owns it once applied, so an apply leaving it out
// would delete it, and the CRD would let apiSpec change again.
func TestStatusAppliesKeepRecordedInputsHash(t *testing.T) {
	const recorded = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	gateway, key, _, _ := staticKeyObjects(t)
	var applied []string
	cl := newFakeClientWithInterceptor(t, interceptor.Funcs{
		SubResourceApply: func(ctx context.Context, cl client.Client, sub string, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
			data, err := json.Marshal(obj)
			require.NoError(t, err)
			var u struct {
				Status struct {
					CreateInputsHash string `json:"createInputsHash"`
				} `json:"status"`
			}
			require.NoError(t, json.Unmarshal(data, &u))
			applied = append(applied, u.Status.CreateInputsHash)
			return cl.SubResource(sub).Apply(ctx, obj, opts...)
		},
	}, gateway, key)
	r := &Reconciler{Client: cl, TypeConverter: testTypeConverter}
	r.inputsHashes.Store(client.ObjectKeyFromObject(key), recordedKeyHash{uid: key.UID, hash: recorded})
	// stale is the key as a cache not showing the fingerprint reads it.
	stale := func() *configurationv1alpha1.EventGatewayStaticKey {
		k := key.DeepCopy()
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(k), k))
		k.Status.CreateInputsHash = ""
		return k
	}
	cond := func(k *configurationv1alpha1.EventGatewayStaticKey, message string) metav1.Condition {
		return entityCondition(k, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType, metav1.ConditionFalse,
			eventgatewayv1alpha1.OnPremEntityGatewayNotProgrammedReason, message)
	}

	k := stale()
	require.NoError(t, r.setEntityStatus(t.Context(), entityStatus{obj: k}, cond(k, "left out")))
	k = stale()
	require.NoError(t, r.setEntityConditions(t.Context(), k, cond(k, "gateway gone")))
	k = stale()
	require.NoError(t, r.setUnownedEntityConditions(t.Context(), k, nil, cond(k, "detached")))

	require.Len(t, applied, 3)
	for i, hash := range applied {
		assert.Equal(t, recorded, hash, "apply %d", i)
	}
	var got configurationv1alpha1.EventGatewayStaticKey
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(key), &got))
	assert.Equal(t, recorded, got.Status.CreateInputsHash)
}

// TestStaticKeyFingerprintIsOfItsValue checks the fingerprint of a static key
// is of its value only: unlike in Konnect, its name, description and labels
// are updated in place, and its value may move from a Secret inline.
func TestStaticKeyFingerprintIsOfItsValue(t *testing.T) {
	gateway, key, secret, objects := staticKeyObjects(t)
	fingerprint := func(k *configurationv1alpha1.EventGatewayStaticKey) string {
		t.Helper()
		cfg, err := buildConfiguration(t.Context(), newFakeClient(t, append(slices.Clone(objects), k, secret)...), gateway, "", nil)
		require.NoError(t, err)
		require.Empty(t, cfg.excluded)
		return cfg.keyInputs[k.UID].hash
	}
	pushed := fingerprint(key)
	require.NotEmpty(t, pushed)

	edited := key.DeepCopy()
	edited.Spec.APISpec.Description = "rotated yearly"
	edited.Spec.APISpec.Labels = configurationv1alpha1.Labels{"team": "a"}
	assert.Equal(t, pushed, fingerprint(edited), "its description and labels aren't part of it")

	inline := key.DeepCopy()
	inline.Spec.APISpec.Value = configurationv1alpha1.SensitiveDataSource{
		Type: configurationv1alpha1.SensitiveDataSourceTypeInline, Value: new(string(secret.Data["key"])),
	}
	assert.Equal(t, pushed, fingerprint(inline), "the same value inline")

	other := inline.DeepCopy()
	other.Spec.APISpec.Value.Value = new("b3RoZXItc2VjcmV0LWtleS0xMjM0NTY3ODkw")
	assert.NotEqual(t, pushed, fingerprint(other))
}

// TestPushResultReachedAPod checks the fingerprint of a static key is
// recorded once its value may have reached a gw-admin pod, even when the push
// to it, or to another one, failed, so that the gateway isn't Programmed.
func TestPushResultReachedAPod(t *testing.T) {
	ok, failing := gwadminsync.Pod{Name: "ok", UID: "ok"}, gwadminsync.Pod{Name: "failing", UID: "failing"}
	target := &gwadminsync.Target{Pods: []gwadminsync.Pod{ok, failing}}
	outcome := func(sent ...gwadminsync.Pod) *certificateOutcome {
		o := newCertificateOutcome()
		o.fail(failing)
		for _, pod := range sent {
			o.sentEntities(pod)
		}
		return o
	}
	var cfg configuration
	assert.True(t, pushResult{target: target, certificates: outcome(ok)}.reachedAPod(cfg),
		"one pod holds it, though the push to another failed")
	assert.True(t, pushResult{target: target, certificates: outcome(failing)}.reachedAPod(cfg),
		"the push to the pod failed after it may have got the entities")
	assert.False(t, pushResult{target: target, certificates: outcome()}.reachedAPod(cfg),
		"no pod got the entities")
	assert.False(t, pushResult{target: target}.reachedAPod(cfg), "nothing pushed, e.g. waiting for the backoff")

	blocked := configuration{excluded: []excludedEntity{{obj: &configurationv1alpha1.EventGatewayStaticKey{}, blocks: true}}}
	assert.True(t, pushResult{target: target, certificates: outcome(ok)}.reachedAPod(blocked),
		"a configuration stopped from being pushed isn't, but the pods get the last one that could be sent")
}

// TestWithKeyInputsBlocked checks a static key of the last configuration that
// could be sent, pushed in place of a blocked one, records the fingerprint of
// the value it has there, not the one it has now: that is the one the pods
// got.
func TestWithKeyInputsBlocked(t *testing.T) {
	key := &configurationv1alpha1.EventGatewayStaticKey{Namespace: "default", Name: "key", UID: "key-uid"}
	held, err := keyValueHash("aGVsZC1zZWNyZXQta2V5LTEyMzQ1Njc4OTA=")
	require.NoError(t, err)
	now, err := keyValueHash("bm93LXNlY3JldC1rZXktMTIzNDU2Nzg5MA==")
	require.NoError(t, err)
	blocked := configuration{
		excluded:      []excludedEntity{{obj: &configurationv1alpha1.EventGatewayTLSTrustBundle{}, blocks: true}},
		keyInputs:     map[types.UID]keyInputs{key.UID: {hash: now}},
		lastKeyHashes: map[uuid.UUID]string{staticKeyID(client.ObjectKeyFromObject(key)): held},
	}

	for _, included := range []bool{true, false} {
		e := withKeyInputs(blocked, entityStatus{obj: key}, true, included)
		assert.Equal(t, held, e.inputsHash, "the value the pods got, the key in the configuration or not: %v", included)
		require.NotNil(t, e.inputsApplied)
		assert.Equal(t, konnectv1alpha1.CreateInputsReasonValueChanged, e.inputsApplied.Reason, "it has another value now")
	}

	assert.Empty(t, withKeyInputs(blocked, entityStatus{obj: key}, false, true).inputsHash, "no pod got it")
	withoutKey := blocked
	withoutKey.lastKeyHashes = nil
	assert.Empty(t, withKeyInputs(withoutKey, entityStatus{obj: key}, true, true).inputsHash,
		"the pods got the last configuration, without the key: its value now wasn't pushed")
}

// TestBlockedPushRecordsKeyValuePushed checks the fingerprint of a static key
// is recorded once a blocked configuration pushes the pods the last one that
// could be sent, holding the key: its value there may then be served, so that
// a rotation afterwards doesn't replace it under the same id.
func TestBlockedPushRecordsKeyValuePushed(t *testing.T) {
	_, key, secret, objects := staticKeyObjects(t)
	f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
		return append(append(f.gwAdmin.Objects(), f.gateway, key, secret), objects...)
	})
	pushed, err := keyValueHash(string(secret.Data["key"]))
	require.NoError(t, err)

	// Revision 1 reaches no pod: gw-admin rejects its entities.
	f.pod().respond = failing(callEntities, http.StatusBadRequest)
	_, _ = f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
	require.Equal(t, int64(1), read(t, f.cl, f.gateway).Status.ConfigRevision)
	require.Empty(t, read(t, f.cl, key).GetCreateInputsHash(), "no pod got its value")

	// A policy that can't be sent blocks the configuration: the pods get
	// revision 1, with the value of the key.
	f.pod().respond = nil
	f.r.backoff.Forget(f.gateway.UID) // Once its backoff is over.
	require.NoError(t, f.cl.Create(t.Context(), newSchemaRegistryFixture(t).validateWith("validate", `{name: missing}`, 3)))
	_, err = f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
	require.NoError(t, err)
	require.NotEmpty(t, f.pod().gateways[f.gatewayID].StaticKeys, "the pods got the key")
	assert.Equal(t, pushed, read(t, f.cl, key).GetCreateInputsHash(), "the value they got is recorded")
}

// TestEntitiesInUseBySentStaticKeyLeftOut checks a static key the last
// configuration sent uses stays held when it is left out of the current one,
// e.g. once the KongReferenceGrant letting it in is gone: the entities sent
// reference it by its id, which its object gives whether it is in the
// configuration or not.
func TestEntitiesInUseBySentStaticKeyLeftOut(t *testing.T) {
	for name, uid := range map[string]types.UID{
		"left out":                        types.UID(uuid.NewString()),
		"with a UID that isn't a v4 UUID": "11111111-1111-1111-1111-111111111111",
	} {
		t.Run(name, func(t *testing.T) {
			key := &configurationv1alpha1.EventGatewayStaticKey{}
			key.SetNamespace(ns)
			key.SetName("key")
			key.SetUID(uid)
			encrypt := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{}
			encrypt.SetNamespace(ns)
			encrypt.SetName("encrypt")
			encrypt.SetUID(types.UID(uuid.NewString()))
			cfg := configuration{
				namespace: ns,
				included:  []entityStatus{{obj: encrypt}},
				excluded:  []excludedEntity{{obj: key}},
			}

			var sent gwadminapi.GatewayEntitySet
			require.NoError(t, json.Unmarshal([]byte(`{"virtual_clusters": [{"id": "`+uuid.NewString()+`", "name": "vc",
				"produce_policies": [{"id": "`+string(encrypt.UID)+`", "name": "encrypt", "type": "encrypt",
				  "config": {"failure_mode": "error", "part_of_record": ["value"],
				    "encryption_key": {"type": "static", "key": {"id": "`+staticKeyID(client.ObjectKeyFromObject(key)).String()+`"}}}}]}]}`), &sent))

			assert.Equal(t, []reportedEntity{{typ: producePolicyType, key: client.ObjectKeyFromObject(encrypt)}},
				entitiesInUseBySent(sent, cfg)[reportedEntity{typ: staticKeyType, key: client.ObjectKeyFromObject(key)}])
		})
	}
}

// TestBuildConfigurationStaticKeyExcluded checks a policy whose static key is
// left out names the key, although the key is referenced by its id, not the
// UID of its object.
func TestBuildConfigurationStaticKeyExcluded(t *testing.T) {
	gateway, key, _, objects := staticKeyObjects(t)
	// Without its Secret, nor a configuration sent before: left out.
	cl := newFakeClient(t, append(objects, key)...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)

	excluded := map[string]string{}
	for _, e := range cfg.excluded {
		excluded[reflect.TypeOf(e.obj).Elem().Name()+" "+e.obj.GetName()] = e.message
	}
	assert.Contains(t, excluded, "EventGatewayStaticKey key")
	assert.Equal(t, "it references the EventGatewayStaticKey "+ns+"/key, which isn't in the configuration of the gateway",
		excluded["EventGatewayVirtualClusterProducePolicy encrypt"])
}

// TestBuildConfigurationStaticKeysOfTwoNamespaces checks static keys of the
// same name in two namespaces get two ids, and a policy references the one it
// names.
func TestBuildConfigurationStaticKeysOfTwoNamespaces(t *testing.T) {
	const other = "other"
	gateway, key, secret, objects := staticKeyObjects(t)
	otherKey := key.DeepCopy()
	otherKey.SetNamespace(other)
	otherKey.SetUID(types.UID(uuid.NewString()))
	otherKey.Spec.ParentRef.Namespace = new(ns)
	otherKey.Spec.APISpec.Name = "other-key"
	otherSecret := secret.DeepCopy()
	otherSecret.Namespace = other
	// The key of the other namespace belongs to the gateway, and the encrypt
	// policy may reference it.
	keyToGateway := &configurationv1alpha1.KongReferenceGrant{
		Name: "keys", Namespace: ns,
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{{Group: "configuration.konghq.com", Kind: "EventGatewayStaticKey", Namespace: other}},
			To:   []configurationv1alpha1.ReferenceGrantTo{{Group: "eventgateway.konghq.com", Kind: "OnPremEventGateway"}},
		},
	}
	policyToKey := &configurationv1alpha1.KongReferenceGrant{
		Name: "policies", Namespace: other,
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{{Group: "configuration.konghq.com", Kind: "EventGatewayVirtualClusterProducePolicy", Namespace: ns}},
			To:   []configurationv1alpha1.ReferenceGrantTo{{Group: "configuration.konghq.com", Kind: "EventGatewayStaticKey"}},
		},
	}
	// The encrypt policy encrypts with the key of the other namespace.
	encrypt := fromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC), `
metadata: {name: encrypt}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    type: encrypt
    encrypt:
      name: encrypt
      config:
        failureMode: error
        partOfRecord: [value]
        encryptionKey: {type: static, static: {key: {name: key, namespace: `+other+`}}}
`)
	objects = slices.DeleteFunc(objects, func(o client.Object) bool { return o.GetName() == "encrypt" })
	cl := newFakeClient(t, append(objects, encrypt, key, secret, otherKey, otherSecret, keyToGateway, policyToKey)...)
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)
	require.Empty(t, cfg.excluded)

	ids := map[string]string{}
	for _, e := range cfg.entities.StaticKeys {
		ids[e.Name] = e.Id.String()
	}
	assert.Equal(t, map[string]string{
		"key":       staticKeyID(client.ObjectKeyFromObject(key)).String(),
		"other-key": staticKeyID(client.ObjectKeyFromObject(otherKey)).String(),
	}, ids)
	assert.NotEqual(t, ids["key"], ids["other-key"])
	assert.Equal(t, ids["other-key"], encryptionKeyID(t, cfg), "the policy references the key of the namespace it names")
}
