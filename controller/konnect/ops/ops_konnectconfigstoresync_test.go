package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

const (
	testSyncCPID    = "cp-1"
	testSyncStoreID = "store-1"
)

func TestUpsertConfigStoreSecret(t *testing.T) {
	ctx := context.Background()

	t.Run("creates a new entry via PUT", func(t *testing.T) {
		fake := sdkmocks.NewFakeConfigStoreSecrets()
		created, updatedAt, err := UpsertConfigStoreSecret(ctx, fake, testSyncCPID, testSyncStoreID, "key", "value")
		require.NoError(t, err)
		assert.True(t, created)
		assert.False(t, updatedAt.IsZero())

		stored, ok := fake.Value(testSyncCPID, testSyncStoreID, "key")
		require.True(t, ok)
		assert.Equal(t, "value", stored)
		require.Len(t, fake.Calls(), 1)
		assert.Equal(t, "Update", fake.Calls()[0].Method)
	})

	t.Run("updates an existing entry via PUT", func(t *testing.T) {
		fake := sdkmocks.NewFakeConfigStoreSecrets()
		fake.SetValue(testSyncCPID, testSyncStoreID, "key", "pre-existing")

		created, _, err := UpsertConfigStoreSecret(ctx, fake, testSyncCPID, testSyncStoreID, "key", "new-value")
		require.NoError(t, err)
		assert.False(t, created, "existing entry must be updated, not created")

		stored, ok := fake.Value(testSyncCPID, testSyncStoreID, "key")
		require.True(t, ok)
		assert.Equal(t, "new-value", stored)

		require.Len(t, fake.Calls(), 1)
		assert.Equal(t, "Update", fake.Calls()[0].Method)
	})

	t.Run("propagates update errors", func(t *testing.T) {
		fake := sdkmocks.NewFakeConfigStoreSecrets()
		// Over-cap value triggers a 400 from the fake.
		_, _, err := UpsertConfigStoreSecret(
			ctx, fake, testSyncCPID, testSyncStoreID, "key",
			strings.Repeat("v", sdkmocks.FakeConfigStoreSecretMaxValueBytes+1),
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to upsert config store secret")
	})
}

func TestGetConfigStoreSecretMetadata(t *testing.T) {
	ctx := context.Background()
	fake := sdkmocks.NewFakeConfigStoreSecrets()

	_, found, err := GetConfigStoreSecretMetadata(ctx, fake, testSyncCPID, testSyncStoreID, "missing")
	require.NoError(t, err)
	assert.False(t, found)

	fake.SetValue(testSyncCPID, testSyncStoreID, "key", "value")
	updatedAt, found, err := GetConfigStoreSecretMetadata(ctx, fake, testSyncCPID, testSyncStoreID, "key")
	require.NoError(t, err)
	assert.True(t, found)
	assert.False(t, updatedAt.IsZero())
}

func TestDeleteConfigStoreSecret(t *testing.T) {
	ctx := context.Background()
	fake := sdkmocks.NewFakeConfigStoreSecrets()
	fake.SetValue(testSyncCPID, testSyncStoreID, "key", "value")

	require.NoError(t, DeleteConfigStoreSecret(ctx, fake, testSyncCPID, testSyncStoreID, "key"))
	assert.True(t, fake.StoreEmpty(testSyncCPID, testSyncStoreID))

	// Deleting an already-missing entry is tolerated.
	require.NoError(t, DeleteConfigStoreSecret(ctx, fake, testSyncCPID, testSyncStoreID, "key"))
}
