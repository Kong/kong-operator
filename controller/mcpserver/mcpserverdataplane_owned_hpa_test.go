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

package mcpserver

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mcpv1alpha1 "github.com/kong/kong-operator/v2/api/mcp/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// mcpServerDataPlaneWithUID returns a minimal MCPServerDataPlane with an
// explicit UID, so ListHPAsForOwner (which matches by owner UID) can find its
// owned HPAs.
func mcpServerDataPlaneWithUID() *mcpv1alpha1.MCPServerDataPlane {
	dp := minimalMCPServerDataPlane()
	dp.UID = types.UID("mcpdp-uid")
	return dp
}

// newHPAMCPDataPlane returns an MCPServerDataPlane with horizontal scaling configured.
func newHPAMCPDataPlane() *mcpv1alpha1.MCPServerDataPlane {
	dp := mcpServerDataPlaneWithUID()
	dp.Spec.Deployment = &mcpv1alpha1.DeploymentOptions{
		Scaling: &mcpv1alpha1.Scaling{
			HorizontalScaling: &mcpv1alpha1.HorizontalScaling{
				MaxReplicas: 5,
			},
		},
	}
	return dp
}

// newReconcilerForHPATest builds a reconciler + fake client seeded with the given objects.
func newReconcilerForHPATest(t *testing.T, objs ...client.Object) (*MCPServerDataPlaneReconciler, client.Client) {
	t.Helper()
	cl := fake.NewClientBuilder().
		WithScheme(managerscheme.Get()).
		WithObjects(objs...).
		Build()
	r := &MCPServerDataPlaneReconciler{
		Client:        cl,
		eventRecorder: events.NewFakeRecorder(10),
	}
	return r, cl
}

func TestEnsureHPA(t *testing.T) {
	t.Run("creates HPA when scaling is configured", func(t *testing.T) {
		mcpDataPlane := newHPAMCPDataPlane()
		r, cl := newReconcilerForHPATest(t, mcpDataPlane)

		deploymentName := generateWorkloadNN(mcpDataPlane).Name
		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), mcpDataPlane, deploymentName))

		var hpa autoscalingv2.HorizontalPodAutoscaler
		require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Namespace: testMCPServerNamespace, Name: mcpDataPlane.Name}, &hpa))
		assert.Equal(t, int32(5), hpa.Spec.MaxReplicas)
		assert.Equal(t, deploymentName, hpa.Spec.ScaleTargetRef.Name)
		assert.Equal(t, "Deployment", hpa.Spec.ScaleTargetRef.Kind)
	})

	t.Run("deletes HPA when scaling is removed", func(t *testing.T) {
		mcpDataPlane := newHPAMCPDataPlane()
		r, cl := newReconcilerForHPATest(t, mcpDataPlane)

		deploymentName := generateWorkloadNN(mcpDataPlane).Name
		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), mcpDataPlane, deploymentName))

		noScaling := mcpDataPlane.DeepCopy()
		noScaling.Spec.Deployment = nil
		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), noScaling, deploymentName))

		var hpaList autoscalingv2.HorizontalPodAutoscalerList
		require.NoError(t, cl.List(t.Context(), &hpaList, client.InNamespace(testMCPServerNamespace)))
		assert.Empty(t, hpaList.Items)
	})

	t.Run("updates HPA when maxReplicas changes", func(t *testing.T) {
		mcpDataPlane := newHPAMCPDataPlane()
		r, cl := newReconcilerForHPATest(t, mcpDataPlane)

		deploymentName := generateWorkloadNN(mcpDataPlane).Name
		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), mcpDataPlane, deploymentName))

		updated := mcpDataPlane.DeepCopy()
		updated.Spec.Deployment.Scaling.HorizontalScaling.MaxReplicas = 10
		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), updated, deploymentName))

		var hpa autoscalingv2.HorizontalPodAutoscaler
		require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Namespace: testMCPServerNamespace, Name: mcpDataPlane.Name}, &hpa))
		assert.Equal(t, int32(10), hpa.Spec.MaxReplicas)
	})

	t.Run("reduces to one HPA when multiple exist", func(t *testing.T) {
		mcpDataPlane := newHPAMCPDataPlane()
		// Managed labels are required so ListHPAsForOwner's label selector finds the HPAs.
		labels := k8sresources.GetManagedLabelForOwner(mcpDataPlane)
		ownerRef := metav1.OwnerReference{UID: mcpDataPlane.UID, Name: mcpDataPlane.Name}

		hpa1 := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcpDataPlane.Name + "-1", Namespace: testMCPServerNamespace,
				Labels:          labels,
				OwnerReferences: []metav1.OwnerReference{ownerRef},
			},
		}
		hpa2 := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcpDataPlane.Name + "-2", Namespace: testMCPServerNamespace,
				Labels:          labels,
				OwnerReferences: []metav1.OwnerReference{ownerRef},
			},
		}
		r, cl := newReconcilerForHPATest(t, mcpDataPlane, hpa1, hpa2)

		deploymentName := generateWorkloadNN(mcpDataPlane).Name
		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), mcpDataPlane, deploymentName))

		var hpaList autoscalingv2.HorizontalPodAutoscalerList
		require.NoError(t, cl.List(t.Context(), &hpaList, client.InNamespace(testMCPServerNamespace)))
		assert.Len(t, hpaList.Items, 1)
	})

	t.Run("no HPA and no scaling is a no-op", func(t *testing.T) {
		mcpDataPlane := mcpServerDataPlaneWithUID()
		r, cl := newReconcilerForHPATest(t, mcpDataPlane)

		deploymentName := generateWorkloadNN(mcpDataPlane).Name
		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), mcpDataPlane, deploymentName))

		var hpaList autoscalingv2.HorizontalPodAutoscalerList
		require.NoError(t, cl.List(t.Context(), &hpaList, client.InNamespace(testMCPServerNamespace)))
		assert.Empty(t, hpaList.Items)
	})
}
