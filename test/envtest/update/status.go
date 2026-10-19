package update

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/test/envtest/consts"
)

// DeploymentRolloutStatus sets Deployment status fields to report a
// fully-complete rollout (matching k8sutils.DeploymentRolloutComplete).
// Envtest doesn't run the real Deployment controller, so nothing populates
// these fields on its own.
func DeploymentRolloutStatus(t *testing.T, ctx context.Context, cl client.Client, ns, name string) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		deploy := &appsv1.Deployment{}
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, deploy)) {
			return
		}
		replicas := int32(1)
		if deploy.Spec.Replicas != nil {
			replicas = *deploy.Spec.Replicas
		}
		deploy.Status = appsv1.DeploymentStatus{
			ObservedGeneration: deploy.Generation,
			Replicas:           replicas,
			UpdatedReplicas:    replicas,
			ReadyReplicas:      replicas,
			AvailableReplicas:  replicas,
		}
		assert.NoError(ct, cl.Status().Update(ctx, deploy))
	}, consts.WaitTime, consts.TickTime)
}
