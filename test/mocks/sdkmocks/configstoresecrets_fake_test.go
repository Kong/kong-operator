package sdkmocks

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fakeTestCPID    = "cp-123"
	fakeTestStoreID = "store-456"
)

func sdkErrorStatusCode(t *testing.T, err error) int {
	t.Helper()
	sdkErr := &sdkkonnecterrs.SDKError{}
	ok := errors.As(err, &sdkErr)
	require.True(t, ok, "expected *sdkerrors.SDKError, got %T (%v)", err, err)
	return sdkErr.StatusCode
}

func createSecret(t *testing.T, f *FakeConfigStoreSecrets, key, value string) {
	t.Helper()
	_, err := f.CreateConfigStoreSecret(context.Background(), sdkkonnectops.CreateConfigStoreSecretRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  fakeTestStoreID,
		CreateConfigStoreSecret: sdkkonnectcomp.CreateConfigStoreSecret{
			Key:   key,
			Value: value,
		},
	})
	require.NoError(t, err)
}

func TestFakeConfigStoreSecretsCreateAndMetadataOnlyReads(t *testing.T) {
	f := NewFakeConfigStoreSecrets()
	createSecret(t, f, "my-key", "super-secret-value")

	getResp, err := f.GetConfigStoreSecret(context.Background(), sdkkonnectops.GetConfigStoreSecretRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  fakeTestStoreID,
		Key:            "my-key",
	})
	require.NoError(t, err)
	require.NotNil(t, getResp.ConfigStoreSecret)
	assert.Equal(t, "my-key", *getResp.ConfigStoreSecret.Key)
	assert.NotNil(t, getResp.ConfigStoreSecret.CreatedAt)
	assert.NotNil(t, getResp.ConfigStoreSecret.UpdatedAt)
	// Metadata-only: the SDK type has no value field at all, and the fake
	// must not expose one through reads. Value() is the test-only backdoor.
	stored, ok := f.Value(fakeTestCPID, fakeTestStoreID, "my-key")
	require.True(t, ok)
	assert.Equal(t, "super-secret-value", stored)

	listResp, err := f.ListConfigStoreSecrets(context.Background(), sdkkonnectops.ListConfigStoreSecretsRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  fakeTestStoreID,
	})
	require.NoError(t, err)
	require.NotNil(t, listResp.ListConfigStoreSecretsResponse)
	require.Len(t, listResp.ListConfigStoreSecretsResponse.Data, 1)
	assert.Equal(t, "my-key", *listResp.ListConfigStoreSecretsResponse.Data[0].Key)
}

func TestFakeConfigStoreSecretsCreateDuplicateReturns409(t *testing.T) {
	f := NewFakeConfigStoreSecrets()
	createSecret(t, f, "dup-key", "value-1")

	_, err := f.CreateConfigStoreSecret(context.Background(), sdkkonnectops.CreateConfigStoreSecretRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  fakeTestStoreID,
		CreateConfigStoreSecret: sdkkonnectcomp.CreateConfigStoreSecret{
			Key:   "dup-key",
			Value: "value-2",
		},
	})
	require.Error(t, err)
	assert.Equal(t, http.StatusConflict, sdkErrorStatusCode(t, err))

	// The original value is untouched.
	stored, ok := f.Value(fakeTestCPID, fakeTestStoreID, "dup-key")
	require.True(t, ok)
	assert.Equal(t, "value-1", stored)
}

func TestFakeConfigStoreSecretsSizeCaps(t *testing.T) {
	f := NewFakeConfigStoreSecrets()

	// Key at the cap is accepted, one byte over is rejected with 400.
	createSecret(t, f, strings.Repeat("k", FakeConfigStoreSecretMaxKeyBytes), "v")
	_, err := f.CreateConfigStoreSecret(context.Background(), sdkkonnectops.CreateConfigStoreSecretRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  fakeTestStoreID,
		CreateConfigStoreSecret: sdkkonnectcomp.CreateConfigStoreSecret{
			Key:   strings.Repeat("k", FakeConfigStoreSecretMaxKeyBytes+1),
			Value: "v",
		},
	})
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, sdkErrorStatusCode(t, err))

	// Value at the cap is accepted, one byte over is rejected with 400.
	createSecret(t, f, "value-at-cap", strings.Repeat("v", FakeConfigStoreSecretMaxValueBytes))
	_, err = f.CreateConfigStoreSecret(context.Background(), sdkkonnectops.CreateConfigStoreSecretRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  fakeTestStoreID,
		CreateConfigStoreSecret: sdkkonnectcomp.CreateConfigStoreSecret{
			Key:   "value-over-cap",
			Value: strings.Repeat("v", FakeConfigStoreSecretMaxValueBytes+1),
		},
	})
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, sdkErrorStatusCode(t, err))

	// Update over the value cap is rejected with 400 as well.
	_, err = f.UpdateConfigStoreSecret(context.Background(), sdkkonnectops.UpdateConfigStoreSecretRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  fakeTestStoreID,
		Key:            "value-at-cap",
		UpdateConfigStoreSecret: sdkkonnectcomp.UpdateConfigStoreSecret{
			Value: strings.Repeat("v", FakeConfigStoreSecretMaxValueBytes+1),
		},
	})
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, sdkErrorStatusCode(t, err))
}

func TestFakeConfigStoreSecretsTrapCharactersAcceptedOnCreateFailOnRead(t *testing.T) {
	testCases := []struct {
		key            string
		readStatusCode int
	}{
		{key: "k8s-2-ns-1-a#frag", readStatusCode: http.StatusNotFound},
		{key: "k8s-2-ns-1-a%pct", readStatusCode: http.StatusInternalServerError},
		{key: "k8s-2-ns-1-a/slash", readStatusCode: http.StatusNotFound},
	}
	for _, tc := range testCases {
		t.Run(tc.key, func(t *testing.T) {
			f := NewFakeConfigStoreSecrets()
			// Create accepts trap characters: no key-character validation.
			createSecret(t, f, tc.key, "value")

			_, err := f.GetConfigStoreSecret(context.Background(), sdkkonnectops.GetConfigStoreSecretRequest{
				ControlPlaneID: fakeTestCPID,
				ConfigStoreID:  fakeTestStoreID,
				Key:            tc.key,
			})
			require.Error(t, err)
			assert.Equal(t, tc.readStatusCode, sdkErrorStatusCode(t, err))

			_, err = f.UpdateConfigStoreSecret(context.Background(), sdkkonnectops.UpdateConfigStoreSecretRequest{
				ControlPlaneID: fakeTestCPID,
				ConfigStoreID:  fakeTestStoreID,
				Key:            tc.key,
				UpdateConfigStoreSecret: sdkkonnectcomp.UpdateConfigStoreSecret{
					Value: "new-value",
				},
			})
			require.Error(t, err)
			assert.Equal(t, tc.readStatusCode, sdkErrorStatusCode(t, err))

			_, err = f.DeleteConfigStoreSecret(context.Background(), sdkkonnectops.DeleteConfigStoreSecretRequest{
				ControlPlaneID: fakeTestCPID,
				ConfigStoreID:  fakeTestStoreID,
				Key:            tc.key,
			})
			require.Error(t, err)
			assert.Equal(t, tc.readStatusCode, sdkErrorStatusCode(t, err))
		})
	}
}

func TestFakeConfigStoreSecretsUpdatedAtAdvancesOnIdenticalWrite(t *testing.T) {
	f := NewFakeConfigStoreSecrets()
	createSecret(t, f, "key", "same-value")

	getUpdatedAt := func() string {
		resp, err := f.GetConfigStoreSecret(context.Background(), sdkkonnectops.GetConfigStoreSecretRequest{
			ControlPlaneID: fakeTestCPID,
			ConfigStoreID:  fakeTestStoreID,
			Key:            "key",
		})
		require.NoError(t, err)
		return resp.ConfigStoreSecret.UpdatedAt.String()
	}

	before := getUpdatedAt()
	_, err := f.UpdateConfigStoreSecret(context.Background(), sdkkonnectops.UpdateConfigStoreSecretRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  fakeTestStoreID,
		Key:            "key",
		UpdateConfigStoreSecret: sdkkonnectcomp.UpdateConfigStoreSecret{
			Value: "same-value", // identical value
		},
	})
	require.NoError(t, err)
	after := getUpdatedAt()
	assert.NotEqual(t, before, after, "updated_at must advance even on identical-value writes")
}

func TestFakeConfigStoreSecretsMissingKeyOperationsReturn404(t *testing.T) {
	f := NewFakeConfigStoreSecrets()

	_, err := f.GetConfigStoreSecret(context.Background(), sdkkonnectops.GetConfigStoreSecretRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  fakeTestStoreID,
		Key:            "missing",
	})
	require.Error(t, err)
	assert.Equal(t, http.StatusNotFound, sdkErrorStatusCode(t, err))

	_, err = f.UpdateConfigStoreSecret(context.Background(), sdkkonnectops.UpdateConfigStoreSecretRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  fakeTestStoreID,
		Key:            "missing",
		UpdateConfigStoreSecret: sdkkonnectcomp.UpdateConfigStoreSecret{
			Value: "v",
		},
	})
	require.Error(t, err)
	assert.Equal(t, http.StatusNotFound, sdkErrorStatusCode(t, err))

	_, err = f.DeleteConfigStoreSecret(context.Background(), sdkkonnectops.DeleteConfigStoreSecretRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  fakeTestStoreID,
		Key:            "missing",
	})
	require.Error(t, err)
	assert.Equal(t, http.StatusNotFound, sdkErrorStatusCode(t, err))
}

func TestFakeConfigStoreSecretsCallLog(t *testing.T) {
	f := NewFakeConfigStoreSecrets()
	createSecret(t, f, "key", "value")

	_, err := f.GetConfigStoreSecret(context.Background(), sdkkonnectops.GetConfigStoreSecretRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  fakeTestStoreID,
		Key:            "key",
	})
	require.NoError(t, err)

	calls := f.Calls()
	require.Len(t, calls, 2)
	assert.Equal(t, "Create", calls[0].Method)
	assert.Equal(t, "Get", calls[1].Method)

	mutating := f.MutatingCalls()
	require.Len(t, mutating, 1)
	assert.Equal(t, "Create", mutating[0].Method)

	f.ResetCalls()
	assert.Empty(t, f.Calls())
}

func TestFakeConfigStoreSecretsErrorHook(t *testing.T) {
	f := NewFakeConfigStoreSecrets()
	createSecret(t, f, "key", "value")

	injected := errors.New("injected failure")
	f.ErrorHook = func(method, key string) error {
		if key == "boom" {
			return injected
		}
		return nil
	}

	// Create: the failed call is logged but leaves no state behind.
	_, err := f.CreateConfigStoreSecret(context.Background(), sdkkonnectops.CreateConfigStoreSecretRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  fakeTestStoreID,
		CreateConfigStoreSecret: sdkkonnectcomp.CreateConfigStoreSecret{
			Key:   "boom",
			Value: "value",
		},
	})
	require.ErrorIs(t, err, injected)
	_, ok := f.Value(fakeTestCPID, fakeTestStoreID, "boom")
	assert.False(t, ok, "a hooked Create must not persist state")

	// Update and Delete consult the hook as well.
	_, err = f.UpdateConfigStoreSecret(context.Background(), sdkkonnectops.UpdateConfigStoreSecretRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  fakeTestStoreID,
		Key:            "boom",
		UpdateConfigStoreSecret: sdkkonnectcomp.UpdateConfigStoreSecret{
			Value: "value",
		},
	})
	require.ErrorIs(t, err, injected)
	_, err = f.DeleteConfigStoreSecret(context.Background(), sdkkonnectops.DeleteConfigStoreSecretRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  fakeTestStoreID,
		Key:            "boom",
	})
	require.ErrorIs(t, err, injected)

	// All four calls (initial Create + 3 hooked calls) are logged in order.
	calls := f.Calls()
	require.Len(t, calls, 4)
	assert.Equal(t, "Create", calls[0].Method)
	assert.Equal(t, "Create", calls[1].Method)
	assert.Equal(t, "Update", calls[2].Method)
	assert.Equal(t, "Delete", calls[3].Method)

	// Unrelated keys are unaffected by the hook.
	createSecret(t, f, "fine", "value")
	stored, ok := f.Value(fakeTestCPID, fakeTestStoreID, "fine")
	require.True(t, ok)
	assert.Equal(t, "value", stored)
}

func TestFakeConfigStoreSecretsStoresAreIsolated(t *testing.T) {
	f := NewFakeConfigStoreSecrets()
	createSecret(t, f, "key", "value")

	// Same key in another store does not conflict.
	_, err := f.CreateConfigStoreSecret(context.Background(), sdkkonnectops.CreateConfigStoreSecretRequest{
		ControlPlaneID: fakeTestCPID,
		ConfigStoreID:  "other-store",
		CreateConfigStoreSecret: sdkkonnectcomp.CreateConfigStoreSecret{
			Key:   "key",
			Value: "other-value",
		},
	})
	require.NoError(t, err)

	stored, ok := f.Value(fakeTestCPID, "other-store", "key")
	require.True(t, ok)
	assert.Equal(t, "other-value", stored)
	assert.False(t, f.StoreEmpty(fakeTestCPID, fakeTestStoreID))
	assert.True(t, f.StoreEmpty(fakeTestCPID, "never-touched-store"))
}
