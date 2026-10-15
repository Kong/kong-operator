package hpa_test

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/controller/pkg/hpa"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

const (
	testNamespace  = "default"
	deploymentName = "test-deployment"
)

// newOwner returns an owner with an explicit UID, so ListHPAsForOwner
// (which matches by owner UID) can find its owned HPAs.
func newOwner() *operatorv1beta1.DataPlane {
	return &operatorv1beta1.DataPlane{
		Name:      "test-dp",
		Namespace: testNamespace,
		UID:       types.UID("test-dp-uid"),
	}
}

func newClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(managerscheme.Get()).
		WithObjects(objs...).
		Build()
}

// ensure sets scaling on owner (removing it when nil) and calls hpa.Ensure.
func ensure(t *testing.T, cl client.Client, owner *operatorv1beta1.DataPlane, scaling *commonv1alpha1.HorizontalScaling) op.Result {
	t.Helper()
	owner.Spec.Deployment.Scaling = nil
	if scaling != nil {
		owner.Spec.Deployment.Scaling = &operatorv1beta1.Scaling{HorizontalScaling: scaling}
	}
	res, err := hpa.Ensure(t.Context(), cl, logr.Discard(), owner, deploymentName)
	require.NoError(t, err)
	return res
}

func listHPAs(t *testing.T, cl client.Client) []autoscalingv2.HorizontalPodAutoscaler {
	t.Helper()
	var hpaList autoscalingv2.HorizontalPodAutoscalerList
	require.NoError(t, cl.List(t.Context(), &hpaList, client.InNamespace(testNamespace)))
	return hpaList.Items
}

func TestEnsure(t *testing.T) {
	t.Run("creates HPA when scaling is configured", func(t *testing.T) {
		owner := newOwner()
		cl := newClient(owner)

		assert.Equal(t, op.Created, ensure(t, cl, owner, &commonv1alpha1.HorizontalScaling{MaxReplicas: 5}))

		var h autoscalingv2.HorizontalPodAutoscaler
		require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Namespace: testNamespace, Name: owner.Name}, &h))
		assert.Equal(t, int32(5), h.Spec.MaxReplicas)
		assert.Equal(t, deploymentName, h.Spec.ScaleTargetRef.Name)
		assert.Equal(t, "Deployment", h.Spec.ScaleTargetRef.Kind)
	})

	t.Run("is a no-op when HPA is up to date", func(t *testing.T) {
		owner := newOwner()
		cl := newClient(owner)
		scaling := &commonv1alpha1.HorizontalScaling{MaxReplicas: 5}

		require.Equal(t, op.Created, ensure(t, cl, owner, scaling))
		assert.Equal(t, op.Noop, ensure(t, cl, owner, scaling))
	})

	t.Run("updates HPA when maxReplicas changes", func(t *testing.T) {
		owner := newOwner()
		cl := newClient(owner)

		require.Equal(t, op.Created, ensure(t, cl, owner, &commonv1alpha1.HorizontalScaling{MaxReplicas: 5}))
		assert.Equal(t, op.Updated, ensure(t, cl, owner, &commonv1alpha1.HorizontalScaling{MaxReplicas: 10}))

		var h autoscalingv2.HorizontalPodAutoscaler
		require.NoError(t, cl.Get(t.Context(), types.NamespacedName{Namespace: testNamespace, Name: owner.Name}, &h))
		assert.Equal(t, int32(10), h.Spec.MaxReplicas)
	})

	t.Run("deletes HPA when scaling is removed", func(t *testing.T) {
		owner := newOwner()
		cl := newClient(owner)

		require.Equal(t, op.Created, ensure(t, cl, owner, &commonv1alpha1.HorizontalScaling{MaxReplicas: 5}))
		assert.Equal(t, op.Noop, ensure(t, cl, owner, nil))
		assert.Empty(t, listHPAs(t, cl))
	})

	t.Run("no HPA and no scaling is a no-op", func(t *testing.T) {
		owner := newOwner()
		cl := newClient(owner)

		assert.Equal(t, op.Noop, ensure(t, cl, owner, nil))
		assert.Empty(t, listHPAs(t, cl))
	})

	t.Run("reduces to one HPA when multiple exist", func(t *testing.T) {
		owner := newOwner()
		// Managed labels are required so ListHPAsForOwner's label selector finds the HPAs.
		labels := k8sresources.GetManagedLabelForOwner(owner)
		ownerRef := metav1.OwnerReference{UID: owner.UID, Name: owner.Name}
		hpa1 := &autoscalingv2.HorizontalPodAutoscaler{
			Name: owner.Name + "-1", Namespace: testNamespace,
			Labels:          labels,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		}
		hpa2 := &autoscalingv2.HorizontalPodAutoscaler{
			Name: owner.Name + "-2", Namespace: testNamespace,
			Labels:          labels,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		}
		cl := newClient(owner, hpa1, hpa2)

		assert.Equal(t, op.Noop, ensure(t, cl, owner, &commonv1alpha1.HorizontalScaling{MaxReplicas: 5}))
		assert.Len(t, listHPAs(t, cl), 1)
	})

	t.Run("List error is wrapped with owner kind, namespace and name", func(t *testing.T) {
		owner := newOwner()
		base := fake.NewClientBuilder().
			WithScheme(managerscheme.Get()).
			WithObjects(owner).
			Build()
		cl := interceptor.NewClient(base, interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				return assert.AnError
			},
		})

		res, err := hpa.Ensure(t.Context(), cl, logr.Discard(), owner, deploymentName)
		require.ErrorIs(t, err, assert.AnError)
		// Match the full prefix so the %T fallback in ownerKind ("*v1beta1.DataPlane") would fail.
		assert.ErrorContains(t, err, "failed listing HPAs for DataPlane "+testNamespace+"/"+owner.Name+":")
		assert.Equal(t, op.Noop, res)
	})
}
