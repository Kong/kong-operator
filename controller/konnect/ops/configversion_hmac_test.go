package ops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

// testSyncHashKeyMaterial returns deterministic key material: fill repeated
// SyncHashKeyMinLength times.
func testSyncHashKeyMaterial(fill byte) []byte {
	return bytes.Repeat([]byte{fill}, SyncHashKeyMinLength)
}

// testSyncHashKey returns the key used by the keyed golden hash tests.
func testSyncHashKey(t *testing.T) SyncHashKey {
	t.Helper()
	b := make([]byte, SyncHashKeyMinLength)
	for i := range b {
		b[i] = byte(i)
	}
	k, err := NewSyncHashKey(b)
	require.NoError(t, err)
	return k
}

func newTestSyncHashKey(t *testing.T, fill byte) SyncHashKey {
	t.Helper()
	k, err := NewSyncHashKey(testSyncHashKeyMaterial(fill))
	require.NoError(t, err)
	return k
}

func TestNewSyncHashKey(t *testing.T) {
	t.Run("short key is rejected", func(t *testing.T) {
		_, err := NewSyncHashKey(make([]byte, SyncHashKeyMinLength-1))
		require.Error(t, err)
	})

	t.Run("key ID is a stable fingerprint", func(t *testing.T) {
		k := testSyncHashKey(t)
		assert.Regexp(t, `^[0-9a-f]{16}$`, k.ID())
		assert.Equal(t, "19ba7ac4031acc7c", k.ID(), "key ID derivation changed: persisted keyIDs would no longer match")

		other := newTestSyncHashKey(t, 0xff)
		assert.NotEqual(t, k.ID(), other.ID())
	})

	t.Run("key material is copied", func(t *testing.T) {
		b := testSyncHashKeyMaterial(1)
		k, err := NewSyncHashKey(b)
		require.NoError(t, err)
		id := k.ID()
		h := syncHasher{key: &k}.sum([]byte("payload"))

		b[0] = 2
		assert.Equal(t, h, syncHasher{key: &k}.sum([]byte("payload")))
		assert.Equal(t, id, k.ID())
	})

	t.Run("generated key material is usable and random", func(t *testing.T) {
		a, err := GenerateSyncHashKeyMaterial()
		require.NoError(t, err)
		b, err := GenerateSyncHashKeyMaterial()
		require.NoError(t, err)
		assert.Len(t, a, SyncHashKeyMinLength)
		assert.NotEqual(t, a, b)
		_, err = NewSyncHashKey(a)
		require.NoError(t, err)
	})
}

func TestSyncHasher(t *testing.T) {
	data := []byte(`{"key":"secret"}`)

	unkeyed := syncHasher{}
	assert.False(t, unkeyed.keyed())
	assert.Empty(t, unkeyed.keyID())
	assert.Equal(t, hash(sha256.Sum256(data)), unkeyed.sum(data))

	k1, k2 := newTestSyncHashKey(t, 1), newTestSyncHashKey(t, 2)
	keyed1, keyed2 := syncHasher{key: &k1}, syncHasher{key: &k2}
	assert.True(t, keyed1.keyed())
	assert.Equal(t, k1.ID(), keyed1.keyID())
	assert.NotEqual(t, unkeyed.sum(data), keyed1.sum(data), "keyed digest must not be the bare SHA-256")
	assert.NotEqual(t, keyed1.sum(data), keyed2.sum(data), "digests must depend on the key")
	first, second := keyed1.sum(data), keyed1.sum(data)
	assert.Equal(t, first, second, "keyed digests must be deterministic")
}

func TestConfigVersionTrackerSetSyncHashKeys(t *testing.T) {
	var nilTracker *ConfigVersionTracker
	nilTracker.SetSyncHashKeys(SyncHashKeys{})

	tracker, _ := newTestConfigVersionTracker()
	k := newTestSyncHashKey(t, 1)
	// A previous key equal to the current one is ignored.
	tracker.SetSyncHashKeys(SyncHashKeys{Current: k, Previous: &k})
	keys := tracker.syncHashKeys()
	require.NotNil(t, keys.current)
	assert.Equal(t, k.ID(), keys.current.ID())
	assert.Nil(t, keys.previous)
}

// secretBearingSync runs updates of a test entity guarded as secret-bearing
// against the fake services SDK, simulating the reconciler's status handling.
type secretBearingSync struct {
	t        *testing.T
	version  *fakeConfigVersion
	services *fakeKonnectServices
	svc      *configurationv1alpha1.KongService
}

func newSecretBearingSync(t *testing.T) *secretBearingSync {
	return &secretBearingSync{
		t:        t,
		version:  newFakeConfigVersion(t),
		services: newFakeKonnectServices(t),
		svc:      newTestKongService(),
	}
}

func newKeyedTracker(t *testing.T, current *SyncHashKey, previous *SyncHashKey) *ConfigVersionTracker {
	t.Helper()
	tracker, _ := newTestConfigVersionTracker()
	if current != nil {
		tracker.SetSyncHashKeys(SyncHashKeys{Current: *current, Previous: previous})
	}
	return tracker
}

// update runs a periodic update and, like the reconciler, persists the pending
// status and commits the pending record. It returns the guard and the number
// of GET and upsert requests.
func (s *secretBearingSync) update(tracker *ConfigVersionTracker) (*syncGuard, int, int) {
	s.t.Helper()
	g := tracker.guard(s.version.sdk, s.svc.GetUID(), testCPID, true, s.svc.GetSyncStatus()).withSecretBearingPayload()
	require.NoError(s.t, updateService(s.t.Context(), s.services.sdk, g, s.svc))
	if g.pendingStatusWrite() {
		s.svc.SetSyncStatus(g.pendingStatus())
		if rec, ok := g.pendingRecord(); ok {
			tracker.store(s.svc.GetUID(), rec)
		}
	}
	gets, upserts := s.services.calls()
	return g, gets, upserts
}

func TestSyncGuardSecretBearingPersistsKeyedHashes(t *testing.T) {
	key := newTestSyncHashKey(t, 1)
	s := newSecretBearingSync(t)

	g, _, upserts := s.update(newKeyedTracker(t, &key, nil))
	require.Equal(t, syncOutcomeSent, g.outcome)
	require.Equal(t, 1, upserts)

	status := s.svc.GetSyncStatus()
	require.NotNil(t, status)
	assert.Regexp(t, `^hmac-sha256:[0-9a-f]{64}$`, status.PayloadHash)
	assert.Regexp(t, `^hmac-sha256:[0-9a-f]{64}$`, status.RepresentationHash)
	assert.Equal(t, key.ID(), status.KeyID)
	assert.Equal(t, SyncHashVersion, status.HashVersion)

	unkeyed, err := entityPayloadHash(syncHasher{}, testCPID, s.svc.GetKonnectStatus().GetKonnectID(), "x")
	require.NoError(t, err)
	assert.NotEqual(t, unkeyed.String(), status.PayloadHash)

	t.Run("restart with the same key verifies with a GET and does not PUT", func(t *testing.T) {
		before := *s.svc.GetSyncStatus()
		g, gets, upserts := s.update(newKeyedTracker(t, &key, nil))
		assert.Equal(t, syncOutcomeVerified, g.outcome)
		assert.False(t, g.pendingStatusWrite(), "a verified GET must not write status")
		assert.Equal(t, 1, gets)
		assert.Equal(t, 0, upserts)
		assert.Equal(t, before, *s.svc.GetSyncStatus())
	})
}

func TestSyncGuardSecretBearingKeyRotation(t *testing.T) {
	oldKey, newKey := newTestSyncHashKey(t, 1), newTestSyncHashKey(t, 2)

	t.Run("previous key: verify with a GET, rewrite hashes, no PUT", func(t *testing.T) {
		s := newSecretBearingSync(t)
		_, _, upserts := s.update(newKeyedTracker(t, &oldKey, nil))
		require.Equal(t, 1, upserts)
		require.Equal(t, oldKey.ID(), s.svc.GetSyncStatus().KeyID)

		rotated := newKeyedTracker(t, &newKey, &oldKey)
		g, gets, upserts := s.update(rotated)
		assert.Equal(t, syncOutcomeVerified, g.outcome)
		assert.True(t, g.pendingStatusWrite(), "a verification with the previous key must rewrite status")
		assert.Equal(t, 1, gets)
		assert.Equal(t, 0, upserts)
		status := s.svc.GetSyncStatus()
		require.NotNil(t, status)
		assert.Equal(t, newKey.ID(), status.KeyID)
		assert.Regexp(t, `^hmac-sha256:[0-9a-f]{64}$`, status.PayloadHash)

		// The rewritten record is the fast path: the next periodic update skips.
		g, gets, upserts = s.update(rotated)
		assert.Equal(t, syncOutcomeSkipped, g.outcome)
		assert.Equal(t, 0, gets)
		assert.Equal(t, 0, upserts)

		// Once the previous key is removed, the rewritten status still verifies.
		g, gets, upserts = s.update(newKeyedTracker(t, &newKey, nil))
		assert.Equal(t, syncOutcomeVerified, g.outcome)
		assert.False(t, g.pendingStatusWrite())
		assert.Equal(t, 1, gets)
		assert.Equal(t, 0, upserts)
	})

	t.Run("previous key: drift in Konnect is sent", func(t *testing.T) {
		s := newSecretBearingSync(t)
		s.update(newKeyedTracker(t, &oldKey, nil))
		s.services.stored.Host = "changed.example.com"

		g, gets, upserts := s.update(newKeyedTracker(t, &newKey, &oldKey))
		assert.Equal(t, syncOutcomeSent, g.outcome)
		assert.Equal(t, 1, gets)
		assert.Equal(t, 1, upserts)
		assert.Equal(t, newKey.ID(), s.svc.GetSyncStatus().KeyID)
	})

	t.Run("previous key: changed payload is sent without a GET", func(t *testing.T) {
		s := newSecretBearingSync(t)
		s.update(newKeyedTracker(t, &oldKey, nil))
		s.svc.Spec.Host = "changed.example.com"

		g, gets, upserts := s.update(newKeyedTracker(t, &newKey, &oldKey))
		assert.Equal(t, syncOutcomeSent, g.outcome)
		assert.Equal(t, 0, gets)
		assert.Equal(t, 1, upserts)
		assert.Equal(t, newKey.ID(), s.svc.GetSyncStatus().KeyID)
	})

	t.Run("unknown key: one-time re-sync without comparing hashes", func(t *testing.T) {
		s := newSecretBearingSync(t)
		s.update(newKeyedTracker(t, &oldKey, nil))

		g, gets, upserts := s.update(newKeyedTracker(t, &newKey, nil))
		assert.Equal(t, syncOutcomeSent, g.outcome)
		assert.Equal(t, 0, gets)
		assert.Equal(t, 1, upserts)
		assert.Equal(t, newKey.ID(), s.svc.GetSyncStatus().KeyID)
	})
}

func TestSyncGuardSecretBearingWithoutKey(t *testing.T) {
	key := newTestSyncHashKey(t, 1)
	s := newSecretBearingSync(t)
	s.update(newKeyedTracker(t, &key, nil))
	require.NotNil(t, s.svc.GetSyncStatus())

	// Without a key, the PUT clears status.sync instead of persisting unkeyed
	// hashes of secret material, and the stale keyed hashes are not trusted.
	tracker := newKeyedTracker(t, nil, nil)
	g, gets, upserts := s.update(tracker)
	assert.Equal(t, syncOutcomeSent, g.outcome)
	assert.Equal(t, 0, gets)
	assert.Equal(t, 1, upserts)
	assert.True(t, g.pendingStatusWrite())
	assert.Nil(t, g.pendingStatus())
	assert.Nil(t, s.svc.GetSyncStatus(), "unkeyed hashes of a secret-bearing entity must not be persisted")

	// The in-memory record still avoids periodic PUTs.
	g, gets, upserts = s.update(tracker)
	assert.Equal(t, syncOutcomeSkipped, g.outcome)
	assert.Equal(t, 0, gets)
	assert.Equal(t, 0, upserts)
}

func TestSyncGuardRejectsMismatchedPersistedHashKind(t *testing.T) {
	key := newTestSyncHashKey(t, 1)

	testCases := []struct {
		name          string
		secretBearing bool
		mutate        func(t *testing.T, s *configurationv1alpha1.SyncStatus)
	}{
		{
			name:          "secret-bearing entity with unkeyed hashes",
			secretBearing: true,
			mutate: func(_ *testing.T, s *configurationv1alpha1.SyncStatus) {
				s.PayloadHash = "sha256:" + s.PayloadHash[len(hmacHashPrefix):]
				s.RepresentationHash = "sha256:" + s.RepresentationHash[len(hmacHashPrefix):]
				s.KeyID = ""
			},
		},
		{
			name:          "secret-bearing entity without key ID",
			secretBearing: true,
			mutate: func(_ *testing.T, s *configurationv1alpha1.SyncStatus) {
				s.KeyID = ""
			},
		},
		{
			name:          "secret-bearing entity with mixed hash kinds",
			secretBearing: true,
			mutate: func(_ *testing.T, s *configurationv1alpha1.SyncStatus) {
				s.RepresentationHash = "sha256:" + s.RepresentationHash[len(hmacHashPrefix):]
			},
		},
		{
			name:          "secret-bearing entity with another key ID",
			secretBearing: true,
			mutate: func(_ *testing.T, s *configurationv1alpha1.SyncStatus) {
				s.KeyID = "0123456789abcdef"
			},
		},
		{
			name: "unkeyed entity with keyed hashes",
			mutate: func(_ *testing.T, s *configurationv1alpha1.SyncStatus) {
				s.PayloadHash = hmacHashPrefix + s.PayloadHash[len(hashPrefix):]
				s.RepresentationHash = hmacHashPrefix + s.RepresentationHash[len(hashPrefix):]
				s.KeyID = key.ID()
			},
		},
		{
			name: "unkeyed entity with a key ID",
			mutate: func(_ *testing.T, s *configurationv1alpha1.SyncStatus) {
				s.KeyID = key.ID()
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			version := newFakeConfigVersion(t)
			services := newFakeKonnectServices(t)
			svc := newTestKongService()
			newGuard := func(tracker *ConfigVersionTracker) *syncGuard {
				g := tracker.guard(version.sdk, svc.GetUID(), testCPID, true, svc.GetSyncStatus())
				if tc.secretBearing {
					g = g.withSecretBearingPayload()
				}
				return g
			}

			g := newGuard(newKeyedTracker(t, &key, nil))
			require.NoError(t, updateService(t.Context(), services.sdk, g, svc))
			status := g.pendingStatus()
			require.NotNil(t, status)
			tc.mutate(t, status)
			svc.SetSyncStatus(status)
			services.calls()

			g = newGuard(newKeyedTracker(t, &key, nil))
			require.NoError(t, updateService(t.Context(), services.sdk, g, svc))
			assert.Equal(t, syncOutcomeSent, g.outcome)
			gets, upserts := services.calls()
			assert.Equal(t, 0, gets, "hashes of another kind must never be compared")
			assert.Equal(t, 1, upserts)
		})
	}
}

func TestSyncGuardUnkeyedEntityIgnoresSyncHashKeys(t *testing.T) {
	key := newTestSyncHashKey(t, 1)
	tracker := newKeyedTracker(t, &key, nil)
	version := newFakeConfigVersion(t)
	services := newFakeKonnectServices(t)
	svc := newTestKongService()

	g := tracker.guard(version.sdk, svc.GetUID(), testCPID, true, nil)
	require.NoError(t, updateService(t.Context(), services.sdk, g, svc))
	status := g.pendingStatus()
	require.NotNil(t, status)
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, status.PayloadHash)
	assert.Empty(t, status.KeyID)
}

type fakeKonnectAPIKeys struct {
	sdk     *mocks.MockAPIKeysSDK
	stored  *sdkkonnectcomp.KeyAuth
	gets    int
	upserts int
}

func newFakeKonnectAPIKeys(t *testing.T) *fakeKonnectAPIKeys {
	f := &fakeKonnectAPIKeys{sdk: mocks.NewMockAPIKeysSDK(t)}
	f.sdk.EXPECT().UpsertKeyAuthWithConsumer(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, req sdkkonnectops.UpsertKeyAuthWithConsumerRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpsertKeyAuthWithConsumerResponse, error) {
			f.upserts++
			f.stored = &sdkkonnectcomp.KeyAuth{
				ID:       new(req.KeyAuthID),
				Consumer: &sdkkonnectcomp.KeyAuthConsumer{ID: new(req.ConsumerIDForNestedEntities)},
				Key:      req.KeyAuthWithoutParents.Key,
				Tags:     req.KeyAuthWithoutParents.Tags,
			}
			return &sdkkonnectops.UpsertKeyAuthWithConsumerResponse{KeyAuth: new(*f.stored)}, nil
		},
	).Maybe()
	f.sdk.EXPECT().GetKeyAuthWithConsumer(mock.Anything, mock.Anything).RunAndReturn(
		func(context.Context, sdkkonnectops.GetKeyAuthWithConsumerRequest, ...sdkkonnectops.Option) (*sdkkonnectops.GetKeyAuthWithConsumerResponse, error) {
			f.gets++
			return &sdkkonnectops.GetKeyAuthWithConsumerResponse{KeyAuth: new(*f.stored)}, nil
		},
	).Maybe()
	return f
}

func (f *fakeKonnectAPIKeys) calls() (gets, upserts int) {
	gets, upserts = f.gets, f.upserts
	f.gets, f.upserts = 0, 0
	return gets, upserts
}

func newTestKongCredentialAPIKey() *configurationv1alpha1.KongCredentialAPIKey {
	cred := &configurationv1alpha1.KongCredentialAPIKey{
		Name:      "api-key",
		Namespace: "default",
		UID:       k8stypes.UID("api-key-uid"),
		Spec: configurationv1alpha1.KongCredentialAPIKeySpec{
			KongCredentialAPIKeyAPISpec: configurationv1alpha1.KongCredentialAPIKeyAPISpec{
				Key: "low-entropy-key",
			},
		},
		Status: configurationv1alpha1.KongCredentialAPIKeyStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndConsumerRefs{
				ControlPlaneID: testCPID,
				ConsumerID:     "consumer-konnect-id",
			},
		},
	}
	cred.SetKonnectID("api-key-konnect-id")
	cred.Generation = 1
	return cred
}

type fakeKonnectCertificates struct {
	sdk     *mocks.MockCertificatesSDK
	stored  *sdkkonnectcomp.Certificate
	gets    int
	upserts int
}

func newFakeKonnectCertificates(t *testing.T) *fakeKonnectCertificates {
	f := &fakeKonnectCertificates{sdk: mocks.NewMockCertificatesSDK(t)}
	f.sdk.EXPECT().UpsertCertificate(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, req sdkkonnectops.UpsertCertificateRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpsertCertificateResponse, error) {
			f.upserts++
			input := req.CertificateRequest.CertificateWithCertAndKey
			f.stored = &sdkkonnectcomp.Certificate{
				ID:   new(req.CertificateID),
				Cert: input.Cert,
				Key:  input.Key,
				Tags: input.Tags,
			}
			return &sdkkonnectops.UpsertCertificateResponse{Certificate: new(*f.stored)}, nil
		},
	).Maybe()
	f.sdk.EXPECT().GetCertificate(mock.Anything, mock.Anything).RunAndReturn(
		func(context.Context, sdkkonnectops.GetCertificateRequest, ...sdkkonnectops.Option) (*sdkkonnectops.GetCertificateResponse, error) {
			f.gets++
			return &sdkkonnectops.GetCertificateResponse{Certificate: new(*f.stored)}, nil
		},
	).Maybe()
	return f
}

func (f *fakeKonnectCertificates) calls() (gets, upserts int) {
	gets, upserts = f.gets, f.upserts
	f.gets, f.upserts = 0, 0
	return gets, upserts
}

// newTestKongCertificateFromSecret returns a KongCertificate whose private key
// is read from a Secret, so it is not readable from the KongCertificate itself.
func newTestKongCertificateFromSecret() (*configurationv1alpha1.KongCertificate, *corev1.Secret) {
	secret := &corev1.Secret{
		Name:      "tls",
		Namespace: "default",
		Data: map[string][]byte{
			"tls.crt": []byte("certificate"),
			"tls.key": []byte("private-key"),
		},
	}
	cert := &configurationv1alpha1.KongCertificate{
		Name:      "cert",
		Namespace: "default",
		UID:       k8stypes.UID("cert-uid"),
		Spec: configurationv1alpha1.KongCertificateSpec{
			Type:      new(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
			SecretRef: &commonv1alpha1.NamespacedRef{Name: secret.Name},
		},
		Status: configurationv1alpha1.KongCertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: testCPID,
			},
		},
	}
	cert.SetKonnectID("cert-konnect-id")
	cert.Generation = 1
	return cert, secret
}

func TestUpdateSecretBearingEntityPersistsKeyedSyncStatus(t *testing.T) {
	oldKey, newKey := newTestSyncHashKey(t, 1), newTestSyncHashKey(t, 2)
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	certs := newFakeKonnectCertificates(t)
	sdk.CertificatesSDK = certs.sdk
	version := newFakeConfigVersion(t)
	sdk.DPNodesSDK = version.sdk
	cert, secret := newTestKongCertificateFromSecret()
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(secret).Build()
	syncPeriod := time.Minute

	_, syncResult, err := Update(t.Context(), sdk, syncPeriod, cl, &metricsmocks.MockRecorder{}, cert, newKeyedTracker(t, &oldKey, nil))
	require.NoError(t, err)
	require.NotNil(t, syncResult)
	_, upserts := certs.calls()
	require.Equal(t, 1, upserts)
	status := cert.GetSyncStatus()
	require.NotNil(t, status)
	assert.Regexp(t, `^hmac-sha256:[0-9a-f]{64}$`, status.PayloadHash)
	assert.Regexp(t, `^hmac-sha256:[0-9a-f]{64}$`, status.RepresentationHash)
	assert.Equal(t, oldKey.ID(), status.KeyID)

	// After a key rotation and a restart, the periodic update verifies the
	// entity with the previous key, rewrites its status with the current key,
	// and returns the record to commit after the status patch.
	rotated := newKeyedTracker(t, &newKey, &oldKey)
	cert.Status.Conditions[0].LastTransitionTime = metav1.NewTime(time.Now().Add(-2 * syncPeriod).Truncate(time.Second))
	programmed := cert.Status.Conditions[0]
	_, syncResult, err = Update(t.Context(), sdk, syncPeriod, cl, &metricsmocks.MockRecorder{}, cert, rotated)
	require.NoError(t, err)
	require.NotNil(t, syncResult, "a status rewrite must return its record to commit")
	gets, upserts := certs.calls()
	assert.Equal(t, 1, gets)
	assert.Equal(t, 0, upserts)
	assert.Equal(t, newKey.ID(), cert.GetSyncStatus().KeyID)
	assert.Equal(t, programmed, cert.Status.Conditions[0], "a verification must not touch the Programmed condition")

	_, ok := rotated.record(cert.GetUID())
	assert.False(t, ok, "memory must not advance before the status patch succeeds")
	syncResult.Commit()
	_, ok = rotated.record(cert.GetUID())
	assert.True(t, ok)
}

// TestUpdateCredentialPersistsUnkeyedSyncStatus checks that a credential, whose
// secret is part of its own spec, keeps unkeyed hashes even with a sync hash key.
func TestUpdateCredentialPersistsUnkeyedSyncStatus(t *testing.T) {
	key := newTestSyncHashKey(t, 1)
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	apiKeys := newFakeKonnectAPIKeys(t)
	sdk.KongCredentialsAPIKeySDK = apiKeys.sdk
	version := newFakeConfigVersion(t)
	sdk.DPNodesSDK = version.sdk
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()
	cred := newTestKongCredentialAPIKey()

	_, syncResult, err := Update(t.Context(), sdk, time.Minute, cl, &metricsmocks.MockRecorder{}, cred, newKeyedTracker(t, &key, nil))
	require.NoError(t, err)
	require.NotNil(t, syncResult)
	_, upserts := apiKeys.calls()
	require.Equal(t, 1, upserts)
	status := cred.GetSyncStatus()
	require.NotNil(t, status)
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, status.PayloadHash)
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, status.RepresentationHash)
	assert.Empty(t, status.KeyID)
}
