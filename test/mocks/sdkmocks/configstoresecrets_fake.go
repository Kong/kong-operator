package sdkmocks

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"

	"github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/konnect/server"
)

// This file provides a hand-maintained, stateful fake of the Konnect Config
// Store Secrets API. Unlike the generated testify mock
// (mocks.MockConfigStoreSecretsSDK), which returns whatever it is programmed
// to return, this fake reproduces the REAL API semantics observed against
// Konnect so that controller tests exercise realistic behavior:
//
//   - Reads (Get/List) are metadata-only: secret values are never returned.
//   - Create on an existing key fails with 409 Conflict.
//   - Update is an upsert: it returns 201 for a new key and 200 otherwise.
//   - Keys are capped at 512 bytes, values at 5120 bytes (400 Bad Request).
//   - There is NO key-character validation on create: keys containing '#',
//     '%' or '/' are accepted, but subsequent reads/updates/deletes of such
//     keys fail ('#' -> 404, '%' -> 500, '/' -> 404) because of how the
//     Konnect API routes/escapes those characters.
//   - updated_at advances on EVERY write, even an identical-value write.
//   - Timestamps are derived from an internal monotonic counter so tests do
//     not depend on wall-clock resolution.

const (
	// FakeConfigStoreSecretMaxKeyBytes is the Konnect Config Store key size cap.
	FakeConfigStoreSecretMaxKeyBytes = 512
	// FakeConfigStoreSecretMaxValueBytes is the Konnect Config Store value size cap.
	FakeConfigStoreSecretMaxValueBytes = 5120
)

// ConfigStoreSecretsCall records one invocation of the fake SDK.
type ConfigStoreSecretsCall struct {
	// Method is one of "Create", "List", "Get", "Update", "Delete".
	Method         string
	ControlPlaneID string
	ConfigStoreID  string
	// Key is empty for List.
	Key string
}

// IsMutating reports whether the call modifies store state.
func (c ConfigStoreSecretsCall) IsMutating() bool {
	switch c.Method {
	case "Create", "Update", "Delete":
		return true
	default:
		return false
	}
}

type fakeConfigStoreSecretEntry struct {
	value     string
	createdAt time.Time
	updatedAt time.Time
}

// FakeConfigStoreSecrets is a stateful fake implementing
// sdkkonnectgo.ConfigStoreSecretsSDK with real Konnect API semantics.
type FakeConfigStoreSecrets struct {
	mu sync.Mutex
	// stores maps "<controlPlaneID>/<configStoreID>" -> key -> entry.
	stores map[string]map[string]*fakeConfigStoreSecretEntry
	// clock is a monotonically increasing counter used to derive
	// created_at/updated_at so updated_at advances on every write.
	clock int64
	// calls records every SDK method invocation in order.
	calls []ConfigStoreSecretsCall
	// ErrorHook, when non-nil, is consulted by every mutating call
	// (Create/Update/Delete) after the call is logged and before any state
	// change; a non-nil return fails the call with that error. It lets
	// controller tests inject API failures (e.g. a failed second write in
	// Split mode). The hook runs under the fake's mutex: it must not call
	// any FakeConfigStoreSecrets method, or the test deadlocks. Reads are
	// not hooked: use key trap characters for those.
	ErrorHook func(method, key string) error
}

// var _ sdkkonnectgo.ConfigStoreSecretsSDK = &FakeConfigStoreSecrets{}

// NewFakeConfigStoreSecrets returns an empty FakeConfigStoreSecrets.
func NewFakeConfigStoreSecrets() *FakeConfigStoreSecrets {
	return &FakeConfigStoreSecrets{
		stores: map[string]map[string]*fakeConfigStoreSecretEntry{},
	}
}

func fakeConfigStoreID(controlPlaneID, configStoreID string) string {
	return controlPlaneID + "/" + configStoreID
}

func (f *FakeConfigStoreSecrets) store(controlPlaneID, configStoreID string) map[string]*fakeConfigStoreSecretEntry {
	id := fakeConfigStoreID(controlPlaneID, configStoreID)
	s, ok := f.stores[id]
	if !ok {
		s = map[string]*fakeConfigStoreSecretEntry{}
		f.stores[id] = s
	}
	return s
}

// nextTimestamp advances the internal clock and returns a timestamp derived
// from it, guaranteeing strictly increasing timestamps across writes.
func (f *FakeConfigStoreSecrets) nextTimestamp() time.Time {
	f.clock++
	return time.Unix(1_700_000_000+f.clock, 0).UTC()
}

func newFakeSDKError(statusCode int, format string, args ...any) *sdkkonnecterrs.SDKError {
	msg := fmt.Sprintf(format, args...)
	return sdkkonnecterrs.NewSDKError(msg, statusCode, msg, nil)
}

// keyReadError reproduces the real API's lack of key-character validation:
// keys with trap characters are accepted on create but fail on later access.
func keyReadError(key string) error {
	switch {
	case strings.Contains(key, "%"):
		return newFakeSDKError(http.StatusInternalServerError, "failed to read secret key %q", key)
	case strings.ContainsAny(key, "#/"):
		return newFakeSDKError(http.StatusNotFound, "secret %q not found", key)
	default:
		return nil
	}
}

func validateKeyValueCaps(key, value string) error {
	if len(key) > FakeConfigStoreSecretMaxKeyBytes {
		return newFakeSDKError(http.StatusBadRequest,
			"key length %d exceeds maximum of %d bytes", len(key), FakeConfigStoreSecretMaxKeyBytes)
	}
	if len(value) > FakeConfigStoreSecretMaxValueBytes {
		return newFakeSDKError(http.StatusBadRequest,
			"value length %d exceeds maximum of %d bytes", len(value), FakeConfigStoreSecretMaxValueBytes)
	}
	return nil
}

// CreateConfigStoreSecret creates a secret. It fails with 409 when the key
// already exists and with 400 when key/value exceed the size caps. No
// key-character validation is performed, matching the real API.
func (f *FakeConfigStoreSecrets) CreateConfigStoreSecret(
	_ context.Context,
	request sdkkonnectops.CreateConfigStoreSecretRequest,
	_ ...sdkkonnectops.Option,
) (*sdkkonnectops.CreateConfigStoreSecretResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := request.CreateConfigStoreSecret.Key
	value := request.CreateConfigStoreSecret.Value
	f.calls = append(f.calls, ConfigStoreSecretsCall{
		Method: "Create", ControlPlaneID: request.ControlPlaneID,
		ConfigStoreID: request.ConfigStoreID, Key: key,
	})

	if f.ErrorHook != nil {
		if err := f.ErrorHook("Create", key); err != nil {
			return nil, err
		}
	}
	if err := validateKeyValueCaps(key, value); err != nil {
		return nil, err
	}
	store := f.store(request.ControlPlaneID, request.ConfigStoreID)
	if _, exists := store[key]; exists {
		return nil, newFakeSDKError(http.StatusConflict, "secret with key %q already exists", key)
	}

	now := f.nextTimestamp()
	store[key] = &fakeConfigStoreSecretEntry{value: value, createdAt: now, updatedAt: now}
	return &sdkkonnectops.CreateConfigStoreSecretResponse{
		StatusCode: http.StatusCreated,
		ConfigStoreSecret: &sdkkonnectcomp.ConfigStoreSecret{
			Key:       new(key),
			CreatedAt: new(now),
			UpdatedAt: new(now),
		},
	}, nil
}

// ListConfigStoreSecrets lists secret metadata. Values are never returned.
// Pagination is not simulated: PageSize and PageAfter are ignored and every
// call returns the whole store.
func (f *FakeConfigStoreSecrets) ListConfigStoreSecrets(
	_ context.Context,
	request sdkkonnectops.ListConfigStoreSecretsRequest,
	_ ...sdkkonnectops.Option,
) (*sdkkonnectops.ListConfigStoreSecretsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, ConfigStoreSecretsCall{
		Method: "List", ControlPlaneID: request.ControlPlaneID,
		ConfigStoreID: request.ConfigStoreID,
	})

	store := f.store(request.ControlPlaneID, request.ConfigStoreID)
	data := make([]sdkkonnectcomp.ConfigStoreSecret, 0, len(store))
	keys := make([]string, 0, len(store))
	for key := range store {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		entry := store[key]
		k := key
		created, updated := entry.createdAt, entry.updatedAt
		data = append(data, sdkkonnectcomp.ConfigStoreSecret{
			Key:       &k,
			CreatedAt: &created,
			UpdatedAt: &updated,
		})
	}
	return &sdkkonnectops.ListConfigStoreSecretsResponse{
		StatusCode: http.StatusOK,
		ListConfigStoreSecretsResponse: &sdkkonnectcomp.ListConfigStoreSecretsResponse{
			Data: data,
		},
	}, nil
}

// GetConfigStoreSecret returns secret metadata. Values are never returned.
func (f *FakeConfigStoreSecrets) GetConfigStoreSecret(
	_ context.Context,
	request sdkkonnectops.GetConfigStoreSecretRequest,
	_ ...sdkkonnectops.Option,
) (*sdkkonnectops.GetConfigStoreSecretResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, ConfigStoreSecretsCall{
		Method: "Get", ControlPlaneID: request.ControlPlaneID,
		ConfigStoreID: request.ConfigStoreID, Key: request.Key,
	})

	if err := keyReadError(request.Key); err != nil {
		return nil, err
	}
	entry, exists := f.store(request.ControlPlaneID, request.ConfigStoreID)[request.Key]
	if !exists {
		return nil, newFakeSDKError(http.StatusNotFound, "secret %q not found", request.Key)
	}
	key := request.Key
	return &sdkkonnectops.GetConfigStoreSecretResponse{
		StatusCode: http.StatusOK,
		ConfigStoreSecret: &sdkkonnectcomp.ConfigStoreSecret{
			Key:       &key,
			CreatedAt: new(entry.createdAt),
			UpdatedAt: new(entry.updatedAt),
		},
	}, nil
}

// UpdateConfigStoreSecret upserts a secret's value. It returns 201 when the
// key is created and 200 when it is updated. updated_at advances even when the
// new value is identical to the stored one, matching the real API.
func (f *FakeConfigStoreSecrets) UpdateConfigStoreSecret(
	_ context.Context,
	request sdkkonnectops.UpdateConfigStoreSecretRequest,
	_ ...sdkkonnectops.Option,
) (*sdkkonnectops.UpdateConfigStoreSecretResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, ConfigStoreSecretsCall{
		Method: "Update", ControlPlaneID: request.ControlPlaneID,
		ConfigStoreID: request.ConfigStoreID, Key: request.Key,
	})

	if f.ErrorHook != nil {
		if err := f.ErrorHook("Update", request.Key); err != nil {
			return nil, err
		}
	}
	if err := keyReadError(request.Key); err != nil {
		return nil, err
	}
	if err := validateKeyValueCaps(request.Key, request.UpdateConfigStoreSecret.Value); err != nil {
		return nil, err
	}
	store := f.store(request.ControlPlaneID, request.ConfigStoreID)
	entry, exists := store[request.Key]
	if !exists {
		now := f.nextTimestamp()
		store[request.Key] = &fakeConfigStoreSecretEntry{
			value:     request.UpdateConfigStoreSecret.Value,
			createdAt: now,
			updatedAt: now,
		}
		key := request.Key
		return &sdkkonnectops.UpdateConfigStoreSecretResponse{
			StatusCode: http.StatusCreated,
			ConfigStoreSecret: &sdkkonnectcomp.ConfigStoreSecret{
				Key:       &key,
				CreatedAt: new(now),
				UpdatedAt: new(now),
			},
		}, nil
	}

	entry.value = request.UpdateConfigStoreSecret.Value
	entry.updatedAt = f.nextTimestamp()
	key := request.Key
	return &sdkkonnectops.UpdateConfigStoreSecretResponse{
		StatusCode: http.StatusOK,
		ConfigStoreSecret: &sdkkonnectcomp.ConfigStoreSecret{
			Key:       &key,
			CreatedAt: new(entry.createdAt),
			UpdatedAt: new(entry.updatedAt),
		},
	}, nil
}

// DeleteConfigStoreSecret deletes a secret. Missing keys fail with 404.
func (f *FakeConfigStoreSecrets) DeleteConfigStoreSecret(
	_ context.Context,
	request sdkkonnectops.DeleteConfigStoreSecretRequest,
	_ ...sdkkonnectops.Option,
) (*sdkkonnectops.DeleteConfigStoreSecretResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, ConfigStoreSecretsCall{
		Method: "Delete", ControlPlaneID: request.ControlPlaneID,
		ConfigStoreID: request.ConfigStoreID, Key: request.Key,
	})

	if f.ErrorHook != nil {
		if err := f.ErrorHook("Delete", request.Key); err != nil {
			return nil, err
		}
	}
	if err := keyReadError(request.Key); err != nil {
		return nil, err
	}
	store := f.store(request.ControlPlaneID, request.ConfigStoreID)
	if _, exists := store[request.Key]; !exists {
		return nil, newFakeSDKError(http.StatusNotFound, "secret %q not found", request.Key)
	}
	delete(store, request.Key)
	return &sdkkonnectops.DeleteConfigStoreSecretResponse{
		StatusCode: http.StatusNoContent,
	}, nil
}

// -----------------------------------------------------------------------------
// Test helpers (not part of the SDK interface)
// -----------------------------------------------------------------------------

// Calls returns a copy of the recorded call log.
func (f *FakeConfigStoreSecrets) Calls() []ConfigStoreSecretsCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ConfigStoreSecretsCall(nil), f.calls...)
}

// MutatingCalls returns the recorded calls that modify store state
// (Create/Update/Delete). Steady-state reconciles must produce none.
func (f *FakeConfigStoreSecrets) MutatingCalls() []ConfigStoreSecretsCall {
	var out []ConfigStoreSecretsCall
	for _, c := range f.Calls() {
		if c.IsMutating() {
			out = append(out, c)
		}
	}
	return out
}

// ResetCalls clears the recorded call log.
func (f *FakeConfigStoreSecrets) ResetCalls() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

// Value returns the stored value for a key, for test assertions. The SDK
// itself never returns values; this is a test-only backdoor.
func (f *FakeConfigStoreSecrets) Value(controlPlaneID, configStoreID, key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.store(controlPlaneID, configStoreID)[key]
	if !ok {
		return "", false
	}
	return entry.value, true
}

// SetValue writes a value directly, bypassing the SDK interface, and advances
// updated_at. It simulates an out-of-band write (drift) by another actor.
func (f *FakeConfigStoreSecrets) SetValue(controlPlaneID, configStoreID, key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	store := f.store(controlPlaneID, configStoreID)
	now := f.nextTimestamp()
	if entry, ok := store[key]; ok {
		entry.value = value
		entry.updatedAt = now
		return
	}
	store[key] = &fakeConfigStoreSecretEntry{value: value, createdAt: now, updatedAt: now}
}

// DeleteValue removes a key directly, bypassing the SDK interface. It
// simulates an out-of-band deletion (entry recreated on next sync).
func (f *FakeConfigStoreSecrets) DeleteValue(controlPlaneID, configStoreID, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.store(controlPlaneID, configStoreID), key)
}

// StoreEmpty reports whether the given store holds no entries.
func (f *FakeConfigStoreSecrets) StoreEmpty(controlPlaneID, configStoreID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.store(controlPlaneID, configStoreID)) == 0
}

// -----------------------------------------------------------------------------
// SDK factory/wrapper fakes serving FakeConfigStoreSecrets
// -----------------------------------------------------------------------------

type fakeConfigStoreSecretsSDKWrapper struct {
	sdk.SDKWrapper // embedded nil interface; only the overridden method is usable

	secrets sdkkonnectgo.ConfigStoreSecretsSDK
}

// GetConfigStoreSecretsSDK returns the fake Config Store Secrets SDK.
func (w fakeConfigStoreSecretsSDKWrapper) GetConfigStoreSecretsSDK() sdkkonnectgo.ConfigStoreSecretsSDK {
	return w.secrets
}

type fakeConfigStoreSecretsSDKFactory struct {
	wrapper sdk.SDKWrapper
}

// NewKonnectSDK returns the wrapper holding the fake, ignoring credentials.
func (f fakeConfigStoreSecretsSDKFactory) NewKonnectSDK(server.Server, sdk.SDKToken) sdk.SDKWrapper {
	return f.wrapper
}

// NewFakeConfigStoreSecretsSDKFactory returns an sdk.SDKFactory whose
// SDKWrapper serves the given ConfigStoreSecretsSDK (typically a
// FakeConfigStoreSecrets). All other sub-clients are nil and must not be
// called by the code under test.
func NewFakeConfigStoreSecretsSDKFactory(secrets sdkkonnectgo.ConfigStoreSecretsSDK) sdk.SDKFactory {
	return fakeConfigStoreSecretsSDKFactory{
		wrapper: fakeConfigStoreSecretsSDKWrapper{secrets: secrets},
	}
}
