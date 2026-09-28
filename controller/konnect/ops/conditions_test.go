package ops

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// TestSetKonnectEntityProgrammedConditionTrueRefreshesTimestamp ensures that re-setting the
// Programmed condition True on an already-True condition refreshes LastTransitionTime.
// shouldUpdate (ops.go) gates Konnect updates on that timestamp; if it stays frozen,
// every event-triggered reconcile performs a full Konnect API update after one sync period.
func TestSetKonnectEntityProgrammedConditionTrueRefreshesTimestamp(t *testing.T) {
	var obj aiconfigurationv1alpha1.AIGatewayModelProvider

	SetKonnectEntityProgrammedConditionTrue(&obj)
	first, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, &obj)
	require.True(t, ok)

	time.Sleep(2 * time.Millisecond)
	SetKonnectEntityProgrammedConditionTrue(&obj)
	second, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, &obj)
	require.True(t, ok)

	assert.True(t, second.LastTransitionTime.After(first.LastTransitionTime.Time),
		"LastTransitionTime must advance when the condition is re-set True: first=%s second=%s",
		first.LastTransitionTime, second.LastTransitionTime)

	// With a fresh timestamp, shouldUpdate must skip the update for the remaining
	// sync period instead of requesting a full Konnect update.
	const syncPeriod = time.Minute
	should, res := shouldUpdate(context.Background(), &obj, syncPeriod, time.Now())
	assert.False(t, should, "update should be skipped while within the sync period")
	assert.True(t, res.RequeueAfter > 0 && res.RequeueAfter <= syncPeriod,
		"requeue should be the remaining sync period, got %s", res.RequeueAfter)

	// Sanity: a condition set False-then-True still transitions normally.
	SetKonnectEntityProgrammedConditionFalse(&obj, "TestReason", assert.AnError)
	SetKonnectEntityProgrammedConditionTrue(&obj)
	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, &obj)
	require.True(t, ok)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed, cond.Reason)
}
