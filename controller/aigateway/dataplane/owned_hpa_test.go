/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dataplane

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// newHPAAIGWDP returns an AIGatewayDataPlane with horizontal scaling configured.
func newHPAAIGWDP() *aigatewayv1alpha1.AIGatewayDataPlane {
	dp := newReconcileAIGWDP()
	dp.Spec.Deployment = &aigatewayv1alpha1.DeploymentOptions{
		Scaling: &aigatewayv1alpha1.Scaling{
			HorizontalScaling: &aigatewayv1alpha1.HorizontalScaling{
				MaxReplicas: 5,
			},
		},
	}
	return dp
}

// newReconcilerForHPATest builds a reconciler + fake client seeded with the given objects.
func newReconcilerForHPATest(t *testing.T, objs ...client.Object) (*Reconciler, client.Client) {
	t.Helper()
	cl := fake.NewClientBuilder().
		WithScheme(managerscheme.Get()).
		WithObjects(objs...).
		Build()
	r := newTestReconciler(cl, events.NewFakeRecorder(10))
	return r, cl
}

func TestEnsureHPA(t *testing.T) {
	t.Run("creates HPA when scaling is configured", func(t *testing.T) {
		aigwdp := newHPAAIGWDP()
		r, cl := newReconcilerForHPATest(t, aigwdp)

		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), aigwdp, aigwdp.Name))

		var hpa autoscalingv2.HorizontalPodAutoscaler
		require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Namespace: reconcileTestNS, Name: aigwdp.Name}, &hpa))
		assert.Equal(t, int32(5), hpa.Spec.MaxReplicas)
		assert.Equal(t, aigwdp.Name, hpa.Spec.ScaleTargetRef.Name)
		assert.Equal(t, "Deployment", hpa.Spec.ScaleTargetRef.Kind)
	})

	t.Run("deletes HPA when scaling is removed", func(t *testing.T) {
		aigwdp := newHPAAIGWDP()
		r, cl := newReconcilerForHPATest(t, aigwdp)

		// First call creates the HPA.
		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), aigwdp, aigwdp.Name))

		// Remove scaling and call again.
		noScaling := aigwdp.DeepCopy()
		noScaling.Spec.Deployment = nil
		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), noScaling, noScaling.Name))

		var hpaList autoscalingv2.HorizontalPodAutoscalerList
		require.NoError(t, cl.List(t.Context(), &hpaList, client.InNamespace(reconcileTestNS)))
		assert.Empty(t, hpaList.Items)
	})

	t.Run("updates HPA when maxReplicas changes", func(t *testing.T) {
		aigwdp := newHPAAIGWDP()
		r, cl := newReconcilerForHPATest(t, aigwdp)

		// Create HPA.
		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), aigwdp, aigwdp.Name))

		// Update maxReplicas.
		updated := aigwdp.DeepCopy()
		updated.Spec.Deployment.Scaling.HorizontalScaling.MaxReplicas = 10
		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), updated, updated.Name))

		var hpa autoscalingv2.HorizontalPodAutoscaler
		require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Namespace: reconcileTestNS, Name: aigwdp.Name}, &hpa))
		assert.Equal(t, int32(10), hpa.Spec.MaxReplicas)
	})
}

func TestGenerateBaseDeployment_ReplicaGuard(t *testing.T) {
	aigatewaycp := testKonnectAIGateway("cp.example.com", "tp.example.com")

	t.Run("sets replicas when HPA is not configured", func(t *testing.T) {
		replicas := int32(3)
		aigwdp := newReconcileAIGWDP()
		aigwdp.Spec.Deployment = &aigatewayv1alpha1.DeploymentOptions{Replicas: &replicas}

		d, err := generateBaseDeployment(logr.Discard(), aigwdp, aigatewaycp, "kong/aigw:latest", "cert-secret")
		require.NoError(t, err)
		require.NotNil(t, d.Spec.Replicas)
		assert.Equal(t, replicas, *d.Spec.Replicas)
	})

	t.Run("omits replicas when HPA is active", func(t *testing.T) {
		aigwdp := newHPAAIGWDP()

		d, err := generateBaseDeployment(logr.Discard(), aigwdp, aigatewaycp, "kong/aigw:latest", "cert-secret")
		require.NoError(t, err)
		assert.Nil(t, d.Spec.Replicas, "spec.replicas must be nil when HPA is active so HPA owns the field")
	})
}
