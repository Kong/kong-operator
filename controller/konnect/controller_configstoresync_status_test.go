package konnect

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func TestConfigStoreSecretWriteDecisionPersistedTimePrecision(t *testing.T) {
	updatedAt := time.Date(2026, time.September, 24, 12, 0, 0, 123_456_789, time.UTC)
	encoded, err := json.Marshal(observedUpdatedAt(updatedAt))
	require.NoError(t, err)
	var persisted metav1.Time
	require.NoError(t, json.Unmarshal(encoded, &persisted))

	write, reason := configStoreSecretWriteDecision(true, updatedAt, &persisted, "sha256:same", "sha256:same")
	assert.False(t, write, "precision lost when status is persisted must not look like drift")
	assert.Empty(t, reason)

	write, reason = configStoreSecretWriteDecision(true, updatedAt.Add(time.Second), &persisted, "sha256:same", "sha256:same")
	assert.True(t, write)
	assert.Equal(t, "Drifted", reason)
}

func TestSetConditionSyncedDuringDeletionEnsuresRequiredConditions(t *testing.T) {
	sync := &konnectv1alpha1.KonnectConfigStoreSync{
		ObjectMeta: metav1.ObjectMeta{
			DeletionTimestamp: new(metav1.Now()),
			Generation:        2,
		},
	}
	r := &KonnectConfigStoreSyncReconciler{}

	r.setConditionSynced(sync, metav1.ConditionFalse, "Blocked", "deletion is blocked")

	require.Len(t, sync.Status.Conditions, 4)
	condition := apimeta.FindStatusCondition(
		sync.Status.Conditions,
		konnectv1alpha1.KonnectConfigStoreSyncSyncedConditionType,
	)
	require.NotNil(t, condition)
	assert.Equal(t, metav1.ConditionFalse, condition.Status)
	assert.Equal(t, int64(2), condition.ObservedGeneration)
}
