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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// newHPAEGDP returns a KegDataPlane with horizontal scaling configured.
func newHPAEGDP() *eventgatewayv1alpha1.KegDataPlane {
	dp := newReconcileEGDP()
	dp.Spec.Deployment = &eventgatewayv1alpha1.DeploymentOptions{
		Scaling: &eventgatewayv1alpha1.Scaling{
			HorizontalScaling: &eventgatewayv1alpha1.HorizontalScaling{
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
		egdp := newHPAEGDP()
		r, cl := newReconcilerForHPATest(t, egdp)

		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), egdp, egdp.Name))

		var hpa autoscalingv2.HorizontalPodAutoscaler
		require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Namespace: reconcileTestNS, Name: egdp.Name}, &hpa))
		assert.Equal(t, int32(5), hpa.Spec.MaxReplicas)
		assert.Equal(t, egdp.Name, hpa.Spec.ScaleTargetRef.Name)
		assert.Equal(t, "Deployment", hpa.Spec.ScaleTargetRef.Kind)
	})

	t.Run("deletes HPA when scaling is removed", func(t *testing.T) {
		egdp := newHPAEGDP()
		r, cl := newReconcilerForHPATest(t, egdp)

		// First call creates the HPA.
		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), egdp, egdp.Name))

		// Remove scaling and call again.
		noScaling := egdp.DeepCopy()
		noScaling.Spec.Deployment = nil
		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), noScaling, noScaling.Name))

		var hpaList autoscalingv2.HorizontalPodAutoscalerList
		require.NoError(t, cl.List(t.Context(), &hpaList, client.InNamespace(reconcileTestNS)))
		assert.Empty(t, hpaList.Items)
	})

	t.Run("updates HPA when maxReplicas changes", func(t *testing.T) {
		egdp := newHPAEGDP()
		r, cl := newReconcilerForHPATest(t, egdp)

		// Create HPA.
		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), egdp, egdp.Name))

		// Update maxReplicas.
		updated := egdp.DeepCopy()
		updated.Spec.Deployment.Scaling.HorizontalScaling.MaxReplicas = 10
		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), updated, updated.Name))

		var hpa autoscalingv2.HorizontalPodAutoscaler
		require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Namespace: reconcileTestNS, Name: egdp.Name}, &hpa))
		assert.Equal(t, int32(10), hpa.Spec.MaxReplicas)
	})

	t.Run("reduces to one HPA when multiple exist", func(t *testing.T) {
		egdp := newHPAEGDP()
		// Managed labels are required so ListHPAsForOwner's label selector finds the HPAs.
		labels := k8sresources.GetManagedLabelForOwner(egdp)
		ownerRef := metav1.OwnerReference{UID: egdp.UID, Name: egdp.Name}

		hpa1 := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name: egdp.Name + "-1", Namespace: reconcileTestNS,
				Labels:          labels,
				OwnerReferences: []metav1.OwnerReference{ownerRef},
			},
		}
		hpa2 := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name: egdp.Name + "-2", Namespace: reconcileTestNS,
				Labels:          labels,
				OwnerReferences: []metav1.OwnerReference{ownerRef},
			},
		}
		r, cl := newReconcilerForHPATest(t, egdp, hpa1, hpa2)

		require.NoError(t, r.ensureHPA(t.Context(), logr.Discard(), egdp, egdp.Name))

		var hpaList autoscalingv2.HorizontalPodAutoscalerList
		require.NoError(t, cl.List(t.Context(), &hpaList, client.InNamespace(reconcileTestNS)))
		assert.Len(t, hpaList.Items, 1)
	})
}

func TestGenerateBaseDeployment_ReplicaGuard(t *testing.T) {
	keg := newProgrammedKEG()

	t.Run("sets replicas when HPA is not configured", func(t *testing.T) {
		replicas := int32(3)
		egdp := newReconcileEGDP()
		egdp.Spec.Deployment = &eventgatewayv1alpha1.DeploymentOptions{Replicas: &replicas}

		d, err := generateBaseDeployment(logr.Discard(), egdp, keg, "kong/keg:latest", "cert-secret")
		require.NoError(t, err)
		require.NotNil(t, d.Spec.Replicas)
		assert.Equal(t, replicas, *d.Spec.Replicas)
	})

	t.Run("omits replicas when HPA is active", func(t *testing.T) {
		egdp := newHPAEGDP()
		egdp.Spec.Deployment.Replicas = new(int32(3))

		d, err := generateBaseDeployment(logr.Discard(), egdp, keg, "kong/keg:latest", "cert-secret")
		require.NoError(t, err)
		assert.Nil(t, d.Spec.Replicas, "spec.replicas must be nil when HPA is active so HPA owns the field")
	})

	t.Run("seeds replicas from minReplicas when HPA is active and minReplicas is set", func(t *testing.T) {
		egdp := newHPAEGDP()
		egdp.Spec.Deployment.Scaling.HorizontalScaling.MinReplicas = new(int32(2))

		d, err := generateBaseDeployment(logr.Discard(), egdp, keg, "kong/keg:latest", "cert-secret")
		require.NoError(t, err)
		require.NotNil(t, d.Spec.Replicas)
		assert.Equal(t, int32(2), *d.Spec.Replicas)
	})
}
