package konnectother

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/internal/utils/configstoresync"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

type configStoreSyncEnvtest struct {
	ctx     context.Context //nolint:containedctx // one test-scoped envtest context, canceled on cleanup
	cl      client.Client   // uncached: assertions observe persisted API server state
	ns      string
	cpID    string
	storeID string
	store   *konnectv1alpha1.KonnectConfigStore
	remote  *sdkmocks.FakeConfigStoreSecrets
	cache   client.Client
}

var configStoreSyncRetryBackoff = wait.Backoff{
	Steps: 20, Duration: 100 * time.Millisecond, Factor: 1.3, Jitter: 0.1,
}

// In-flight reconciles triggered by earlier watches may still write after
// the awaited condition appears. Wait for the fake's call log to stop
// changing before snapshotting it for fail-closed assertions.
func (e *configStoreSyncEnvtest) waitSettled(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		before := len(e.remote.MutatingCalls())
		time.Sleep(500 * time.Millisecond)
		return len(e.remote.MutatingCalls()) == before
	}, consts.WaitTime, consts.TickTime, "waiting for status watch backlog to settle")
}

func newConfigStoreSyncEnvtest(t *testing.T) *configStoreSyncEnvtest {
	t.Helper()
	ctx, cancel := envtest.Context(t, t.Context())
	t.Cleanup(cancel)
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	cl := envtest.NewControllerClient(t, scheme.Get(), cfg)
	namespaced := client.NewNamespacedClient(cl, ns.Name)
	auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, namespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, namespaced, auth)
	store := deploy.KonnectConfigStoreWithID(t, ctx, namespaced, cp)
	store.Status.ControlPlaneID = &konnectv1alpha1.KonnectEntityRef{ID: cp.GetKonnectID()}
	require.NoError(t, namespaced.Status().Update(ctx, store))

	remote := sdkmocks.NewFakeConfigStoreSecrets()
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	envtest.StartReconcilers(ctx, t, mgr, logs, &konnect.KonnectConfigStoreSyncReconciler{
		Client:      mgr.GetClient(),
		SDKFactory:  sdkmocks.NewFakeConfigStoreSecretsSDKFactory(remote),
		SyncPeriod:  consts.KonnectInfiniteSyncTime,
		LoggingMode: logging.DevelopmentMode,
	})
	return &configStoreSyncEnvtest{
		ctx: ctx, cl: cl, ns: ns.Name, cpID: cp.GetKonnectID(), storeID: store.GetKonnectID(),
		store: store, remote: remote, cache: mgr.GetClient(),
	}
}

func (e *configStoreSyncEnvtest) secret(t *testing.T, name string, data map[string][]byte) *corev1.Secret {
	t.Helper()
	s := &corev1.Secret{Name: name, Namespace: e.ns, Data: data}
	require.NoError(t, e.cl.Create(e.ctx, s))
	return s
}

func (e *configStoreSyncEnvtest) sync(t *testing.T, name, secretName string, spec konnectv1alpha1.KonnectConfigStoreSyncSpec) *konnectv1alpha1.KonnectConfigStoreSync {
	t.Helper()
	spec.ConfigStoreRef = commonv1alpha1.NamespacedRef{Name: e.store.Name}
	spec.SecretRef = commonv1alpha1.NamespacedRef{Name: secretName}
	s := &konnectv1alpha1.KonnectConfigStoreSync{
		Name: name, Namespace: e.ns, Spec: spec,
	}
	require.NoError(t, e.cl.Create(e.ctx, s))
	return s
}

func combinedSyncSpec(key string) konnectv1alpha1.KonnectConfigStoreSyncSpec {
	return konnectv1alpha1.KonnectConfigStoreSyncSpec{
		Mode:     konnectv1alpha1.KonnectConfigStoreSyncModeCombined,
		Combined: &konnectv1alpha1.KonnectConfigStoreSyncCombined{StoreKey: new(key)},
	}
}

func splitSyncSpec(entries ...konnectv1alpha1.KonnectConfigStoreSyncSplitEntry) konnectv1alpha1.KonnectConfigStoreSyncSpec {
	return konnectv1alpha1.KonnectConfigStoreSyncSpec{
		Mode:  konnectv1alpha1.KonnectConfigStoreSyncModeSplit,
		Split: &konnectv1alpha1.KonnectConfigStoreSyncSplit{Entries: entries},
	}
}

func (e *configStoreSyncEnvtest) getSync(t *testing.T, s *konnectv1alpha1.KonnectConfigStoreSync) *konnectv1alpha1.KonnectConfigStoreSync {
	t.Helper()
	got := new(konnectv1alpha1.KonnectConfigStoreSync)
	require.NoError(t, e.cl.Get(e.ctx, client.ObjectKeyFromObject(s), got))
	return got
}

func (e *configStoreSyncEnvtest) waitSync(t *testing.T, s *konnectv1alpha1.KonnectConfigStoreSync, conditionType, reason string) *konnectv1alpha1.KonnectConfigStoreSync {
	t.Helper()
	var got *konnectv1alpha1.KonnectConfigStoreSync
	require.Eventually(t, func() bool {
		current := new(konnectv1alpha1.KonnectConfigStoreSync)
		if err := e.cl.Get(e.ctx, client.ObjectKeyFromObject(s), current); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(current.Status.Conditions, conditionType)
		if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != reason || cond.ObservedGeneration != current.Generation {
			return false
		}
		got = current
		return true
	}, consts.WaitTime, consts.TickTime, "waiting for %s=False/%s on %s", conditionType, reason, client.ObjectKeyFromObject(s))
	return got
}

func (e *configStoreSyncEnvtest) waitReady(t *testing.T, s *konnectv1alpha1.KonnectConfigStoreSync, expectedHash string) *konnectv1alpha1.KonnectConfigStoreSync {
	t.Helper()
	var got *konnectv1alpha1.KonnectConfigStoreSync
	require.Eventually(t, func() bool {
		current := new(konnectv1alpha1.KonnectConfigStoreSync)
		if err := e.cl.Get(e.ctx, client.ObjectKeyFromObject(s), current); err != nil {
			return false
		}
		cond := apimeta.FindStatusCondition(current.Status.Conditions, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType)
		if cond == nil || cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != current.Generation ||
			current.Status.EntriesSynced != current.Status.EntriesTotal || len(current.Status.Entries) == 0 {
			return false
		}
		if expectedHash != "" && current.Status.Entries[0].Hash != expectedHash {
			return false
		}
		got = current
		return true
	}, consts.WaitTime, consts.TickTime, "waiting for synced status on %s", client.ObjectKeyFromObject(s))
	return got
}

func (e *configStoreSyncEnvtest) updateSecret(t *testing.T, s *corev1.Secret, data map[string][]byte) {
	t.Helper()
	require.NoError(t, retry.RetryOnConflict(configStoreSyncRetryBackoff, func() error {
		got := new(corev1.Secret)
		if err := e.cl.Get(e.ctx, client.ObjectKeyFromObject(s), got); err != nil {
			return err
		}
		got.Data = data
		return e.cl.Update(e.ctx, got)
	}))
}

func (e *configStoreSyncEnvtest) updateSync(t *testing.T, s *konnectv1alpha1.KonnectConfigStoreSync, modify func(*konnectv1alpha1.KonnectConfigStoreSync)) {
	t.Helper()
	require.NoError(t, retry.RetryOnConflict(configStoreSyncRetryBackoff, func() error {
		got := new(konnectv1alpha1.KonnectConfigStoreSync)
		if err := e.cl.Get(e.ctx, client.ObjectKeyFromObject(s), got); err != nil {
			return err
		}
		modify(got)
		return e.cl.Update(e.ctx, got)
	}))
}

func (e *configStoreSyncEnvtest) value(t *testing.T, key string) string {
	t.Helper()
	value, ok := e.remote.Value(e.cpID, e.storeID, key)
	require.True(t, ok, "store entry %q should exist", key)
	return value
}

func assertNoConfigStoreWrites(t *testing.T, remote *sdkmocks.FakeConfigStoreSecrets) {
	t.Helper()
	assert.Empty(t, remote.Calls(), "safety gate must reject before even reading Konnect")
}

func assertCombinedValue(t *testing.T, value string, cert, key []byte) {
	t.Helper()
	var fields map[string]string
	require.NoError(t, json.Unmarshal([]byte(value), &fields))
	assert.Equal(t, map[string]string{"certificate": string(cert), "key": string(key)}, fields)
}

func combinedHash(t *testing.T, cert, key []byte) string {
	t.Helper()
	value, err := configstoresync.CombinedValue(cert, key)
	require.NoError(t, err)
	return configstoresync.ValueHash(value)
}

func TestKonnectConfigStoreSyncEnvtestCombined(t *testing.T) {
	t.Parallel()
	e := newConfigStoreSyncEnvtest(t)
	cert1, key1 := certificate.MustGenerateCertPEMFormat(certificate.WithKeyType(certificate.ECDSA))
	cert2, key2 := certificate.MustGenerateCertPEMFormat(certificate.WithKeyType(certificate.ECDSA))
	cert3, key3 := certificate.MustGenerateCertPEMFormat(certificate.WithKeyType(certificate.ECDSA))
	secret := e.secret(t, "tls-source", map[string][]byte{"tls.crt": cert1, "tls.key": key1})
	sync := e.sync(t, "combined", secret.Name, combinedSyncSpec("tls-entry"))

	got := e.waitReady(t, sync, combinedHash(t, cert1, key1))
	require.Len(t, got.Status.Entries, 1)
	assert.Equal(t, e.storeID, got.Status.StoreID)
	assert.Equal(t, e.cpID, got.Status.ControlPlaneID)
	assert.Equal(t, secret.ResourceVersion, got.Status.ObservedSecretResourceVersion)
	assert.Equal(t, int64(len(e.value(t, "tls-entry"))), got.Status.Entries[0].ValueBytes)
	assert.NotNil(t, got.Status.Entries[0].NotAfter)
	assert.NotNil(t, got.Status.Entries[0].ObservedUpdatedAt)
	assert.Equal(t, []konnectv1alpha1.KonnectConfigStoreSyncReference{
		{Suffix: "tls-entry/certificate", SubField: "certificate"},
		{Suffix: "tls-entry/key", SubField: "key"},
	}, got.Status.References)
	assertCombinedValue(t, e.value(t, "tls-entry"), cert1, key1)
	e.waitSettled(t)
	require.Len(t, e.remote.MutatingCalls(), 1, "status watches must not generate false drift writes")
	for _, call := range e.remote.MutatingCalls() {
		assert.Equal(t, "Update", call.Method, "PUT upsert, not POST create")
	}
	badSecret := e.secret(t, "initially-invalid", map[string][]byte{"tls.crt": cert2, "tls.key": key1})
	e.remote.ResetCalls()
	badSync := e.sync(t, "blocked-from-start", badSecret.Name, combinedSyncSpec("blocked-key"))
	e.waitSync(t, badSync, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
		konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonPairMismatch)
	assertNoConfigStoreWrites(t, e.remote)
	_, exists := e.remote.Value(e.cpID, e.storeID, "blocked-key")
	assert.False(t, exists, "an initially invalid pair must never be created")

	// An invalid rotation must not write either half of the pair or read the
	// remote entry; the previous valid value continues serving.
	e.remote.ResetCalls()
	e.updateSecret(t, secret, map[string][]byte{"tls.crt": cert2, "tls.key": key1})
	got = e.waitSync(t, sync, konnectv1alpha1.KonnectConfigStoreSyncPairValidConditionType,
		konnectv1alpha1.KonnectConfigStoreSyncPairValidReasonPairMismatch)
	assertNoConfigStoreWrites(t, e.remote)
	assertCombinedValue(t, e.value(t, "tls-entry"), cert1, key1)
	assert.Equal(t, combinedHash(t, cert1, key1), got.Status.Entries[0].Hash)

	e.remote.ResetCalls()
	e.updateSecret(t, secret, map[string][]byte{"tls.crt": cert2}) // missing key is not a bypass
	require.Eventually(t, func() bool {
		got := e.getSync(t, sync)
		condition := apimeta.FindStatusCondition(got.Status.Conditions, konnectv1alpha1.KonnectConfigStoreSyncPairValidConditionType)
		return condition != nil && strings.Contains(condition.Message, `secret data field "tls.key" is missing`)
	}, consts.WaitTime, consts.TickTime)
	assertNoConfigStoreWrites(t, e.remote)
	assertCombinedValue(t, e.value(t, "tls-entry"), cert1, key1)

	e.updateSecret(t, secret, map[string][]byte{"tls.crt": cert2, "tls.key": key2})
	e.waitReady(t, sync, combinedHash(t, cert2, key2))
	assertCombinedValue(t, e.value(t, "tls-entry"), cert2, key2)
	e.waitSettled(t)

	// Changing secretRef is permitted: the store key and reference suffixes
	// remain stable while the remote value changes to the new Secret's pair.
	next := e.secret(t, "tls-repointed", map[string][]byte{"tls.crt": cert3, "tls.key": key3})
	e.updateSync(t, sync, func(s *konnectv1alpha1.KonnectConfigStoreSync) {
		s.Spec.SecretRef.Name = next.Name
	})
	got = e.waitReady(t, sync, combinedHash(t, cert3, key3))
	assert.Equal(t, next.ResourceVersion, got.Status.ObservedSecretResourceVersion)
	assert.Equal(t, "tls-entry", got.Status.Entries[0].StoreKey)
	assertCombinedValue(t, e.value(t, "tls-entry"), cert3, key3)
}

func TestKonnectConfigStoreSyncEnvtestPreflight(t *testing.T) {
	t.Parallel()
	e := newConfigStoreSyncEnvtest(t)
	// The Config Store API measures decoded bytes, not runes. The existing
	// fake's own unit tests assert that 5120/512 bytes succeed and +1 fail.
	atCap := []byte(strings.Repeat("é", 2559) + "ab")
	require.Len(t, atCap, sdkmocks.FakeConfigStoreSecretMaxValueBytes)
	secret := e.secret(t, "values", map[string][]byte{"value": atCap})
	sync := e.sync(t, "size", secret.Name, splitSyncSpec(
		konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "value", StoreKey: new("size-entry")},
	))
	got := e.waitReady(t, sync, configstoresync.ValueHash(atCap))
	assert.Equal(t, int64(5120), got.Status.Entries[0].ValueBytes)
	assert.Equal(t, string(atCap), e.value(t, "size-entry"))
	e.waitSettled(t)

	e.remote.ResetCalls()
	e.updateSecret(t, secret, map[string][]byte{"value": append(append([]byte(nil), atCap...), 'x')})
	e.waitSync(t, sync, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
		konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonValueTooLarge)
	assertNoConfigStoreWrites(t, e.remote)
	assert.Equal(t, string(atCap), e.value(t, "size-entry"))

	// Preflight the entire Split set before any writes: a valid changed entry
	// must not be pushed if a later entry exceeds the value cap.
	e.updateSync(t, sync, func(s *konnectv1alpha1.KonnectConfigStoreSync) {
		s.Spec.Split.Entries = append(s.Spec.Split.Entries,
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "oversize", StoreKey: new("second-entry")})
	})
	e.remote.ResetCalls()
	e.updateSecret(t, secret, map[string][]byte{"value": []byte("new"), "oversize": stringsToBytes(5121)})
	e.waitSync(t, sync, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
		konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonValueTooLarge)
	assertNoConfigStoreWrites(t, e.remote)
	assert.Equal(t, string(atCap), e.value(t, "size-entry"))

	// A derived Split key can exceed the API cap even though every field in
	// the spec is individually valid. Exercise both sides of that boundary.
	longName := strings.Repeat("s", 245)
	fieldLength := sdkmocks.FakeConfigStoreSecretMaxKeyBytes -
		len(configstoresync.DerivedKey(e.ns, longName)) - 1
	require.Positive(t, fieldLength)
	require.LessOrEqual(t, fieldLength+1, 253, "Secret data field must remain CRD-valid")
	field := strings.Repeat("f", fieldLength)
	tooLongField := field + "f"
	keysSecret := e.secret(t, "key-fields", map[string][]byte{
		field: []byte("accepted"), tooLongField: []byte("rejected"),
	})
	atKeyCap := e.sync(t, longName, keysSecret.Name, splitSyncSpec(
		konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: field},
	))
	key := configstoresync.DerivedKey(e.ns, longName) + "-" + field
	require.Len(t, key, sdkmocks.FakeConfigStoreSecretMaxKeyBytes)
	e.waitReady(t, atKeyCap, configstoresync.ValueHash([]byte("accepted")))
	assert.Equal(t, "accepted", e.value(t, key))
	e.waitSettled(t)

	overName := strings.Repeat("t", len(longName))
	overKeyCap := e.sync(t, overName, keysSecret.Name, splitSyncSpec(
		konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: tooLongField},
	))
	e.remote.ResetCalls()
	e.waitSync(t, overKeyCap, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
		konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonKeyTooLong)
	assertNoConfigStoreWrites(t, e.remote)
	assert.Equal(t, "accepted", e.value(t, key))
}

func stringsToBytes(length int) []byte {
	return []byte(strings.Repeat("v", length))
}

func TestKonnectConfigStoreSyncEnvtestRecovery(t *testing.T) {
	t.Parallel()
	e := newConfigStoreSyncEnvtest(t)
	secret := e.secret(t, "source", map[string][]byte{"first": []byte("one"), "second": []byte("two")})
	var failSecond atomic.Bool
	failSecond.Store(true)
	e.remote.ErrorHook = func(method, key string) error {
		if method == "Update" && key == "second-key" && failSecond.Load() {
			return errors.New("injected second write failure")
		}
		return nil
	}
	sync := e.sync(t, "partial", secret.Name, splitSyncSpec(
		konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "first", StoreKey: new("first-key")},
		konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "second", StoreKey: new("second-key")},
	))
	got := e.waitSync(t, sync, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
		konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonPushFailed)
	assert.Equal(t, "one", e.value(t, "first-key"))
	_, exists := e.remote.Value(e.cpID, e.storeID, "second-key")
	assert.False(t, exists, "failed second write must not appear in the store")
	require.Len(t, got.Status.Entries, 1, "first write must be recorded even when the second fails")
	assert.Equal(t, configstoresync.ValueHash([]byte("one")), got.Status.Entries[0].Hash)

	failSecond.Store(false)
	e.updateSync(t, sync, func(s *konnectv1alpha1.KonnectConfigStoreSync) {
		s.Annotations = map[string]string{"test.konghq.com/retry": "true"}
	})
	got = e.waitReady(t, sync, configstoresync.ValueHash([]byte("one")))
	assert.Equal(t, int32(2), got.Status.EntriesSynced)
	assert.Equal(t, "two", e.value(t, "second-key"))
	var firstWrites int
	for _, call := range e.remote.MutatingCalls() {
		if call.Key == "first-key" {
			firstWrites++
		}
	}
	assert.Positive(t, firstWrites)
	e.waitSettled(t)
	e.remote.ResetCalls()
	e.updateSync(t, sync, func(s *konnectv1alpha1.KonnectConfigStoreSync) {
		s.Annotations["test.konghq.com/retry"] = "steady"
	})
	e.waitSettled(t)
	assert.Empty(t, e.remote.MutatingCalls(), "a settled sync must make no further writes")

	// Simulate a crash after PUT but before status persistence by removing the
	// durable entry observation through the real status subresource. The next
	// reconcile must re-derive the key and safely PUT the same value again,
	// even though POST would receive a 409 from this mock.
	e.remote.ResetCalls()
	require.NoError(t, retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := new(konnectv1alpha1.KonnectConfigStoreSync)
		if err := e.cl.Get(e.ctx, client.ObjectKeyFromObject(sync), current); err != nil {
			return err
		}
		current.Status.Entries = nil
		current.Status.References = nil
		current.Status.EntriesSynced = 0
		return e.cl.Status().Update(e.ctx, current)
	}))
	require.Eventually(t, func() bool {
		for _, call := range e.remote.MutatingCalls() {
			if call.Method == "Update" && call.Key == "first-key" {
				return true
			}
		}
		return false
	}, consts.WaitTime, consts.TickTime, "missing status must lead to a PUT upsert")
	got = e.waitReady(t, sync, configstoresync.ValueHash([]byte("one")))
	assert.Equal(t, int32(2), got.Status.EntriesSynced)
	assert.Equal(t, "one", e.value(t, "first-key"))
	assert.Equal(t, "two", e.value(t, "second-key"))
	for _, call := range e.remote.MutatingCalls() {
		assert.NotEqual(t, "Create", call.Method, "a duplicate POST would fail with 409")
	}
}

func TestKonnectConfigStoreSyncEnvtestOrphanRecreate(t *testing.T) {
	t.Parallel()
	e := newConfigStoreSyncEnvtest(t)
	cert, key := certificate.MustGenerateCertPEMFormat(certificate.WithKeyType(certificate.ECDSA))
	secret := e.secret(t, "source", map[string][]byte{"tls.crt": cert, "tls.key": key})
	sync := e.sync(t, "recreated", secret.Name, combinedSyncSpec("persistent-key"))
	e.waitReady(t, sync, combinedHash(t, cert, key))
	e.waitSettled(t)
	require.NoError(t, e.cl.Delete(e.ctx, sync))
	require.Eventually(t, func() bool {
		err := e.cl.Get(e.ctx, client.ObjectKeyFromObject(sync), new(konnectv1alpha1.KonnectConfigStoreSync))
		return apierrors.IsNotFound(err)
	}, consts.WaitTime, consts.TickTime)
	assertCombinedValue(t, e.value(t, "persistent-key"), cert, key)

	e.remote.ResetCalls()
	recreated := e.sync(t, sync.Name, secret.Name, combinedSyncSpec("persistent-key"))
	got := e.waitReady(t, recreated, combinedHash(t, cert, key))
	assert.Equal(t, int32(1), got.Status.EntriesSynced)
	assertCombinedValue(t, e.value(t, "persistent-key"), cert, key)
	require.NotEmpty(t, e.remote.MutatingCalls())
	for _, call := range e.remote.MutatingCalls() {
		assert.Equal(t, "Update", call.Method)
	}
}

func TestKonnectConfigStoreSyncEnvtestConflictOrder(t *testing.T) {
	t.Parallel()
	for _, firstName := range []string{"a-first", "z-first"} {
		t.Run(firstName, func(t *testing.T) {
			e := newConfigStoreSyncEnvtest(t)
			cert1, key1 := certificate.MustGenerateCertPEMFormat(certificate.WithKeyType(certificate.ECDSA))
			cert2, key2 := certificate.MustGenerateCertPEMFormat(certificate.WithKeyType(certificate.ECDSA))
			firstSecret := e.secret(t, "first-source", map[string][]byte{"tls.crt": cert1, "tls.key": key1})
			secondSecret := e.secret(t, "second-source", map[string][]byte{"tls.crt": cert2, "tls.key": key2})
			first := e.sync(t, firstName, firstSecret.Name, combinedSyncSpec("shared-key"))
			e.waitReady(t, first, combinedHash(t, cert1, key1))
			e.waitSettled(t)

			// Ensure the manager's *indexed cache*, not just the API server,
			// sees the first owner's persisted status before creating the
			// challenger. This makes the assertion independent of watch order.
			require.Eventually(t, func() bool {
				var listed konnectv1alpha1.KonnectConfigStoreSyncList
				if err := e.cache.List(e.ctx, &listed, client.MatchingFields{
					index.IndexFieldKonnectConfigStoreSyncOnStoreKey: e.storeID + "/shared-key",
				}); err != nil {
					return false
				}
				return len(listed.Items) == 1 && listed.Items[0].Name == first.Name
			}, consts.WaitTime, consts.TickTime)

			e.remote.ResetCalls()
			secondName := "z-second"
			if firstName == "z-first" {
				secondName = "a-second" // name order must not beat creation time
			}
			second := e.sync(t, secondName, secondSecret.Name, combinedSyncSpec("shared-key"))
			got := e.waitSync(t, second, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
				konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonKeyConflict)
			assert.Empty(t, got.Status.References)
			assert.Empty(t, e.remote.MutatingCalls(), "losing sync may not overwrite the winner")
			assertCombinedValue(t, e.value(t, "shared-key"), cert1, key1)
			assert.Equal(t, metav1.ConditionTrue,
				apimeta.FindStatusCondition(e.getSync(t, first).Status.Conditions,
					konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType).Status)
		})
	}
}

func grantConfigStoreSyncReference(t *testing.T, e *configStoreSyncEnvtest, fromNS, toNS, kind, name string) *configurationv1alpha1.KongReferenceGrant {
	t.Helper()
	group := configurationv1alpha1.Group("core")
	if kind == "KonnectConfigStore" {
		group = configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group)
	}
	return deploy.KongReferenceGrant(t, e.ctx, e.cl,
		func(obj client.Object) { obj.SetNamespace(toNS) },
		deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
			Group:     configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
			Kind:      "KonnectConfigStoreSync",
			Namespace: configurationv1alpha1.Namespace(fromNS),
		}),
		deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
			Group: group, Kind: configurationv1alpha1.Kind(kind),
			Name: new(configurationv1alpha1.ObjectName(name)),
		}),
	)
}

func TestKonnectConfigStoreSyncEnvtestReferenceGrants(t *testing.T) {
	t.Parallel()
	e := newConfigStoreSyncEnvtest(t)
	other := deploy.Namespace(t, e.ctx, e.cl)
	cert, key := certificate.MustGenerateCertPEMFormat(certificate.WithKeyType(certificate.ECDSA))
	remoteSecret := &corev1.Secret{
		Name: "remote-secret", Namespace: other.Name,
		Data: map[string][]byte{"tls.crt": cert, "tls.key": key},
	}
	require.NoError(t, e.cl.Create(e.ctx, remoteSecret))

	// A Secret in another namespace is inaccessible until a grant in the
	// Secret's namespace explicitly permits this sync namespace.
	secretSync := &konnectv1alpha1.KonnectConfigStoreSync{
		Name: "cross-secret", Namespace: e.ns, Spec: combinedSyncSpec("cross-secret-key"),
	}
	secretSync.Spec.ConfigStoreRef = commonv1alpha1.NamespacedRef{Name: e.store.Name}
	secretSync.Spec.SecretRef = commonv1alpha1.NamespacedRef{Name: remoteSecret.Name, Namespace: new(other.Name)}
	require.NoError(t, e.cl.Create(e.ctx, secretSync))
	e.waitSync(t, secretSync, konnectv1alpha1.SecretRefValidConditionType,
		konnectv1alpha1.KonnectConfigStoreSyncSecretRefReasonNotAllowed)
	assertNoConfigStoreWrites(t, e.remote)
	grant := grantConfigStoreSyncReference(t, e, e.ns, other.Name, "Secret", remoteSecret.Name)
	e.waitReady(t, secretSync, combinedHash(t, cert, key))
	assertCombinedValue(t, e.value(t, "cross-secret-key"), cert, key)
	e.waitSettled(t)

	// Revoking the grant fails closed without erasing the serving value.
	e.remote.ResetCalls()
	require.NoError(t, e.cl.Delete(e.ctx, grant))
	e.waitSync(t, secretSync, konnectv1alpha1.SecretRefValidConditionType,
		konnectv1alpha1.KonnectConfigStoreSyncSecretRefReasonNotAllowed)
	assertNoConfigStoreWrites(t, e.remote)
	assertCombinedValue(t, e.value(t, "cross-secret-key"), cert, key)

	// The same policy applies independently to a cross-namespace store.
	localSecret := &corev1.Secret{
		Name: "local-secret", Namespace: other.Name,
		Data: map[string][]byte{"tls.crt": cert, "tls.key": key},
	}
	require.NoError(t, e.cl.Create(e.ctx, localSecret))
	storeSync := &konnectv1alpha1.KonnectConfigStoreSync{
		Name: "cross-store", Namespace: other.Name, Spec: combinedSyncSpec("cross-store-key"),
	}
	storeSync.Spec.ConfigStoreRef = commonv1alpha1.NamespacedRef{Name: e.store.Name, Namespace: new(e.ns)}
	storeSync.Spec.SecretRef = commonv1alpha1.NamespacedRef{Name: localSecret.Name}
	require.NoError(t, e.cl.Create(e.ctx, storeSync))
	e.waitSync(t, storeSync, konnectv1alpha1.ConfigStoreRefValidConditionType,
		konnectv1alpha1.ConfigStoreRefReasonRefNotPermitted)
	assertNoConfigStoreWrites(t, e.remote)
	grantConfigStoreSyncReference(t, e, other.Name, e.ns, "KonnectConfigStore", e.store.Name)
	e.waitReady(t, storeSync, combinedHash(t, cert, key))
	assertCombinedValue(t, e.value(t, "cross-store-key"), cert, key)
}
