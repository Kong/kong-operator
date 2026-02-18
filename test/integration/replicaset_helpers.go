package integration

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
)

// FindDataPlaneReplicaSetNewerThan finds a ReplicaSet created after or at the specified time.
// This helper logs detailed information about ReplicaSets found to help
// with debugging timestamp issues.
//
// It will return nil if no ReplicaSet is found or if there's an error listing ReplicaSets.
// If multiple ReplicaSets are found that were created after or at the specified time,
// it will fail the test immediately with t.FailNow().
// It succeed when exactly 1 matching ReplicaSet is found.
func FindDataPlaneReplicaSetNewerThan(
	t *testing.T,
	ctx context.Context,
	cli client.Client,
	creationTime time.Time,
	namespace string,
	dp *operatorv1beta1.DataPlane,
) *appsv1.ReplicaSet {
	t.Helper()

	rsList, err := testutils.GetDataPlaneReplicaSets(ctx, cli, dp)
	if err != nil {
		t.Logf("Error listing ReplicaSets: %v", err)
		return nil
	}

	// Find ReplicaSets newer than or exactly at the specified time
	var newReplicaSets []*appsv1.ReplicaSet
	t.Logf("Looking for ReplicaSets created at or after %v", creationTime)
	for _, rs := range rsList {
		t.Logf("Found ReplicaSet %s with creation time %v", rs.Name, rs.CreationTimestamp.Time)
		// Truncate times to seconds for comparison since k8s doesn't use same precision
		rsTime := rs.CreationTimestamp.Truncate(time.Second)
		refTime := creationTime.Truncate(time.Second)
		if rsTime.Equal(refTime) || rsTime.After(refTime) {
			t.Logf("ReplicaSet %s is at or after %v", rs.Name, creationTime)
			newReplicaSets = append(newReplicaSets, rs)
		}
	}

	// No new ReplicaSets found
	if len(newReplicaSets) == 0 {
		t.Logf("No ReplicaSets found at or after %v", creationTime)
		return nil
	}

	// Multiple new ReplicaSets found - fail the test
	if len(newReplicaSets) > 1 {
		t.Errorf("Found %d ReplicaSets created at or after %v, expected exactly 1",
			len(newReplicaSets), creationTime)
		t.FailNow()
	}

	// Success: exactly one new ReplicaSet found
	return newReplicaSets[0]
}
