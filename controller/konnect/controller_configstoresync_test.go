package konnect

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/internal/utils/configstoresync"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

const (
	testConfigStoreSyncNamespace = "default"
	testConfigStoreSyncCPID      = "cp-12345"
	testConfigStoreSyncStoreID   = "config-store-12345"
	testConfigStoreSyncPeriod    = time.Minute
)

// -----------------------------------------------------------------------------
// Test harness
// -----------------------------------------------------------------------------

type configStoreSyncTestEnv struct {
	cl         client.Client
	fake       *sdkmocks.FakeConfigStoreSecrets
	recorder   *events.FakeRecorder
	reconciler *KonnectConfigStoreSyncReconciler
}

func newConfigStoreSyncTestEnv(t *testing.T, objs ...client.Object) *configStoreSyncTestEnv {
	t.Helper()
	builder := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(objs...).
		WithStatusSubresource(&konnectv1alpha1.KonnectConfigStoreSync{})
	for _, opt := range index.OptionsForKonnectConfigStoreSync() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	fakeSecrets := sdkmocks.NewFakeConfigStoreSecrets()
	recorder := events.NewFakeRecorder(100)
	r := &KonnectConfigStoreSyncReconciler{
		Client:        cl,
		SDKFactory:    sdkmocks.NewFakeConfigStoreSecretsSDKFactory(fakeSecrets),
		SyncPeriod:    testConfigStoreSyncPeriod,
		eventRecorder: recorder,
	}
	return &configStoreSyncTestEnv{
		cl:         cl,
		fake:       fakeSecrets,
		recorder:   recorder,
		reconciler: r,
	}
}

// reconcile fetches the named sync and reconciles it, mirroring what
// reconcile.AsReconciler does in production.
func (e *configStoreSyncTestEnv) reconcile(t *testing.T, nn types.NamespacedName) (ctrl.Result, error) {
	t.Helper()
	var sync konnectv1alpha1.KonnectConfigStoreSync
	require.NoError(t, e.cl.Get(context.Background(), nn, &sync))
	return e.reconciler.Reconcile(context.Background(), &sync)
}

func (e *configStoreSyncTestEnv) getSync(t *testing.T, nn types.NamespacedName) *konnectv1alpha1.KonnectConfigStoreSync {
	t.Helper()
	var sync konnectv1alpha1.KonnectConfigStoreSync
	require.NoError(t, e.cl.Get(context.Background(), nn, &sync))
	return &sync
}

func (e *configStoreSyncTestEnv) drainEvents() []string {
	var out []string
	for {
		select {
		case ev := <-e.recorder.Events:
			out = append(out, ev)
		default:
			return out
		}
	}
}

func eventsContain(events []string, reason string) bool {
	for _, ev := range events {
		if strings.Contains(ev, reason) {
			return true
		}
	}
	return false
}

// assertConfigStoreSyncDeleted asserts the sync object is gone. The fake
// client deletes an object once its last finalizer is removed while a
// deletion timestamp is set, so NotFound proves the finalizer was removed.
func assertConfigStoreSyncDeleted(t *testing.T, env *configStoreSyncTestEnv, nn types.NamespacedName) {
	t.Helper()
	var sync konnectv1alpha1.KonnectConfigStoreSync
	err := env.cl.Get(context.Background(), nn, &sync)
	assert.True(t, apierrors.IsNotFound(err), "expected the sync to be deleted, got %v", err)
}

// -----------------------------------------------------------------------------
// Fixtures
// -----------------------------------------------------------------------------

func newConfigStoreSyncTestAPIAuth() *konnectv1alpha1.KonnectAPIAuthConfiguration {
	return &konnectv1alpha1.KonnectAPIAuthConfiguration{
		Name:      "api-auth",
		Namespace: testConfigStoreSyncNamespace,
		Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
			Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
			Token:     "kpat_test",
			ServerURL: sdkmocks.SDKServerURL,
		},
	}
}

func newConfigStoreSyncTestControlPlane() *konnectv1alpha2.KonnectGatewayControlPlane {
	return &konnectv1alpha2.KonnectGatewayControlPlane{
		Name:      "cp",
		Namespace: testConfigStoreSyncNamespace,
		Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
			KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
					Name: "api-auth",
				},
			},
		},
		Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
			KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: testConfigStoreSyncCPID},
		},
	}
}

func newConfigStoreSyncTestConfigStore() *konnectv1alpha1.KonnectConfigStore {
	return &konnectv1alpha1.KonnectConfigStore{
		Name:      "config-store",
		Namespace: testConfigStoreSyncNamespace,
		Spec: konnectv1alpha1.KonnectConfigStoreSpec{
			ControlPlaneRef: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "cp"},
			},
		},
		Status: konnectv1alpha1.KonnectConfigStoreStatus{
			KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{ID: testConfigStoreSyncStoreID},
			ControlPlaneID:      &konnectv1alpha1.KonnectEntityRef{ID: testConfigStoreSyncCPID},
		},
	}
}

func newConfigStoreSyncTestSecret(certPEM, keyPEM []byte) *corev1.Secret {
	return &corev1.Secret{
		Name:      "tls-secret",
		Namespace: testConfigStoreSyncNamespace,
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": keyPEM,
		},
	}
}

type configStoreSyncOption func(*konnectv1alpha1.KonnectConfigStoreSync)

func withConfigStoreSyncFinalizer() configStoreSyncOption {
	return func(s *konnectv1alpha1.KonnectConfigStoreSync) {
		s.Finalizers = []string{KonnectCleanupFinalizer}
	}
}

func withConfigStoreSyncDeletionPolicy(p konnectv1alpha1.KonnectConfigStoreSyncDeletionPolicy) configStoreSyncOption {
	return func(s *konnectv1alpha1.KonnectConfigStoreSync) {
		s.Spec.DeletionPolicy = p
	}
}

// withConfigStoreSyncSharedStoreKey points the sync at the fixed explicit
// store key shared by the conflict tests.
func withConfigStoreSyncSharedStoreKey() configStoreSyncOption {
	return func(s *konnectv1alpha1.KonnectConfigStoreSync) {
		if s.Spec.Combined == nil {
			s.Spec.Combined = &konnectv1alpha1.KonnectConfigStoreSyncCombined{}
		}
		s.Spec.Combined.StoreKey = new("shared-key")
	}
}

func withConfigStoreSyncSplit(entries ...konnectv1alpha1.KonnectConfigStoreSyncSplitEntry) configStoreSyncOption {
	return func(s *konnectv1alpha1.KonnectConfigStoreSync) {
		s.Spec.Mode = konnectv1alpha1.KonnectConfigStoreSyncModeSplit
		s.Spec.Combined = nil
		s.Spec.Split = &konnectv1alpha1.KonnectConfigStoreSyncSplit{Entries: entries}
	}
}

func withConfigStoreSyncCreationTimestamp(ts time.Time) configStoreSyncOption {
	return func(s *konnectv1alpha1.KonnectConfigStoreSync) {
		s.CreationTimestamp = metav1.NewTime(ts)
	}
}

func newConfigStoreSync(name string, opts ...configStoreSyncOption) *konnectv1alpha1.KonnectConfigStoreSync {
	s := &konnectv1alpha1.KonnectConfigStoreSync{
		Name:      name,
		Namespace: testConfigStoreSyncNamespace,
		Spec: konnectv1alpha1.KonnectConfigStoreSyncSpec{
			ConfigStoreRef: commonv1alpha1.NamespacedRef{Name: "config-store"},
			SecretRef:      commonv1alpha1.NamespacedRef{Name: "tls-secret"},
			Mode:           konnectv1alpha1.KonnectConfigStoreSyncModeCombined,
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func configStoreSyncNN(name string) types.NamespacedName {
	return types.NamespacedName{Namespace: testConfigStoreSyncNamespace, Name: name}
}

func findConfigStoreSyncCondition(sync *konnectv1alpha1.KonnectConfigStoreSync, condType string) *metav1.Condition {
	return apimeta.FindStatusCondition(sync.Status.Conditions, condType)
}

func requireConfigStoreSyncCondition(
	t *testing.T,
	sync *konnectv1alpha1.KonnectConfigStoreSync,
	condType string,
	status metav1.ConditionStatus,
	reason string,
) {
	t.Helper()
	cond := findConfigStoreSyncCondition(sync, condType)
	require.NotNil(t, cond, "condition %s must be present", condType)
	assert.Equal(t, status, cond.Status, "condition %s status", condType)
	assert.Equal(t, reason, cond.Reason, "condition %s reason", condType)
}

// assertNoPlaintext scans everything user-visible (conditions, entry status,
// events) for Secret plaintext. The absolute constraint of the feature is
// that only hashes ever appear.
func assertNoPlaintext(t *testing.T, sync *konnectv1alpha1.KonnectConfigStoreSync, events []string, secrets ...[]byte) {
	t.Helper()
	var visible strings.Builder
	for _, c := range sync.Status.Conditions {
		visible.WriteString(c.Message)
	}
	for _, e := range sync.Status.Entries {
		visible.WriteString(e.Hash)
	}
	for _, ev := range events {
		visible.WriteString(ev)
	}
	for _, s := range secrets {
		// PEM blocks share their first line; check a distinctive interior
		// fragment to avoid trivial matches.
		fragment := string(s)
		if len(fragment) > 40 {
			fragment = fragment[20:40]
		}
		assert.NotContains(t, visible.String(), fragment, "Secret plaintext must never appear in status or events")
	}
}

// baseObjects returns the objects every reconcile test needs: auth, control
// plane, programmed store.
func configStoreSyncBaseObjects() []client.Object {
	return []client.Object{
		newConfigStoreSyncTestAPIAuth(),
		newConfigStoreSyncTestControlPlane(),
		newConfigStoreSyncTestConfigStore(),
	}
}

// -----------------------------------------------------------------------------
// Tests
// -----------------------------------------------------------------------------

func TestKonnectConfigStoreSyncCombinedHappyPath(t *testing.T) {
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()
	secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
	sync := newConfigStoreSync("sync")

	objs := append(configStoreSyncBaseObjects(), secret, sync)
	env := newConfigStoreSyncTestEnv(t, objs...)
	nn := configStoreSyncNN("sync")

	// First reconcile adds the finalizer and stops.
	res, err := env.reconcile(t, nn)
	require.NoError(t, err)
	assert.True(t, res.IsZero())
	assert.Empty(t, env.fake.Calls(), "no Konnect calls before the finalizer lands")
	assert.Contains(t, env.getSync(t, nn).Finalizers, KonnectCleanupFinalizer)

	// Second reconcile performs the first write.
	res, err = env.reconcile(t, nn)
	require.NoError(t, err)
	assert.Equal(t, testConfigStoreSyncPeriod, res.RequeueAfter)

	got := env.getSync(t, nn)
	derivedKey := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")

	// Store state.
	value, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey)
	require.True(t, ok, "entry must exist in the store")
	var parsed map[string]string
	require.NoError(t, json.Unmarshal([]byte(value), &parsed))
	assert.Equal(t, string(certPEM), parsed["certificate"])
	assert.Equal(t, string(keyPEM), parsed["key"])

	// Status.
	assert.Equal(t, testConfigStoreSyncStoreID, got.Status.StoreID)
	assert.Equal(t, testConfigStoreSyncCPID, got.Status.ControlPlaneID)
	assert.Equal(t, int32(1), got.Status.EntriesTotal)
	assert.Equal(t, int32(1), got.Status.EntriesSynced)
	require.Len(t, got.Status.Entries, 1)
	entry := got.Status.Entries[0]
	assert.Equal(t, derivedKey, entry.StoreKey)
	assert.Equal(t, []string{"tls.crt", "tls.key"}, entry.SourceFields)
	assert.True(t, strings.HasPrefix(entry.Hash, "sha256:"), "hash must be sha256-prefixed")
	assert.Equal(t, int64(len(derivedKey)), entry.KeyBytes)
	assert.Positive(t, entry.ValueBytes)
	assert.NotNil(t, entry.NotAfter)
	assert.NotNil(t, entry.LastPushTime)
	assert.NotNil(t, entry.ObservedUpdatedAt)
	assert.NotEmpty(t, got.Status.ObservedSecretResourceVersion)

	// References: suffix always published, Combined mode has subfields.
	require.Len(t, got.Status.References, 2)
	suffixes := map[string]string{}
	for _, ref := range got.Status.References {
		suffixes[ref.SubField] = ref.Suffix
	}
	assert.Equal(t, derivedKey+"/certificate", suffixes["certificate"])
	assert.Equal(t, derivedKey+"/key", suffixes["key"])

	// Conditions.
	requireConfigStoreSyncCondition(t, got, konnectv1alpha1.ConfigStoreRefValidConditionType,
		metav1.ConditionTrue, konnectv1alpha1.ConfigStoreRefReasonValid)
	requireConfigStoreSyncCondition(t, got, konnectv1alpha1.SecretRefValidConditionType,
		metav1.ConditionTrue, konnectv1alpha1.SecretRefReasonValid)
	requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncPairValidConditionType,
		metav1.ConditionTrue, konnectConfigStoreSyncPairValidReasonValid)
	requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
		metav1.ConditionTrue, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonAllEntriesUpToDate)

	events := env.drainEvents()
	assert.True(t, eventsContain(events, "Synced"), "expected Synced event, got %v", events)
	assertNoPlaintext(t, got, events, certPEM, keyPEM)
}

func TestKonnectConfigStoreSyncSteadyStateMakesNoMutatingCalls(t *testing.T) {
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()
	secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
	sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())

	objs := append(configStoreSyncBaseObjects(), secret, sync)
	env := newConfigStoreSyncTestEnv(t, objs...)
	nn := configStoreSyncNN("sync")

	_, err := env.reconcile(t, nn)
	require.NoError(t, err)
	require.NotEmpty(t, env.fake.MutatingCalls(), "first reconcile must write")

	env.fake.ResetCalls()
	env.drainEvents()

	res, err := env.reconcile(t, nn)
	require.NoError(t, err)
	assert.Equal(t, testConfigStoreSyncPeriod, res.RequeueAfter)
	assert.Empty(t, env.fake.MutatingCalls(), "steady-state reconcile must make zero mutating Konnect calls")
	assert.Empty(t, env.drainEvents(), "steady-state reconcile must not emit events")
}

func TestKonnectConfigStoreSyncPairGate(t *testing.T) {
	certPEM, _ := certificate.MustGenerateCertPEMFormat()
	_, otherKeyPEM := certificate.MustGenerateCertPEMFormat()

	t.Run("mismatched pair on empty store writes nothing", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, otherKeyPEM)
		sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())
		objs := append(configStoreSyncBaseObjects(), secret, sync)
		env := newConfigStoreSyncTestEnv(t, objs...)
		nn := configStoreSyncNN("sync")

		_, err := env.reconcile(t, nn)
		require.NoError(t, err)

		assert.Empty(t, env.fake.Calls(), "pair gate fails before any Konnect call")
		got := env.getSync(t, nn)
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncPairValidConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncPairValidReasonPairMismatch)
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonPairMismatch)
		events := env.drainEvents()
		assert.True(t, eventsContain(events, "PairMismatch"), "expected PairMismatch event, got %v", events)
		assertNoPlaintext(t, got, events, certPEM, otherKeyPEM)
	})

	t.Run("mismatched pair with existing entry writes nothing", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, otherKeyPEM)
		sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())
		objs := append(configStoreSyncBaseObjects(), secret, sync)
		env := newConfigStoreSyncTestEnv(t, objs...)
		nn := configStoreSyncNN("sync")

		// Pre-existing entry in the store (e.g. written before the Secret was
		// rotated to a broken pair).
		derivedKey := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")
		env.fake.SetValue(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey, `{"certificate":"old","key":"old"}`)

		_, err := env.reconcile(t, nn)
		require.NoError(t, err)
		assert.Empty(t, env.fake.Calls(), "pair gate fails before any Konnect call")

		// The previous value keeps serving.
		value, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey)
		require.True(t, ok)
		assert.JSONEq(t, `{"certificate":"old","key":"old"}`, value)
	})

	t.Run("missing key field is a pair mismatch", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, nil)
		delete(secret.Data, "tls.key")
		sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())
		objs := append(configStoreSyncBaseObjects(), secret, sync)
		env := newConfigStoreSyncTestEnv(t, objs...)

		_, err := env.reconcile(t, configStoreSyncNN("sync"))
		require.NoError(t, err)
		assert.Empty(t, env.fake.Calls())
		got := env.getSync(t, configStoreSyncNN("sync"))
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncPairValidConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncPairValidReasonPairMismatch)
		cond := findConfigStoreSyncCondition(got, konnectv1alpha1.KonnectConfigStoreSyncPairValidConditionType)
		assert.Contains(t, cond.Message, `secret data field "tls.key" is missing`,
			"the message must name the missing field, got %q", cond.Message)
	})

	t.Run("missing cert field is a pair mismatch", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(nil, otherKeyPEM)
		delete(secret.Data, "tls.crt")
		sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())
		objs := append(configStoreSyncBaseObjects(), secret, sync)
		env := newConfigStoreSyncTestEnv(t, objs...)

		_, err := env.reconcile(t, configStoreSyncNN("sync"))
		require.NoError(t, err)
		assert.Empty(t, env.fake.Calls())
		got := env.getSync(t, configStoreSyncNN("sync"))
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncPairValidConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncPairValidReasonPairMismatch)
		cond := findConfigStoreSyncCondition(got, konnectv1alpha1.KonnectConfigStoreSyncPairValidConditionType)
		assert.Contains(t, cond.Message, `secret data field "tls.crt" is missing`,
			"the message must name the missing field, got %q", cond.Message)
	})

	t.Run("split mode skips the pair gate", func(t *testing.T) {
		// Split mode syncs raw fields; the values need not form a pair.
		secret := newConfigStoreSyncTestSecret(certPEM, otherKeyPEM)
		sync := newConfigStoreSync("sync",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSplit(
				konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.crt"},
				konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.key"},
			),
		)
		objs := append(configStoreSyncBaseObjects(), secret, sync)
		env := newConfigStoreSyncTestEnv(t, objs...)
		nn := configStoreSyncNN("sync")

		_, err := env.reconcile(t, nn)
		require.NoError(t, err)
		assert.Len(t, env.fake.MutatingCalls(), 2, "both split entries written")

		got := env.getSync(t, nn)
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncPairValidConditionType,
			metav1.ConditionTrue, konnectv1alpha1.KonnectConfigStoreSyncPairValidReasonNotApplicable)
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionTrue, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonAllEntriesUpToDate)
	})
}

func TestKonnectConfigStoreSyncKeyTooLong(t *testing.T) {
	// The Split derived-key worst case: max-length namespace (63) and name
	// (253) plus a max-length field (253) yields a 583-byte key, over the
	// 512-byte cap. CEL cannot express this cross-field length check, so the
	// controller enforces it pre-write and fails closed.
	longNS := strings.Repeat("n", 63)
	longName := strings.Repeat("a", 253)
	longField := strings.Repeat("f", 253)

	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()
	secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
	secret.Namespace = longNS
	secret.Data[longField] = []byte("value")

	sync := newConfigStoreSync(longName,
		withConfigStoreSyncFinalizer(),
		withConfigStoreSyncSplit(konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: longField}),
	)
	sync.Namespace = longNS

	store := newConfigStoreSyncTestConfigStore()
	store.Namespace = longNS
	cp := newConfigStoreSyncTestControlPlane()
	cp.Namespace = longNS
	apiAuth := newConfigStoreSyncTestAPIAuth()
	apiAuth.Namespace = longNS

	env := newConfigStoreSyncTestEnv(t, apiAuth, cp, store, secret, sync)
	nn := types.NamespacedName{Namespace: longNS, Name: longName}

	_, err := env.reconcile(t, nn)
	require.NoError(t, err)

	assert.Empty(t, env.fake.Calls(), "key-length pre-flight fails before any Konnect call")
	got := env.getSync(t, nn)
	requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
		metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonKeyTooLong)
	events := env.drainEvents()
	assert.True(t, eventsContain(events, "KeyTooLong"), "expected KeyTooLong event, got %v", events)
}

func TestKonnectConfigStoreSyncDuplicateStoreKeys(t *testing.T) {
	// A Split entry's explicit storeKey colliding with another entry's
	// derived key is not caught by CRD validation (explicit storeKeys are
	// only checked against each other). The controller must reject it
	// pre-write and fail closed: duplicates would double-write the same
	// Config Store entry and the status update would be rejected by the API
	// server (listMapKey=storeKey).
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()
	secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
	colliding := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync") + "-tls.crt"
	sync := newConfigStoreSync("sync",
		withConfigStoreSyncFinalizer(),
		withConfigStoreSyncSplit(
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.crt"},
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.key", StoreKey: &colliding},
		),
	)
	objs := append(configStoreSyncBaseObjects(), secret, sync)
	env := newConfigStoreSyncTestEnv(t, objs...)

	_, err := env.reconcile(t, configStoreSyncNN("sync"))
	require.NoError(t, err)

	assert.Empty(t, env.fake.Calls(), "duplicate store keys are rejected before any Konnect call")
	got := env.getSync(t, configStoreSyncNN("sync"))
	requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
		metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonKeyConflict)
	cond := findConfigStoreSyncCondition(got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType)
	assert.Contains(t, cond.Message, "multiple entries of this sync")
	events := env.drainEvents()
	assert.True(t, eventsContain(events, "KeyConflict"), "expected KeyConflict event, got %v", events)
}

func TestKonnectConfigStoreSyncValueTooLarge(t *testing.T) {
	secret := newConfigStoreSyncTestSecret(nil, nil)
	secret.Data = map[string][]byte{
		"big": make([]byte, configstoresync.MaxValueBytes+1),
	}
	sync := newConfigStoreSync("sync",
		withConfigStoreSyncFinalizer(),
		withConfigStoreSyncSplit(konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "big"}),
	)
	objs := append(configStoreSyncBaseObjects(), secret, sync)
	env := newConfigStoreSyncTestEnv(t, objs...)
	nn := configStoreSyncNN("sync")

	_, err := env.reconcile(t, nn)
	require.NoError(t, err)

	assert.Empty(t, env.fake.Calls(), "size pre-flight fails before any Konnect call")
	got := env.getSync(t, nn)
	requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
		metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonValueTooLarge)
	events := env.drainEvents()
	assert.True(t, eventsContain(events, "ValueTooLarge"), "expected ValueTooLarge event, got %v", events)
}

func TestKonnectConfigStoreSyncConflictOwnershipRequiresDurableEntry(t *testing.T) {
	older := time.Now().Add(-time.Hour)
	newer := time.Now()

	t.Run("never-written fail-closed sync does not claim the key", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(nil, nil)
		secret.Data = map[string][]byte{
			"too-big": make([]byte, configstoresync.MaxValueBytes+1),
			"healthy": []byte("healthy-value"),
		}
		failed := newConfigStoreSync("failed",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncCreationTimestamp(older),
			withConfigStoreSyncSplit(konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "too-big",
				StoreKey: new("shared-key"),
			}),
		)
		healthy := newConfigStoreSync("healthy",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncCreationTimestamp(newer),
			withConfigStoreSyncSplit(konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "healthy",
				StoreKey: new("shared-key"),
			}),
		)
		objs := append(configStoreSyncBaseObjects(), secret, failed, healthy)
		env := newConfigStoreSyncTestEnv(t, objs...)

		_, err := env.reconcile(t, configStoreSyncNN("failed"))
		require.NoError(t, err)
		gotFailed := env.getSync(t, configStoreSyncNN("failed"))
		assert.Equal(t, testConfigStoreSyncStoreID, gotFailed.Status.StoreID)
		assert.Empty(t, gotFailed.Status.Entries, "failed pre-flight must not create durable ownership")
		requireConfigStoreSyncCondition(t, gotFailed, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonValueTooLarge)

		_, err = env.reconcile(t, configStoreSyncNN("healthy"))
		require.NoError(t, err)
		value, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, "shared-key")
		require.True(t, ok)
		assert.Equal(t, "healthy-value", value)
		gotHealthy := env.getSync(t, configStoreSyncNN("healthy"))
		requireConfigStoreSyncCondition(t, gotHealthy, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionTrue, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonAllEntriesUpToDate)
	})

	t.Run("previously-written fail-closed sync retains the key", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(nil, nil)
		secret.Data = map[string][]byte{
			"owner":     []byte("serving-value"),
			"contender": []byte("contender-value"),
		}
		owner := newConfigStoreSync("owner",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncCreationTimestamp(older),
			withConfigStoreSyncSplit(konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "owner",
				StoreKey: new("shared-key"),
			}),
		)
		contender := newConfigStoreSync("contender",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncCreationTimestamp(newer),
			withConfigStoreSyncSplit(konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "contender",
				StoreKey: new("shared-key"),
			}),
		)
		objs := append(configStoreSyncBaseObjects(), secret, owner, contender)
		env := newConfigStoreSyncTestEnv(t, objs...)

		_, err := env.reconcile(t, configStoreSyncNN("owner"))
		require.NoError(t, err)
		require.Len(t, env.getSync(t, configStoreSyncNN("owner")).Status.Entries, 1)

		secret = new(corev1.Secret)
		require.NoError(t, env.cl.Get(context.Background(), configStoreSyncNN("tls-secret"), secret))
		secret.Data["owner"] = make([]byte, configstoresync.MaxValueBytes+1)
		require.NoError(t, env.cl.Update(context.Background(), secret))
		_, err = env.reconcile(t, configStoreSyncNN("owner"))
		require.NoError(t, err)
		requireConfigStoreSyncCondition(t, env.getSync(t, configStoreSyncNN("owner")),
			konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonValueTooLarge)

		env.fake.ResetCalls()
		_, err = env.reconcile(t, configStoreSyncNN("contender"))
		require.NoError(t, err)
		assert.Empty(t, env.fake.Calls(), "contender must not touch the serving owner's key")
		value, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, "shared-key")
		require.True(t, ok)
		assert.Equal(t, "serving-value", value)
		requireConfigStoreSyncCondition(t, env.getSync(t, configStoreSyncNN("contender")),
			konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonKeyConflict)
	})

	t.Run("re-created store does not inherit ownership from the old store ID", func(t *testing.T) {
		const newStoreID = "config-store-recreated"
		secret := newConfigStoreSyncTestSecret(nil, nil)
		secret.Data = map[string][]byte{
			"owner":     []byte("old-store-value"),
			"contender": []byte("new-store-value"),
		}
		owner := newConfigStoreSync("owner",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncCreationTimestamp(older),
			withConfigStoreSyncSplit(konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "owner",
				StoreKey: new("shared-key"),
			}),
		)
		contender := newConfigStoreSync("contender",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncCreationTimestamp(newer),
			withConfigStoreSyncSplit(konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "contender",
				StoreKey: new("shared-key"),
			}),
		)
		objs := append(configStoreSyncBaseObjects(), secret, owner, contender)
		env := newConfigStoreSyncTestEnv(t, objs...)

		_, err := env.reconcile(t, configStoreSyncNN("owner"))
		require.NoError(t, err)
		require.Len(t, env.getSync(t, configStoreSyncNN("owner")).Status.Entries, 1)

		var store konnectv1alpha1.KonnectConfigStore
		require.NoError(t, env.cl.Get(context.Background(), configStoreSyncNN("config-store"), &store))
		store.Status.ID = newStoreID
		require.NoError(t, env.cl.Update(context.Background(), &store))

		secret = new(corev1.Secret)
		require.NoError(t, env.cl.Get(context.Background(), configStoreSyncNN("tls-secret"), secret))
		secret.Data["owner"] = make([]byte, configstoresync.MaxValueBytes+1)
		require.NoError(t, env.cl.Update(context.Background(), secret))

		_, err = env.reconcile(t, configStoreSyncNN("owner"))
		require.NoError(t, err)
		gotOwner := env.getSync(t, configStoreSyncNN("owner"))
		assert.Equal(t, newStoreID, gotOwner.Status.StoreID)
		assert.Empty(t, gotOwner.Status.Entries, "old-store entries must not claim keys in the re-created store")
		assert.Empty(t, gotOwner.Status.References)
		assert.Zero(t, gotOwner.Status.EntriesSynced)
		assert.Empty(t, gotOwner.Status.ObservedSecretResourceVersion)
		requireConfigStoreSyncCondition(t, gotOwner,
			konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonValueTooLarge)

		_, err = env.reconcile(t, configStoreSyncNN("contender"))
		require.NoError(t, err)
		value, ok := env.fake.Value(testConfigStoreSyncCPID, newStoreID, "shared-key")
		require.True(t, ok)
		assert.Equal(t, "new-store-value", value)
		requireConfigStoreSyncCondition(t, env.getSync(t, configStoreSyncNN("contender")),
			konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionTrue, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonAllEntriesUpToDate)
	})
}

func TestKonnectConfigStoreSyncConflictOrderIndependence(t *testing.T) {
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()
	older := time.Now().Add(-time.Hour)
	newer := time.Now()

	// runReconciles reconciles both syncs in the given order (round 1), then
	// resets the fake's call log and reconciles both again (round 2). It
	// returns the resulting sync states plus the round-2 mutating calls.
	//
	// The conflict index is convergent, not atomic (D3): a sync only appears
	// in the index after a successful write is recorded in status. When the
	// newer sync reconciles first it writes before the older sync is indexed,
	// and the older sync then re-writes with PUT on its first reconcile. Round
	// 2 is the converged steady state: the loser reports KeyConflict and
	// neither party writes.
	run := func(t *testing.T, first, second string) (*konnectv1alpha1.KonnectConfigStoreSync, *konnectv1alpha1.KonnectConfigStoreSync, []sdkmocks.ConfigStoreSecretsCall) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		syncA := newConfigStoreSync("sync-a",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSharedStoreKey(),
			withConfigStoreSyncCreationTimestamp(older),
		)
		syncB := newConfigStoreSync("sync-b",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSharedStoreKey(),
			withConfigStoreSyncCreationTimestamp(newer),
		)
		objs := append(configStoreSyncBaseObjects(), secret, syncA, syncB)
		env := newConfigStoreSyncTestEnv(t, objs...)

		for _, name := range []string{first, second} {
			_, err := env.reconcile(t, configStoreSyncNN(name))
			require.NoError(t, err)
		}
		env.fake.ResetCalls()
		for _, name := range []string{first, second} {
			_, err := env.reconcile(t, configStoreSyncNN(name))
			require.NoError(t, err)
		}
		return env.getSync(t, configStoreSyncNN("sync-a")),
			env.getSync(t, configStoreSyncNN("sync-b")),
			env.fake.MutatingCalls()
	}

	t.Run("older first", func(t *testing.T) {
		a, b, mutatingCalls := run(t, "sync-a", "sync-b")
		requireConfigStoreSyncCondition(t, a, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionTrue, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonAllEntriesUpToDate)
		requireConfigStoreSyncCondition(t, b, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonKeyConflict)
		assert.Empty(t, mutatingCalls, "converged: loser blocked, winner steady-state")
	})

	t.Run("newer first", func(t *testing.T) {
		a, b, mutatingCalls := run(t, "sync-b", "sync-a")
		requireConfigStoreSyncCondition(t, a, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionTrue, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonAllEntriesUpToDate)
		requireConfigStoreSyncCondition(t, b, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonKeyConflict)
		require.Len(t, b.Status.Entries, 1, "a previous writer retains durable ownership while losing")
		assert.Zero(t, b.Status.EntriesSynced)
		assert.Empty(t, mutatingCalls, "converged: loser blocked, winner steady-state")
	})

	t.Run("both parties emit KeyConflict events", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		syncA := newConfigStoreSync("sync-a",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSharedStoreKey(),
			withConfigStoreSyncCreationTimestamp(older),
		)
		syncB := newConfigStoreSync("sync-b",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSharedStoreKey(),
			withConfigStoreSyncCreationTimestamp(newer),
		)
		objs := append(configStoreSyncBaseObjects(), secret, syncA, syncB)
		env := newConfigStoreSyncTestEnv(t, objs...)

		// Reconcile the newer sync first so both parties durably owned the key
		// during convergence and therefore both observe the conflict.
		_, err := env.reconcile(t, configStoreSyncNN("sync-b"))
		require.NoError(t, err)
		_, err = env.reconcile(t, configStoreSyncNN("sync-a"))
		require.NoError(t, err)
		_, err = env.reconcile(t, configStoreSyncNN("sync-b"))
		require.NoError(t, err)

		events := env.drainEvents()
		assert.True(t, eventsContain(events, "KeyConflict"), "expected KeyConflict events, got %v", events)
	})

	t.Run("loser requeues and recovers after winner deletion", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		syncA := newConfigStoreSync("sync-a",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSharedStoreKey(),
			withConfigStoreSyncCreationTimestamp(older),
		)
		syncB := newConfigStoreSync("sync-b",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSharedStoreKey(),
			withConfigStoreSyncCreationTimestamp(newer),
		)
		objs := append(configStoreSyncBaseObjects(), secret, syncA, syncB)
		env := newConfigStoreSyncTestEnv(t, objs...)

		_, err := env.reconcile(t, configStoreSyncNN("sync-a"))
		require.NoError(t, err)
		res, err := env.reconcile(t, configStoreSyncNN("sync-b"))
		require.NoError(t, err)
		assert.Equal(t, testConfigStoreSyncPeriod, res.RequeueAfter,
			"conflict loser must keep requeuing: there is no sync-on-sync watch")

		// Delete the winner (default Orphan policy): the loser recovers on
		// its next reconcile and adopts the orphaned entry.
		winner := env.getSync(t, configStoreSyncNN("sync-a"))
		require.NoError(t, env.cl.Delete(context.Background(), winner))
		_, err = env.reconcile(t, configStoreSyncNN("sync-a"))
		require.NoError(t, err)

		env.fake.ResetCalls()
		_, err = env.reconcile(t, configStoreSyncNN("sync-b"))
		require.NoError(t, err)
		assert.NotEmpty(t, env.fake.MutatingCalls(), "loser must write once the winner is gone")
		got := env.getSync(t, configStoreSyncNN("sync-b"))
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionTrue, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonAllEntriesUpToDate)
	})

	t.Run("all-losing sync reports conflict without credentials", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		winner := newConfigStoreSync("winner",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSharedStoreKey(),
			withConfigStoreSyncCreationTimestamp(older),
		)
		loser := newConfigStoreSync("loser",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSharedStoreKey(),
			withConfigStoreSyncCreationTimestamp(newer),
		)
		objs := append(configStoreSyncBaseObjects(), secret, winner, loser)
		env := newConfigStoreSyncTestEnv(t, objs...)

		// Let the eventual loser write first so this also proves that stale
		// references are removed while its durable entry record is retained.
		_, err := env.reconcile(t, configStoreSyncNN("loser"))
		require.NoError(t, err)
		_, err = env.reconcile(t, configStoreSyncNN("winner"))
		require.NoError(t, err)

		var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
		require.NoError(t, env.cl.Get(context.Background(), configStoreSyncNN("api-auth"), &apiAuth))
		require.NoError(t, env.cl.Delete(context.Background(), &apiAuth))
		env.fake.ResetCalls()

		_, err = env.reconcile(t, configStoreSyncNN("loser"))
		require.NoError(t, err)
		assert.Empty(t, env.fake.Calls())
		got := env.getSync(t, configStoreSyncNN("loser"))
		require.Len(t, got.Status.Entries, 1, "the previous write remains durable election evidence")
		assert.Zero(t, got.Status.EntriesSynced)
		assert.Empty(t, got.Status.References, "a losing key must not publish a synced reference")
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonKeyConflict)
	})
}

func TestKonnectConfigStoreSyncConflictsAreResolvedPerKey(t *testing.T) {
	older := time.Now().Add(-time.Hour)
	middle := time.Now().Add(-30 * time.Minute)
	newer := time.Now()

	newSplitSync := func(name string, created time.Time, entries ...konnectv1alpha1.KonnectConfigStoreSyncSplitEntry) *konnectv1alpha1.KonnectConfigStoreSync {
		return newConfigStoreSync(name,
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncCreationTimestamp(created),
			withConfigStoreSyncSplit(entries...),
		)
	}

	t.Run("losing one key does not block another key", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(nil, nil)
		secret.Data = map[string][]byte{
			"winner-key-1": []byte("winner-key-1-value"),
			"sync-key-1":   []byte("sync-key-1-value"),
			"sync-key-2":   []byte("sync-key-2-value"),
		}
		winner := newSplitSync("winner", older,
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "winner-key-1",
				StoreKey: new("key-1"),
			},
		)
		sync := newSplitSync("sync", middle,
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "sync-key-1",
				StoreKey: new("key-1"),
			},
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "sync-key-2",
				StoreKey: new("key-2"),
			},
		)
		objs := append(configStoreSyncBaseObjects(), secret, winner, sync)
		env := newConfigStoreSyncTestEnv(t, objs...)

		_, err := env.reconcile(t, configStoreSyncNN("winner"))
		require.NoError(t, err)
		env.fake.ResetCalls()

		res, err := env.reconcile(t, configStoreSyncNN("sync"))
		require.NoError(t, err)
		assert.Equal(t, testConfigStoreSyncPeriod, res.RequeueAfter)
		for _, call := range env.fake.MutatingCalls() {
			assert.NotEqual(t, "key-1", call.Key, "losing key must not be mutated")
		}
		value, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, "key-1")
		require.True(t, ok)
		assert.Equal(t, "winner-key-1-value", value)
		value, ok = env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, "key-2")
		require.True(t, ok)
		assert.Equal(t, "sync-key-2-value", value)

		got := env.getSync(t, configStoreSyncNN("sync"))
		assert.Equal(t, int32(2), got.Status.EntriesTotal)
		assert.Equal(t, int32(1), got.Status.EntriesSynced)
		require.Len(t, got.Status.Entries, 1, "never-written losing key must not gain ownership")
		assert.Equal(t, "key-2", got.Status.Entries[0].StoreKey)
		require.Equal(t,
			[]konnectv1alpha1.KonnectConfigStoreSyncReference{{Suffix: "key-2"}},
			got.Status.References,
		)
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonKeyConflict)
		condition := findConfigStoreSyncCondition(got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType)
		assert.Equal(t, `store key "key-1" is owned by sync default/winner`, condition.Message)

		// Removing the winner lets only the previously losing key recover;
		// key-2 remains a steady-state no-op.
		require.NoError(t, env.cl.Delete(context.Background(), env.getSync(t, configStoreSyncNN("winner"))))
		_, err = env.reconcile(t, configStoreSyncNN("winner"))
		require.NoError(t, err)
		env.fake.ResetCalls()
		_, err = env.reconcile(t, configStoreSyncNN("sync"))
		require.NoError(t, err)
		mutatingCalls := env.fake.MutatingCalls()
		require.Len(t, mutatingCalls, 1)
		assert.Equal(t, "key-1", mutatingCalls[0].Key)
		got = env.getSync(t, configStoreSyncNN("sync"))
		assert.Equal(t, int32(2), got.Status.EntriesSynced)
		require.Len(t, got.Status.Entries, 2)
		require.ElementsMatch(t,
			[]konnectv1alpha1.KonnectConfigStoreSyncReference{{Suffix: "key-1"}, {Suffix: "key-2"}},
			got.Status.References,
		)
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionTrue, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonAllEntriesUpToDate)
	})

	t.Run("three-way cross conflict leaves every key maintained", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(nil, nil)
		secret.Data = map[string][]byte{
			"b-key-1": []byte("b-key-1-value"),
			"a-key-1": []byte("a-key-1-value"),
			"a-key-2": []byte("a-key-2-value"),
			"c-key-2": []byte("c-key-2-value"),
		}
		syncB := newSplitSync("sync-b", older,
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "b-key-1",
				StoreKey: new("key-1"),
			},
		)
		syncA := newSplitSync("sync-a", middle,
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "a-key-1",
				StoreKey: new("key-1"),
			},
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "a-key-2",
				StoreKey: new("key-2"),
			},
		)
		syncC := newSplitSync("sync-c", newer,
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "c-key-2",
				StoreKey: new("key-2"),
			},
		)
		objs := append(configStoreSyncBaseObjects(), secret, syncA, syncB, syncC)
		env := newConfigStoreSyncTestEnv(t, objs...)

		_, err := env.reconcile(t, configStoreSyncNN("sync-b"))
		require.NoError(t, err)
		_, err = env.reconcile(t, configStoreSyncNN("sync-a"))
		require.NoError(t, err)
		env.fake.ResetCalls()
		_, err = env.reconcile(t, configStoreSyncNN("sync-c"))
		require.NoError(t, err)
		assert.Empty(t, env.fake.Calls(), "sync-c loses key-2 and makes no Konnect calls")

		value, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, "key-1")
		require.True(t, ok)
		assert.Equal(t, "b-key-1-value", value)
		value, ok = env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, "key-2")
		require.True(t, ok)
		assert.Equal(t, "a-key-2-value", value)

		gotA := env.getSync(t, configStoreSyncNN("sync-a"))
		assert.Equal(t, int32(1), gotA.Status.EntriesSynced)
		require.Len(t, gotA.Status.Entries, 1)
		assert.Equal(t, "key-2", gotA.Status.Entries[0].StoreKey)
		require.Equal(t,
			[]konnectv1alpha1.KonnectConfigStoreSyncReference{{Suffix: "key-2"}},
			gotA.Status.References,
		)
		requireConfigStoreSyncCondition(t, gotA, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonKeyConflict)

		gotC := env.getSync(t, configStoreSyncNN("sync-c"))
		assert.Zero(t, gotC.Status.EntriesSynced)
		assert.Empty(t, gotC.Status.Entries)
		assert.Empty(t, gotC.Status.References)
		requireConfigStoreSyncCondition(t, gotC, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonKeyConflict)
	})

	t.Run("multiple losing keys produce a stable message", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(nil, nil)
		secret.Data = map[string][]byte{
			"winner-a":  []byte("winner-a"),
			"winner-z":  []byte("winner-z"),
			"current-a": []byte("current-a"),
			"current-z": []byte("current-z"),
		}
		winnerA := newSplitSync("winner-a", older,
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "winner-a",
				StoreKey: new("a-key"),
			},
		)
		winnerZ := newSplitSync("winner-z", older,
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "winner-z",
				StoreKey: new("z-key"),
			},
		)
		current := newSplitSync("current", newer,
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "current-z",
				StoreKey: new("z-key"),
			},
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "current-a",
				StoreKey: new("a-key"),
			},
		)
		objs := append(configStoreSyncBaseObjects(), secret, winnerA, winnerZ, current)
		env := newConfigStoreSyncTestEnv(t, objs...)

		_, err := env.reconcile(t, configStoreSyncNN("winner-a"))
		require.NoError(t, err)
		_, err = env.reconcile(t, configStoreSyncNN("winner-z"))
		require.NoError(t, err)
		_, err = env.reconcile(t, configStoreSyncNN("current"))
		require.NoError(t, err)

		condition := findConfigStoreSyncCondition(
			env.getSync(t, configStoreSyncNN("current")),
			konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
		)
		assert.Equal(t,
			`store key "a-key" is owned by sync default/winner-a; store key "z-key" is owned by sync default/winner-z`,
			condition.Message,
		)
	})
}

func TestKonnectConfigStoreSyncSplitWritesBothEntries(t *testing.T) {
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()
	secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
	sync := newConfigStoreSync("sync",
		withConfigStoreSyncFinalizer(),
		withConfigStoreSyncSplit(
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.crt"},
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.key"},
		),
	)
	objs := append(configStoreSyncBaseObjects(), secret, sync)
	env := newConfigStoreSyncTestEnv(t, objs...)
	nn := configStoreSyncNN("sync")

	_, err := env.reconcile(t, nn)
	require.NoError(t, err)

	derived := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")
	certValue, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derived+"-tls.crt")
	require.True(t, ok)
	assert.Equal(t, string(certPEM), certValue, "Split stores raw field values")
	keyValue, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derived+"-tls.key")
	require.True(t, ok)
	assert.Equal(t, string(keyPEM), keyValue)

	got := env.getSync(t, nn)
	assert.Equal(t, int32(2), got.Status.EntriesTotal)
	assert.Equal(t, int32(2), got.Status.EntriesSynced)
	require.Len(t, got.Status.Entries, 2)

	// Split references carry no subfield.
	require.Len(t, got.Status.References, 2)
	suffixes := []string{}
	for _, ref := range got.Status.References {
		assert.Empty(t, ref.SubField)
		suffixes = append(suffixes, ref.Suffix)
	}
	assert.Contains(t, suffixes, derived+"-tls.crt")
	assert.Contains(t, suffixes, derived+"-tls.key")
}

func TestKonnectConfigStoreSyncSplitPartialFailure(t *testing.T) {
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()
	secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
	sync := newConfigStoreSync("sync",
		withConfigStoreSyncFinalizer(),
		withConfigStoreSyncSplit(
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.crt"},
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.key"},
		),
	)
	objs := append(configStoreSyncBaseObjects(), secret, sync)
	env := newConfigStoreSyncTestEnv(t, objs...)
	nn := configStoreSyncNN("sync")

	derived := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")
	secondKey := derived + "-tls.key"
	env.fake.ErrorHook = func(method, key string) error {
		if key == secondKey {
			return sdkkonnecterrs.NewSDKError("api error", 500, "internal error", nil)
		}
		return nil
	}

	_, err := env.reconcile(t, nn)
	require.Error(t, err, "a failed write surfaces an error for backoff retry")

	// The first entry was written; the second was not: a half-applied set.
	_, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derived+"-tls.crt")
	assert.True(t, ok, "first entry written")
	_, ok = env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, secondKey)
	assert.False(t, ok, "second entry not written")

	got := env.getSync(t, nn)
	requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
		metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonPushFailed)
	events := env.drainEvents()
	assert.True(t, eventsContain(events, konnectConfigStoreSyncEventReasonSplitWriteIncomplete),
		"expected the distinct SplitWriteIncomplete signal, got %v", events)

	// The entry written before the failure is recorded in status, so the next
	// reconcile does not mistake it for external drift.
	require.Len(t, got.Status.Entries, 1)
	assert.Equal(t, derived+"-tls.crt", got.Status.Entries[0].StoreKey)
	assert.NotEmpty(t, got.Status.Entries[0].Hash)
	assert.NotNil(t, got.Status.Entries[0].ObservedUpdatedAt)

	// Recovery: with the failure cleared, the next reconcile writes only the
	// missing entry and raises no false Drifted warning for the first.
	env.fake.ErrorHook = nil
	_, err = env.reconcile(t, nn)
	require.NoError(t, err)
	_, ok = env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, secondKey)
	assert.True(t, ok, "second entry written on recovery")
	events = env.drainEvents()
	assert.False(t, eventsContain(events, "Drifted"),
		"no false Drifted event for the entry written before the failure, got %v", events)
	assert.True(t, eventsContain(events, "Synced"), "expected Synced event after recovery, got %v", events)
}

func TestKonnectConfigStoreSyncSecretRotation(t *testing.T) {
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()
	secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
	sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())

	objs := append(configStoreSyncBaseObjects(), secret, sync)
	env := newConfigStoreSyncTestEnv(t, objs...)
	nn := configStoreSyncNN("sync")
	derivedKey := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")

	_, err := env.reconcile(t, nn)
	require.NoError(t, err)
	before := env.getSync(t, nn)
	require.Len(t, before.Status.Entries, 1)
	prevHash := before.Status.Entries[0].Hash

	// Rotate the Secret: the entry is re-written with the new value. This is
	// a spec-side change, not drift: no Drifted warning.
	newCertPEM, newKeyPEM := certificate.MustGenerateCertPEMFormat()
	var sec corev1.Secret
	require.NoError(t, env.cl.Get(context.Background(), types.NamespacedName{
		Namespace: testConfigStoreSyncNamespace, Name: "tls-secret",
	}, &sec))
	sec.Data["tls.crt"] = newCertPEM
	sec.Data["tls.key"] = newKeyPEM
	require.NoError(t, env.cl.Update(context.Background(), &sec))
	env.drainEvents()

	res, err := env.reconcile(t, nn)
	require.NoError(t, err)
	assert.Equal(t, testConfigStoreSyncPeriod, res.RequeueAfter)

	value, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey)
	require.True(t, ok)
	var parsed map[string]string
	require.NoError(t, json.Unmarshal([]byte(value), &parsed))
	assert.Equal(t, string(newCertPEM), parsed["certificate"])
	assert.Equal(t, string(newKeyPEM), parsed["key"])

	got := env.getSync(t, nn)
	require.Len(t, got.Status.Entries, 1)
	assert.NotEqual(t, prevHash, got.Status.Entries[0].Hash, "hash tracks the rotated value")
	requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
		metav1.ConditionTrue, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonAllEntriesUpToDate)
	events := env.drainEvents()
	assert.False(t, eventsContain(events, "Drifted"), "rotation is a spec-side change, not drift, got %v", events)
	assertNoPlaintext(t, got, events, newCertPEM, newKeyPEM)
}

func TestKonnectConfigStoreSyncSecretRefRepoint(t *testing.T) {
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()
	secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
	newCertPEM, newKeyPEM := certificate.MustGenerateCertPEMFormat()
	secret2 := &corev1.Secret{
		Name:      "tls-secret-2",
		Namespace: testConfigStoreSyncNamespace,
		Data:      map[string][]byte{"tls.crt": newCertPEM, "tls.key": newKeyPEM},
	}
	sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())

	objs := append(configStoreSyncBaseObjects(), secret, secret2, sync)
	env := newConfigStoreSyncTestEnv(t, objs...)
	nn := configStoreSyncNN("sync")
	derivedKey := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")

	_, err := env.reconcile(t, nn)
	require.NoError(t, err)

	// Repoint secretRef: the same store key now serves the new Secret's
	// value.
	got := env.getSync(t, nn)
	got.Spec.SecretRef = commonv1alpha1.NamespacedRef{Name: "tls-secret-2"}
	require.NoError(t, env.cl.Update(context.Background(), got))
	env.drainEvents()

	res, err := env.reconcile(t, nn)
	require.NoError(t, err)
	assert.Equal(t, testConfigStoreSyncPeriod, res.RequeueAfter)

	value, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey)
	require.True(t, ok)
	var parsed map[string]string
	require.NoError(t, json.Unmarshal([]byte(value), &parsed))
	assert.Equal(t, string(newCertPEM), parsed["certificate"])
	assert.Equal(t, string(newKeyPEM), parsed["key"])

	got = env.getSync(t, nn)
	requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
		metav1.ConditionTrue, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonAllEntriesUpToDate)
	events := env.drainEvents()
	assert.False(t, eventsContain(events, "Drifted"), "repoint is a spec-side change, not drift, got %v", events)
	assertNoPlaintext(t, got, events, newCertPEM, newKeyPEM)
}

func TestKonnectConfigStoreSyncSpecEntryRemoval(t *testing.T) {
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()

	splitSync := func() *konnectv1alpha1.KonnectConfigStoreSync {
		return newConfigStoreSync("sync",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSplit(
				konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.crt"},
				konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.key"},
			),
		)
	}

	t.Run("removed entry is pruned from the store", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		sync := splitSync()
		objs := append(configStoreSyncBaseObjects(), secret, sync)
		env := newConfigStoreSyncTestEnv(t, objs...)
		nn := configStoreSyncNN("sync")
		derived := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")

		_, err := env.reconcile(t, nn)
		require.NoError(t, err)
		require.Len(t, env.getSync(t, nn).Status.Entries, 2)

		// Remove tls.key from the spec: the entry must be deleted from the
		// store and dropped from status.
		got := env.getSync(t, nn)
		got.Spec.Split.Entries = got.Spec.Split.Entries[:1]
		require.NoError(t, env.cl.Update(context.Background(), got))
		env.fake.ResetCalls()
		env.drainEvents()

		res, err := env.reconcile(t, nn)
		require.NoError(t, err)
		assert.Equal(t, testConfigStoreSyncPeriod, res.RequeueAfter)

		_, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derived+"-tls.key")
		assert.False(t, ok, "removed entry pruned from the store")
		_, ok = env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derived+"-tls.crt")
		assert.True(t, ok, "remaining entry untouched")

		got = env.getSync(t, nn)
		require.Len(t, got.Status.Entries, 1)
		assert.Equal(t, derived+"-tls.crt", got.Status.Entries[0].StoreKey)
		assert.Equal(t, int32(1), got.Status.EntriesTotal)
		assert.Equal(t, int32(1), got.Status.EntriesSynced)
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionTrue, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonAllEntriesUpToDate)
		events := env.drainEvents()
		assert.True(t, eventsContain(events, "EntryPruned"), "expected EntryPruned event, got %v", events)

		// Steady state after the prune: no mutating calls.
		env.fake.ResetCalls()
		env.drainEvents()
		_, err = env.reconcile(t, nn)
		require.NoError(t, err)
		assert.Empty(t, env.fake.MutatingCalls(), "steady state after prune makes no mutating calls")
	})

	t.Run("prune blocked while the removed entry is in use", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		sync := splitSync()
		derived := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")
		removedKey := derived + "-tls.key"
		cert := &configurationv1alpha1.KongCertificate{
			Name: "cert", Namespace: testConfigStoreSyncNamespace,
			Spec: configurationv1alpha1.KongCertificateSpec{
				Cert: fmt.Sprintf("{vault://my-vault/%s}", removedKey),
			},
		}
		objs := append(configStoreSyncBaseObjects(), secret, sync, cert)
		env := newConfigStoreSyncTestEnv(t, objs...)
		nn := configStoreSyncNN("sync")

		_, err := env.reconcile(t, nn)
		require.NoError(t, err)
		require.Len(t, env.getSync(t, nn).Status.Entries, 2)

		got := env.getSync(t, nn)
		got.Spec.Split.Entries = got.Spec.Split.Entries[:1]
		require.NoError(t, env.cl.Update(context.Background(), got))

		res, err := env.reconcile(t, nn)
		require.NoError(t, err)
		assert.Equal(t, ctrlconsts.KonnectConfigStoreDeletionBlockedRequeuePeriod, res.RequeueAfter)

		// The entry stays in the store and in status; the prune is retried.
		_, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, removedKey)
		assert.True(t, ok, "in-use entry not deleted")
		got = env.getSync(t, nn)
		require.Len(t, got.Status.Entries, 2, "blocked entry record kept")
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonEntryInUse)
		events := env.drainEvents()
		assert.True(t, eventsContain(events, "EntryInUse"), "expected EntryInUse event, got %v", events)

		// Once the reference is gone, the retry prunes the entry.
		require.NoError(t, env.cl.Delete(context.Background(), cert))
		res, err = env.reconcile(t, nn)
		require.NoError(t, err)
		assert.Equal(t, testConfigStoreSyncPeriod, res.RequeueAfter)
		_, ok = env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, removedKey)
		assert.False(t, ok, "entry pruned once no longer in use")
		got = env.getSync(t, nn)
		require.Len(t, got.Status.Entries, 1)
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionTrue, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonAllEntriesUpToDate)
	})

	t.Run("prune deletes unreferenced keys while retaining referenced keys", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		secret.Data["extra"] = []byte("extra-value")
		sync := newConfigStoreSync("sync",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSplit(
				konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.crt"},
				konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.key"},
				konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "extra"},
			),
		)
		derived := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")
		referencedKey := derived + "-tls.key"
		unreferencedKey := derived + "-extra"
		cert := &configurationv1alpha1.KongCertificate{
			Name: "cert", Namespace: testConfigStoreSyncNamespace,
			Spec: configurationv1alpha1.KongCertificateSpec{
				Cert: fmt.Sprintf("{vault://my-vault/%s}", referencedKey),
			},
		}
		objs := append(configStoreSyncBaseObjects(), secret, sync, cert)
		env := newConfigStoreSyncTestEnv(t, objs...)
		nn := configStoreSyncNN("sync")

		_, err := env.reconcile(t, nn)
		require.NoError(t, err)
		require.Len(t, env.getSync(t, nn).Status.Entries, 3)

		got := env.getSync(t, nn)
		got.Spec.Split.Entries = got.Spec.Split.Entries[:1]
		require.NoError(t, env.cl.Update(context.Background(), got))
		env.fake.ResetCalls()

		res, err := env.reconcile(t, nn)
		require.NoError(t, err)
		assert.Equal(t, ctrlconsts.KonnectConfigStoreDeletionBlockedRequeuePeriod, res.RequeueAfter)
		_, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, referencedKey)
		assert.True(t, ok, "referenced key remains")
		_, ok = env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, unreferencedKey)
		assert.False(t, ok, "unreferenced key is pruned independently")

		got = env.getSync(t, nn)
		require.Len(t, got.Status.Entries, 2)
		assert.NotNil(t, findEntryStatus(got.Status.Entries, referencedKey), "referenced key remains tracked")
		assert.Nil(t, findEntryStatus(got.Status.Entries, unreferencedKey), "pruned key is dropped from status")
	})

	t.Run("64-entry replacement retains one entry awaiting cleanup", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		entries := make([]konnectv1alpha1.KonnectConfigStoreSyncSplitEntry, 0, 64)
		for i := range 64 {
			field := fmt.Sprintf("field-%02d", i)
			key := fmt.Sprintf("key-%02d", i)
			secret.Data[field] = []byte("value-" + field)
			entries = append(entries, konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    field,
				StoreKey: new(key),
			})
		}
		sync := newConfigStoreSync("sync",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSplit(entries...),
		)
		cert := &configurationv1alpha1.KongCertificate{
			Name: "cert", Namespace: testConfigStoreSyncNamespace,
			Spec: configurationv1alpha1.KongCertificateSpec{
				Cert: "{vault://my-vault/key-00}",
			},
		}
		objs := append(configStoreSyncBaseObjects(), secret, sync, cert)
		env := newConfigStoreSyncTestEnv(t, objs...)
		nn := configStoreSyncNN("sync")

		_, err := env.reconcile(t, nn)
		require.NoError(t, err)
		require.Len(t, env.getSync(t, nn).Status.Entries, 64)

		got := env.getSync(t, nn)
		secret = new(corev1.Secret)
		require.NoError(t, env.cl.Get(context.Background(), types.NamespacedName{
			Namespace: testConfigStoreSyncNamespace,
			Name:      "tls-secret",
		}, secret))
		secret.Data["field-new"] = []byte("value-field-new")
		require.NoError(t, env.cl.Update(context.Background(), secret))
		got.Spec.Split.Entries = append(
			got.Spec.Split.Entries[1:],
			konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    "field-new",
				StoreKey: new("key-new"),
			},
		)
		require.NoError(t, env.cl.Update(context.Background(), got))

		res, err := env.reconcile(t, nn)
		require.NoError(t, err)
		assert.Equal(t, ctrlconsts.KonnectConfigStoreDeletionBlockedRequeuePeriod, res.RequeueAfter)
		got = env.getSync(t, nn)
		require.Len(t, got.Status.Entries, 65)
		assert.NotNil(t, findEntryStatus(got.Status.Entries, "key-00"), "blocked old key remains tracked")
		assert.NotNil(t, findEntryStatus(got.Status.Entries, "key-new"), "replacement key is tracked")
		_, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, "key-new")
		assert.True(t, ok, "replacement key is written")
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonEntryInUse)
	})

	t.Run("writes are deferred before pending cleanup would exceed status capacity", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		entries := make([]konnectv1alpha1.KonnectConfigStoreSyncSplitEntry, 0, 64)
		statusEntries := make([]konnectv1alpha1.KonnectConfigStoreSyncEntryStatus, 0, 128)
		for i := 1; i < 64; i++ {
			field := fmt.Sprintf("field-%02d", i)
			key := fmt.Sprintf("key-%02d", i)
			secret.Data[field] = []byte("value-" + field)
			entries = append(entries, konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
				Field:    field,
				StoreKey: new(key),
			})
			statusEntries = append(statusEntries, konnectv1alpha1.KonnectConfigStoreSyncEntryStatus{
				StoreKey:     key,
				SourceFields: []string{field},
			})
		}
		secret.Data["field-new"] = []byte("value-field-new")
		entries = append(entries, konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
			Field:    "field-new",
			StoreKey: new("key-new"),
		})

		objects := configStoreSyncBaseObjects()
		for i := range 65 {
			key := fmt.Sprintf("old-%02d", i)
			statusEntries = append(statusEntries, konnectv1alpha1.KonnectConfigStoreSyncEntryStatus{
				StoreKey:     key,
				SourceFields: []string{"old"},
			})
			objects = append(objects, &configurationv1alpha1.KongCertificate{
				Name: fmt.Sprintf("cert-%02d", i), Namespace: testConfigStoreSyncNamespace,
				Spec: configurationv1alpha1.KongCertificateSpec{
					Cert: fmt.Sprintf("{vault://my-vault/%s}", key),
				},
			})
		}
		sync := newConfigStoreSync("sync",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSplit(entries...),
		)
		sync.Status.Entries = statusEntries
		objects = append(objects, secret, sync)
		env := newConfigStoreSyncTestEnv(t, objects...)

		res, err := env.reconcile(t, configStoreSyncNN("sync"))
		require.NoError(t, err)
		assert.Equal(t, ctrlconsts.KonnectConfigStoreDeletionBlockedRequeuePeriod, res.RequeueAfter)
		assert.Empty(t, env.fake.Calls(), "no remote reads or writes occur without status capacity")

		got := env.getSync(t, configStoreSyncNN("sync"))
		require.Len(t, got.Status.Entries, konnectConfigStoreSyncMaxStatusEntries)
		assert.Nil(t, findEntryStatus(got.Status.Entries, "key-new"), "deferred key is not reported as written")
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonEntryInUse)
	})

	t.Run("prune failure keeps the status record and retries", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		sync := splitSync()
		objs := append(configStoreSyncBaseObjects(), secret, sync)
		env := newConfigStoreSyncTestEnv(t, objs...)
		nn := configStoreSyncNN("sync")
		derived := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")
		removedKey := derived + "-tls.key"

		_, err := env.reconcile(t, nn)
		require.NoError(t, err)

		got := env.getSync(t, nn)
		got.Spec.Split.Entries = got.Spec.Split.Entries[:1]
		require.NoError(t, env.cl.Update(context.Background(), got))

		env.fake.ErrorHook = func(method, key string) error {
			if method == "Delete" && key == removedKey {
				return sdkkonnecterrs.NewSDKError("api error", 500, "internal error", nil)
			}
			return nil
		}
		_, err = env.reconcile(t, nn)
		require.Error(t, err, "a failed prune surfaces an error for backoff retry")

		// The entry stays in the store; its record is kept for the retry.
		_, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, removedKey)
		assert.True(t, ok, "entry not deleted")
		got = env.getSync(t, nn)
		require.Len(t, got.Status.Entries, 2, "failed entry record kept for retry")
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonPushFailed)

		env.fake.ErrorHook = nil
		_, err = env.reconcile(t, nn)
		require.NoError(t, err)
		_, ok = env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, removedKey)
		assert.False(t, ok, "entry pruned on retry")
		got = env.getSync(t, nn)
		require.Len(t, got.Status.Entries, 1)
	})

	t.Run("prune relinquishes a key won by an older sync", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		older := time.Now().Add(-time.Hour)
		newer := time.Now()
		olderSync := newConfigStoreSync("older",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSharedStoreKey(),
			withConfigStoreSyncCreationTimestamp(older),
		)
		newerSync := newConfigStoreSync("newer",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncCreationTimestamp(newer),
			withConfigStoreSyncSplit(
				konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
					Field:    "tls.crt",
					StoreKey: new("shared-key"),
				},
				konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.key"},
			),
		)
		objs := append(configStoreSyncBaseObjects(), secret, olderSync, newerSync)
		env := newConfigStoreSyncTestEnv(t, objs...)

		// The newer sync writes before the older sync is indexed. The older
		// sync then wins the convergent election and overwrites shared-key.
		_, err := env.reconcile(t, configStoreSyncNN("newer"))
		require.NoError(t, err)
		_, err = env.reconcile(t, configStoreSyncNN("older"))
		require.NoError(t, err)

		// Removing shared-key from the newer sync must drop its stale status
		// record without deleting the older winner's value.
		got := env.getSync(t, configStoreSyncNN("newer"))
		got.Spec.Split.Entries = got.Spec.Split.Entries[1:]
		require.NoError(t, env.cl.Update(context.Background(), got))
		env.fake.ResetCalls()

		_, err = env.reconcile(t, configStoreSyncNN("newer"))
		require.NoError(t, err)

		_, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, "shared-key")
		assert.True(t, ok, "the older winner's entry must remain")
		for _, call := range env.fake.MutatingCalls() {
			assert.False(t, call.Method == "Delete" && call.Key == "shared-key",
				"the conflict loser must not prune the winner's entry")
		}
		got = env.getSync(t, configStoreSyncNN("newer"))
		require.Len(t, got.Status.Entries, 1)
		assert.NotEqual(t, "shared-key", got.Status.Entries[0].StoreKey)
	})

	t.Run("older sync relinquishes a key claimed by a newer sync", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		older := time.Now().Add(-time.Hour)
		newer := time.Now()
		olderSync := newConfigStoreSync("older",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncCreationTimestamp(older),
			withConfigStoreSyncSplit(
				konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{
					Field:    "tls.crt",
					StoreKey: new("shared-key"),
				},
				konnectv1alpha1.KonnectConfigStoreSyncSplitEntry{Field: "tls.key"},
			),
		)
		newerSync := newConfigStoreSync("newer",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSharedStoreKey(),
			withConfigStoreSyncCreationTimestamp(newer),
		)
		newerSync.Spec.ConfigStoreRef.Name = "alias-store"
		aliasStore := newConfigStoreSyncTestConfigStore()
		aliasStore.Name = "alias-store"
		objs := append(configStoreSyncBaseObjects(), secret, olderSync, newerSync, aliasStore)
		env := newConfigStoreSyncTestEnv(t, objs...)

		_, err := env.reconcile(t, configStoreSyncNN("older"))
		require.NoError(t, err)

		got := env.getSync(t, configStoreSyncNN("older"))
		got.Spec.Split.Entries = got.Spec.Split.Entries[1:]
		require.NoError(t, env.cl.Update(context.Background(), got))

		_, err = env.reconcile(t, configStoreSyncNN("newer"))
		require.NoError(t, err)
		valueBeforeCleanup, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, "shared-key")
		require.True(t, ok)
		env.fake.ResetCalls()

		_, err = env.reconcile(t, configStoreSyncNN("older"))
		require.NoError(t, err)
		valueAfterCleanup, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, "shared-key")
		require.True(t, ok, "the active successor's value must remain")
		assert.Equal(t, valueBeforeCleanup, valueAfterCleanup)
		for _, call := range env.fake.MutatingCalls() {
			assert.False(t, call.Method == "Delete" && call.Key == "shared-key",
				"the former owner must not delete the active successor's value")
		}
		got = env.getSync(t, configStoreSyncNN("older"))
		assert.Nil(t, findEntryStatus(got.Status.Entries, "shared-key"), "relinquished key is dropped from status")
	})
}

func TestKonnectConfigStoreSyncDeletion(t *testing.T) {
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()

	newDeletingSync := func(policy konnectv1alpha1.KonnectConfigStoreSyncDeletionPolicy) *konnectv1alpha1.KonnectConfigStoreSync {
		sync := newConfigStoreSync("sync",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncDeletionPolicy(policy),
		)
		sync.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		return sync
	}

	t.Run("orphan leaves entries and removes the finalizer", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		sync := newDeletingSync(konnectv1alpha1.KonnectConfigStoreSyncDeletionPolicyOrphan)
		objs := append(configStoreSyncBaseObjects(), secret, sync)
		env := newConfigStoreSyncTestEnv(t, objs...)
		nn := configStoreSyncNN("sync")

		derivedKey := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")
		env.fake.SetValue(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey, "value")

		_, err := env.reconcile(t, nn)
		require.NoError(t, err)

		assert.Empty(t, env.fake.MutatingCalls(), "orphan deletes nothing")
		value, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey)
		require.True(t, ok, "entry left in place")
		assert.Equal(t, "value", value)
		assertConfigStoreSyncDeleted(t, env, nn)
	})

	t.Run("delete removes entries and removes the finalizer", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		sync := newDeletingSync(konnectv1alpha1.KonnectConfigStoreSyncDeletionPolicyDelete)
		objs := append(configStoreSyncBaseObjects(), secret, sync)
		env := newConfigStoreSyncTestEnv(t, objs...)
		nn := configStoreSyncNN("sync")

		derivedKey := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")
		env.fake.SetValue(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey, "value")

		_, err := env.reconcile(t, nn)
		require.NoError(t, err)

		assert.True(t, env.fake.StoreEmpty(testConfigStoreSyncCPID, testConfigStoreSyncStoreID),
			"entries deleted from the store")
		assertConfigStoreSyncDeleted(t, env, nn)
	})

	t.Run("conflict loser delete does not remove the winner's entry", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		older := time.Now().Add(-time.Hour)
		newer := time.Now()
		winner := newConfigStoreSync("winner",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSharedStoreKey(),
			withConfigStoreSyncCreationTimestamp(older),
		)
		loser := newConfigStoreSync("loser",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSharedStoreKey(),
			withConfigStoreSyncCreationTimestamp(newer),
			withConfigStoreSyncDeletionPolicy(konnectv1alpha1.KonnectConfigStoreSyncDeletionPolicyDelete),
		)
		loser.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		objs := append(configStoreSyncBaseObjects(), secret, winner, loser)
		env := newConfigStoreSyncTestEnv(t, objs...)

		_, err := env.reconcile(t, configStoreSyncNN("winner"))
		require.NoError(t, err)
		env.fake.ResetCalls()

		_, err = env.reconcile(t, configStoreSyncNN("loser"))
		require.NoError(t, err)

		_, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, "shared-key")
		assert.True(t, ok, "deleting the conflict loser must preserve the winner's entry")
		assert.Empty(t, env.fake.MutatingCalls())
		assertConfigStoreSyncDeleted(t, env, configStoreSyncNN("loser"))
	})

	t.Run("delete relinquishes a status-only key claimed by an active sync", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		deleting := newDeletingSync(konnectv1alpha1.KonnectConfigStoreSyncDeletionPolicyDelete)
		deleting.Status.Entries = []konnectv1alpha1.KonnectConfigStoreSyncEntryStatus{
			{
				StoreKey:     "shared-key",
				SourceFields: []string{"tls.crt"},
			},
		}
		successor := newConfigStoreSync("successor",
			withConfigStoreSyncFinalizer(),
			withConfigStoreSyncSharedStoreKey(),
		)
		objs := append(configStoreSyncBaseObjects(), secret, deleting, successor)
		env := newConfigStoreSyncTestEnv(t, objs...)
		env.fake.SetValue(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, "shared-key", "successor-value")

		_, err := env.reconcile(t, configStoreSyncNN("sync"))
		require.NoError(t, err)

		value, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, "shared-key")
		require.True(t, ok, "the active successor's value must remain")
		assert.Equal(t, "successor-value", value)
		for _, call := range env.fake.MutatingCalls() {
			assert.False(t, call.Method == "Delete" && call.Key == "shared-key",
				"status-only ownership must be relinquished to the active successor")
		}
		assertConfigStoreSyncDeleted(t, env, configStoreSyncNN("sync"))
	})

	for _, tc := range []struct {
		name   string
		setRef func(*configurationv1alpha1.KongCertificateSpec, string)
	}{
		{
			name: "cert",
			setRef: func(spec *configurationv1alpha1.KongCertificateSpec, ref string) {
				spec.Cert = ref
			},
		},
		{
			name: "key",
			setRef: func(spec *configurationv1alpha1.KongCertificateSpec, ref string) {
				spec.Key = ref
			},
		},
		{
			name: "alternate cert",
			setRef: func(spec *configurationv1alpha1.KongCertificateSpec, ref string) {
				spec.CertAlt = ref
			},
		},
		{
			name: "alternate key",
			setRef: func(spec *configurationv1alpha1.KongCertificateSpec, ref string) {
				spec.KeyAlt = ref
			},
		},
	} {
		t.Run("delete blocked while an entry is referenced by "+tc.name, func(t *testing.T) {
			secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
			sync := newDeletingSync(konnectv1alpha1.KonnectConfigStoreSyncDeletionPolicyDelete)
			derivedKey := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")
			cert := &configurationv1alpha1.KongCertificate{
				Name: "cert", Namespace: testConfigStoreSyncNamespace,
			}
			tc.setRef(&cert.Spec, fmt.Sprintf("{vault://my-vault/%s/certificate} \n\t", derivedKey))
			objs := append(configStoreSyncBaseObjects(), secret, sync, cert)
			env := newConfigStoreSyncTestEnv(t, objs...)
			nn := configStoreSyncNN("sync")
			env.fake.SetValue(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey, "value")

			res, err := env.reconcile(t, nn)
			require.NoError(t, err)
			assert.Equal(t, ctrlconsts.KonnectConfigStoreDeletionBlockedRequeuePeriod, res.RequeueAfter)

			got := env.getSync(t, nn)
			requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
				metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonEntryInUse)
			assert.Contains(t, got.Finalizers, KonnectCleanupFinalizer, "finalizer held")
			_, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey)
			assert.True(t, ok, "entry not deleted")
			events := env.drainEvents()
			assert.True(t, eventsContain(events, "EntryInUse"), "expected EntryInUse event, got %v", events)
		})
	}

	t.Run("delete from an unprogrammed store removes the finalizer", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		sync := newDeletingSync(konnectv1alpha1.KonnectConfigStoreSyncDeletionPolicyDelete)
		derivedKey := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")
		cert := &configurationv1alpha1.KongCertificate{
			Name: "cert", Namespace: testConfigStoreSyncNamespace,
			Spec: configurationv1alpha1.KongCertificateSpec{
				KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
					Cert: fmt.Sprintf("{vault://my-vault/%s/certificate}", derivedKey),
				},
			},
		}
		store := newConfigStoreSyncTestConfigStore()
		store.Status = konnectv1alpha1.KonnectConfigStoreStatus{}
		env := newConfigStoreSyncTestEnv(t,
			newConfigStoreSyncTestAPIAuth(), newConfigStoreSyncTestControlPlane(), store, secret, sync, cert)
		nn := configStoreSyncNN("sync")

		_, err := env.reconcile(t, nn)
		require.NoError(t, err)
		assert.Empty(t, env.fake.Calls(), "no Konnect calls when the store was never programmed")
		assertConfigStoreSyncDeleted(t, env, nn)
	})

	t.Run("delete blocked while the store is terminating", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		sync := newDeletingSync(konnectv1alpha1.KonnectConfigStoreSyncDeletionPolicyDelete)
		store := newConfigStoreSyncTestConfigStore()
		store.Finalizers = []string{KonnectCleanupFinalizer}
		store.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		env := newConfigStoreSyncTestEnv(t,
			newConfigStoreSyncTestAPIAuth(), newConfigStoreSyncTestControlPlane(), store, secret, sync)
		nn := configStoreSyncNN("sync")

		res, err := env.reconcile(t, nn)
		require.NoError(t, err)
		assert.Equal(t, ctrlconsts.KonnectConfigStoreDeletionBlockedRequeuePeriod, res.RequeueAfter)

		got := env.getSync(t, nn)
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonConfigStoreDeletionBlocked)
		assert.Contains(t, got.Finalizers, KonnectCleanupFinalizer, "finalizer held")
		events := env.drainEvents()
		assert.True(t, eventsContain(events, "ConfigStoreDeletionBlocked"), "expected event, got %v", events)
	})

	t.Run("delete with a gone store removes the finalizer", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		sync := newDeletingSync(konnectv1alpha1.KonnectConfigStoreSyncDeletionPolicyDelete)
		// No store object.
		env := newConfigStoreSyncTestEnv(t,
			newConfigStoreSyncTestAPIAuth(), newConfigStoreSyncTestControlPlane(), secret, sync)
		nn := configStoreSyncNN("sync")

		_, err := env.reconcile(t, nn)
		require.NoError(t, err)
		assert.Empty(t, env.fake.Calls(), "no Konnect calls when the store is gone")
		assertConfigStoreSyncDeleted(t, env, nn)
	})
}

func TestKonnectConfigStoreSyncCrashSafety(t *testing.T) {
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()

	t.Run("create-then-crash before status persist does not leak", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())

		// Fail the first status patch to simulate a crash between the Konnect
		// write and the status persist.
		var failPatchOnce bool
		builder := fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(append(configStoreSyncBaseObjects(), secret, sync)...).
			WithStatusSubresource(&konnectv1alpha1.KonnectConfigStoreSync{}).
			WithInterceptorFuncs(interceptor.Funcs{
				SubResourcePatch: func(
					ctx context.Context, cl client.Client, subResourceName string,
					obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption,
				) error {
					if !failPatchOnce {
						failPatchOnce = true
						return sdkkonnecterrs.NewSDKError("simulated crash", 500, "crash", nil)
					}
					return cl.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
				},
			})
		for _, opt := range index.OptionsForKonnectConfigStoreSync() {
			builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
		}
		cl := builder.Build()
		fakeSecrets := sdkmocks.NewFakeConfigStoreSecrets()
		recorder := events.NewFakeRecorder(100)
		r := &KonnectConfigStoreSyncReconciler{
			Client:        cl,
			SDKFactory:    sdkmocks.NewFakeConfigStoreSecretsSDKFactory(fakeSecrets),
			SyncPeriod:    testConfigStoreSyncPeriod,
			eventRecorder: recorder,
		}
		nn := configStoreSyncNN("sync")

		// First reconcile: writes the entry, then the status patch fails.
		var fetched konnectv1alpha1.KonnectConfigStoreSync
		require.NoError(t, cl.Get(context.Background(), nn, &fetched))
		_, err := r.Reconcile(context.Background(), &fetched)
		require.Error(t, err)

		derivedKey := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")
		_, ok := fakeSecrets.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey)
		require.True(t, ok, "entry was written before the crash")

		// The persisted status has no record of the entry.
		persisted := &konnectv1alpha1.KonnectConfigStoreSync{}
		require.NoError(t, cl.Get(context.Background(), nn, persisted))
		assert.Empty(t, persisted.Status.Entries)

		// Second reconcile: the key is re-derived from the spec, the existing
		// entry is adopted via PUT, and nothing leaks.
		require.NoError(t, cl.Get(context.Background(), nn, &fetched))
		_, err = r.Reconcile(context.Background(), &fetched)
		require.NoError(t, err)

		var creates, updates int
		for _, c := range fakeSecrets.Calls() {
			switch c.Method {
			case "Create":
				creates++
			case "Update":
				updates++
			}
		}
		assert.Zero(t, creates)
		assert.Equal(t, 2, updates, "one PUT upsert per reconcile")

		persisted = &konnectv1alpha1.KonnectConfigStoreSync{}
		require.NoError(t, cl.Get(context.Background(), nn, persisted))
		requireConfigStoreSyncCondition(t, persisted, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionTrue, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonAllEntriesUpToDate)
	})

	t.Run("orphan then recreate with the same name is idempotent", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())
		objs := append(configStoreSyncBaseObjects(), secret, sync)
		env := newConfigStoreSyncTestEnv(t, objs...)
		nn := configStoreSyncNN("sync")

		// Write the entry, then orphan-delete the sync.
		_, err := env.reconcile(t, nn)
		require.NoError(t, err)
		require.NoError(t, env.cl.Delete(context.Background(), env.getSync(t, nn)))
		_, err = env.reconcile(t, nn)
		require.NoError(t, err)

		derivedKey := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")
		_, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey)
		require.True(t, ok, "orphaned entry remains")

		// Recreate with the same name: the derived key is identical, so the
		// new sync adopts the existing entry instead of failing on it.
		recreated := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())
		require.NoError(t, env.cl.Create(context.Background(), recreated))
		env.fake.ResetCalls()
		env.drainEvents()

		_, err = env.reconcile(t, nn)
		require.NoError(t, err)
		got := env.getSync(t, nn)
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionTrue, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonAllEntriesUpToDate)
		events := env.drainEvents()
		assert.True(t, eventsContain(events, "AdoptedExistingEntry"),
			"expected adoption event, got %v", events)
	})
}

func TestKonnectConfigStoreSyncDriftAndRecreation(t *testing.T) {
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()

	t.Run("out-of-band modification is detected and rewritten", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())
		objs := append(configStoreSyncBaseObjects(), secret, sync)
		env := newConfigStoreSyncTestEnv(t, objs...)
		nn := configStoreSyncNN("sync")

		_, err := env.reconcile(t, nn)
		require.NoError(t, err)
		derivedKey := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")

		// External actor modifies the entry (updated_at advances).
		env.fake.SetValue(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey, `{"certificate":"evil","key":"evil"}`)
		env.fake.ResetCalls()
		env.drainEvents()

		_, err = env.reconcile(t, nn)
		require.NoError(t, err)

		value, _ := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey)
		var parsed map[string]string
		require.NoError(t, json.Unmarshal([]byte(value), &parsed))
		assert.Equal(t, string(certPEM), parsed["certificate"], "drift rewritten")
		events := env.drainEvents()
		assert.True(t, eventsContain(events, "Drifted"), "expected Drifted event, got %v", events)
	})

	t.Run("out-of-band deletion is recreated", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())
		objs := append(configStoreSyncBaseObjects(), secret, sync)
		env := newConfigStoreSyncTestEnv(t, objs...)
		nn := configStoreSyncNN("sync")

		_, err := env.reconcile(t, nn)
		require.NoError(t, err)
		derivedKey := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")

		env.fake.DeleteValue(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey)
		env.fake.ResetCalls()
		env.drainEvents()

		_, err = env.reconcile(t, nn)
		require.NoError(t, err)

		_, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey)
		assert.True(t, ok, "entry recreated")
		events := env.drainEvents()
		assert.True(t, eventsContain(events, "EntryRecreated"), "expected EntryRecreated event, got %v", events)
	})
}

func TestKonnectConfigStoreSyncReferenceValidation(t *testing.T) {
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()

	t.Run("store not programmed waits", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())
		store := newConfigStoreSyncTestConfigStore()
		store.Status = konnectv1alpha1.KonnectConfigStoreStatus{}
		env := newConfigStoreSyncTestEnv(t,
			newConfigStoreSyncTestAPIAuth(), newConfigStoreSyncTestControlPlane(), store, secret, sync)

		_, err := env.reconcile(t, configStoreSyncNN("sync"))
		require.NoError(t, err)
		assert.Empty(t, env.fake.Calls())
		got := env.getSync(t, configStoreSyncNN("sync"))
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.ConfigStoreRefValidConditionType,
			metav1.ConditionFalse, konnectv1alpha1.ConfigStoreRefReasonNotProgrammed)
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSyncedReasonWaitingForConfigStore)
	})

	t.Run("missing secret preserves store data", func(t *testing.T) {
		sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())
		objs := append(configStoreSyncBaseObjects(), sync)
		env := newConfigStoreSyncTestEnv(t, objs...)
		nn := configStoreSyncNN("sync")

		derivedKey := configstoresync.DerivedKey(testConfigStoreSyncNamespace, "sync")
		env.fake.SetValue(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey, "previous")

		_, err := env.reconcile(t, nn)
		require.NoError(t, err)
		assert.Empty(t, env.fake.Calls(), "no Konnect calls without a Secret")

		value, ok := env.fake.Value(testConfigStoreSyncCPID, testConfigStoreSyncStoreID, derivedKey)
		require.True(t, ok, "previous value keeps serving")
		assert.Equal(t, "previous", value)

		got := env.getSync(t, nn)
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.SecretRefValidConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSecretRefReasonNotFound)
		events := env.drainEvents()
		assert.True(t, eventsContain(events, "SecretRefNoLongerExists"), "expected event, got %v", events)
	})

	t.Run("cross-namespace secret without grant is not allowed", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		secret.Namespace = "other"
		sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())
		sync.Spec.SecretRef = commonv1alpha1.NamespacedRef{Name: "tls-secret", Namespace: new("other")}
		objs := append(configStoreSyncBaseObjects(), secret, sync)
		env := newConfigStoreSyncTestEnv(t, objs...)

		_, err := env.reconcile(t, configStoreSyncNN("sync"))
		require.NoError(t, err)
		assert.Empty(t, env.fake.Calls())
		got := env.getSync(t, configStoreSyncNN("sync"))
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.SecretRefValidConditionType,
			metav1.ConditionFalse, konnectv1alpha1.KonnectConfigStoreSyncSecretRefReasonNotAllowed)
	})

	t.Run("cross-namespace secret with grant proceeds", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		secret.Namespace = "other"
		sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())
		sync.Spec.SecretRef = commonv1alpha1.NamespacedRef{Name: "tls-secret", Namespace: new("other")}
		grant := &configurationv1alpha1.KongReferenceGrant{
			Name: "grant", Namespace: "other",
			Spec: configurationv1alpha1.KongReferenceGrantSpec{
				From: []configurationv1alpha1.ReferenceGrantFrom{
					{
						Group:     configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
						Kind:      "KonnectConfigStoreSync",
						Namespace: configurationv1alpha1.Namespace(testConfigStoreSyncNamespace),
					},
				},
				To: []configurationv1alpha1.ReferenceGrantTo{
					{
						Group: configurationv1alpha1.Group("core"),
						Kind:  "Secret",
					},
				},
			},
		}
		objs := append(configStoreSyncBaseObjects(), secret, sync, grant)
		env := newConfigStoreSyncTestEnv(t, objs...)

		_, err := env.reconcile(t, configStoreSyncNN("sync"))
		require.NoError(t, err)
		assert.NotEmpty(t, env.fake.MutatingCalls(), "grant permits the write")
	})

	t.Run("cross-namespace store without grant is refused", func(t *testing.T) {
		secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
		sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())
		sync.Spec.ConfigStoreRef = commonv1alpha1.NamespacedRef{Name: "config-store", Namespace: new("other")}
		store := newConfigStoreSyncTestConfigStore()
		store.Namespace = "other"
		env := newConfigStoreSyncTestEnv(t,
			newConfigStoreSyncTestAPIAuth(), newConfigStoreSyncTestControlPlane(), store, secret, sync)

		_, err := env.reconcile(t, configStoreSyncNN("sync"))
		require.NoError(t, err)
		assert.Empty(t, env.fake.Calls())
		got := env.getSync(t, configStoreSyncNN("sync"))
		requireConfigStoreSyncCondition(t, got, konnectv1alpha1.ConfigStoreRefValidConditionType,
			metav1.ConditionFalse, konnectv1alpha1.ConfigStoreRefReasonRefNotPermitted)
	})
}

func TestObservedUpdatedAt(t *testing.T) {
	observed := observedUpdatedAt(time.Time{})
	assert.Nil(t, observed)
	write, reason := configStoreSecretWriteDecision(true, time.Time{}, observed, "sha256:same", "sha256:same")
	assert.False(t, write, "a missing updated_at must not cause a steady-state rewrite")
	assert.Empty(t, reason)

	now := time.Now()
	observed = observedUpdatedAt(now)
	require.NotNil(t, observed)
	assert.Equal(t, now, observed.Time)
}

func TestKonnectConfigStoreSyncWatchMappers(t *testing.T) {
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat()
	secret := newConfigStoreSyncTestSecret(certPEM, keyPEM)
	sync := newConfigStoreSync("sync", withConfigStoreSyncFinalizer())
	other := newConfigStoreSync("other", withConfigStoreSyncFinalizer())
	other.Spec.SecretRef = commonv1alpha1.NamespacedRef{Name: "other-secret"}
	objs := append(configStoreSyncBaseObjects(), secret, sync, other)
	env := newConfigStoreSyncTestEnv(t, objs...)
	ctx := context.Background()

	t.Run("secret mapper enqueues referencing syncs only", func(t *testing.T) {
		reqs := env.reconciler.listSyncsForSecret(ctx, secret)
		assert.Len(t, reqs, 1)
		assert.Equal(t, configStoreSyncNN("sync"), reqs[0].NamespacedName)
	})

	t.Run("store mapper enqueues referencing syncs", func(t *testing.T) {
		reqs := env.reconciler.listSyncsForConfigStore(ctx, newConfigStoreSyncTestConfigStore())
		assert.Len(t, reqs, 2, "both syncs reference the store")
	})

	t.Run("grant mapper enqueues syncs referencing the grant namespace", func(t *testing.T) {
		grant := &configurationv1alpha1.KongReferenceGrant{
			Name: "grant", Namespace: testConfigStoreSyncNamespace,
		}
		reqs := env.reconciler.listSyncsForReferenceGrant(ctx, grant)
		assert.Len(t, reqs, 2, "both syncs reference objects in the grant namespace")

		grantOtherNS := &configurationv1alpha1.KongReferenceGrant{
			Name: "grant", Namespace: "unrelated",
		}
		reqs = env.reconciler.listSyncsForReferenceGrant(ctx, grantOtherNS)
		assert.Empty(t, reqs)
	})
}
