package hpa_test

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
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

func TestDeploymentReplicas(t *testing.T) {
	key := client.ObjectKey{Namespace: testNamespace, Name: deploymentName}
	deploymentWithReplicas := func(replicas int32) *appsv1.Deployment {
		return &appsv1.Deployment{
			Namespace: testNamespace, Name: deploymentName,
			Spec: appsv1.DeploymentSpec{Replicas: new(replicas)},
		}
	}

	testCases := []struct {
		name     string
		existing *appsv1.Deployment
		scaling  *commonv1alpha1.HorizontalScaling
		static   *int32
		expected *int32
	}{
		{
			name:     "no scaling: static",
			existing: deploymentWithReplicas(3),
			static:   new(int32(5)),
			expected: new(int32(5)),
		},
		{
			name:     "no scaling and no static: defaults to 1",
			existing: deploymentWithReplicas(3),
			expected: new(int32(1)),
		},
		{
			name:     "no Deployment yet: minReplicas wins over static",
			scaling:  &commonv1alpha1.HorizontalScaling{MinReplicas: new(int32(2)), MaxReplicas: 5},
			static:   new(int32(3)),
			expected: new(int32(2)),
		},
		{
			name:     "no Deployment yet and no minReplicas: HPA default of 1",
			scaling:  &commonv1alpha1.HorizontalScaling{MaxReplicas: 5},
			static:   new(int32(3)),
			expected: new(int32(1)),
		},
		{
			name:     "HPA scaled above minReplicas: keeps current",
			existing: deploymentWithReplicas(4),
			scaling:  &commonv1alpha1.HorizontalScaling{MinReplicas: new(int32(2)), MaxReplicas: 5},
			static:   new(int32(3)),
			expected: new(int32(4)),
		},
		{
			name:     "below minReplicas: raised to minReplicas",
			existing: deploymentWithReplicas(1),
			scaling:  &commonv1alpha1.HorizontalScaling{MinReplicas: new(int32(2)), MaxReplicas: 5},
			expected: new(int32(2)),
		},
		{
			name:     "no minReplicas: keeps current",
			existing: deploymentWithReplicas(3),
			scaling:  &commonv1alpha1.HorizontalScaling{MaxReplicas: 5},
			expected: new(int32(3)),
		},
		{
			name:     "no minReplicas and Deployment at 0: raised to HPA default of 1",
			existing: deploymentWithReplicas(0),
			scaling:  &commonv1alpha1.HorizontalScaling{MaxReplicas: 5},
			expected: new(int32(1)),
		},
		{
			name:     "explicit minReplicas 0 and Deployment at 0: keeps 0",
			existing: deploymentWithReplicas(0),
			scaling:  &commonv1alpha1.HorizontalScaling{MinReplicas: new(int32(0)), MaxReplicas: 5},
			expected: new(int32(0)),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var objs []client.Object
			if tc.existing != nil {
				objs = append(objs, tc.existing)
			}
			owner := newOwner()
			owner.Spec.Deployment.Replicas = tc.static
			if tc.scaling != nil {
				owner.Spec.Deployment.Scaling = &operatorv1beta1.Scaling{HorizontalScaling: tc.scaling}
			}
			assert.Equal(t, tc.expected, hpa.DeploymentReplicas(owner, tc.existing))

			replicas, err := hpa.DesiredDeploymentReplicas(t.Context(), newClient(objs...), owner, key)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, replicas)
		})
	}
}

func TestDesiredDeploymentReplicas_ScaledThroughScaleSubresource(t *testing.T) {
	key := client.ObjectKey{Namespace: testNamespace, Name: deploymentName}
	replicasEntry := func(manager string, operation metav1.ManagedFieldsOperationType, subresource, fields string) metav1.ManagedFieldsEntry {
		return metav1.ManagedFieldsEntry{
			Manager:     manager,
			Operation:   operation,
			Subresource: subresource,
			APIVersion:  "apps/v1",
			FieldsType:  "FieldsV1",
			FieldsV1:    metav1.NewFieldsV1(fields),
		}
	}
	hpaScale := replicasEntry("kube-controller-manager", metav1.ManagedFieldsOperationUpdate, "scale", `{"f:spec":{"f:replicas":{}}}`)
	hpaOwner := func() *operatorv1beta1.DataPlane {
		owner := newOwner()
		owner.Spec.Deployment.Scaling = &operatorv1beta1.Scaling{
			HorizontalScaling: &commonv1alpha1.HorizontalScaling{MinReplicas: new(int32(2)), MaxReplicas: 5},
		}
		return owner
	}

	testCases := []struct {
		name  string
		owner *operatorv1beta1.DataPlane
		// current is the Deployment spec.replicas, 4 when nil.
		current       *int32
		managedFields []metav1.ManagedFieldsEntry
		expected      *int32
		expectedErr   bool
	}{
		{
			name:          "scaled through the scale subresource: left out",
			owner:         hpaOwner(),
			managedFields: []metav1.ManagedFieldsEntry{hpaScale},
			expected:      nil,
		},
		{
			name:          "scaled through the scale subresource to 0: raised to minReplicas",
			owner:         hpaOwner(),
			current:       new(int32(0)),
			managedFields: []metav1.ManagedFieldsEntry{hpaScale},
			expected:      new(int32(2)),
		},
		{
			name: "scaled through the scale subresource to 0 without minReplicas: raised to HPA default of 1",
			owner: func() *operatorv1beta1.DataPlane {
				owner := newOwner()
				owner.Spec.Deployment.Scaling = &operatorv1beta1.Scaling{
					HorizontalScaling: &commonv1alpha1.HorizontalScaling{MaxReplicas: 5},
				}
				return owner
			}(),
			current:       new(int32(0)),
			managedFields: []metav1.ManagedFieldsEntry{hpaScale},
			expected:      new(int32(1)),
		},
		{
			name:  "scaled through a regular update: keeps current",
			owner: hpaOwner(),
			managedFields: []metav1.ManagedFieldsEntry{
				replicasEntry("kubectl-edit", metav1.ManagedFieldsOperationUpdate, "", `{"f:spec":{"f:replicas":{}}}`),
			},
			expected: new(int32(4)),
		},
		{
			name:  "scale subresource entry without spec.replicas: keeps current",
			owner: hpaOwner(),
			managedFields: []metav1.ManagedFieldsEntry{
				replicasEntry("kube-controller-manager", metav1.ManagedFieldsOperationUpdate, "scale", `{"f:metadata":{}}`),
			},
			expected: new(int32(4)),
		},
		{
			name: "horizontal scaling removed: static replicas",
			owner: func() *operatorv1beta1.DataPlane {
				owner := newOwner()
				owner.Spec.Deployment.Replicas = new(int32(3))
				return owner
			}(),
			managedFields: []metav1.ManagedFieldsEntry{hpaScale},
			expected:      new(int32(3)),
		},
		{
			name:  "malformed managed fields: error",
			owner: hpaOwner(),
			managedFields: []metav1.ManagedFieldsEntry{
				replicasEntry("kube-controller-manager", metav1.ManagedFieldsOperationUpdate, "scale", `{`),
			},
			expectedErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// The fake client does not track managed fields of subresource
			// updates, so they are set on read.
			cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).
				WithObjects(&appsv1.Deployment{
					Namespace: testNamespace, Name: deploymentName,
					Spec: appsv1.DeploymentSpec{Replicas: lo.CoalesceOrEmpty(tc.current, new(int32(4)))},
				}).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if err := cl.Get(ctx, key, obj, opts...); err != nil {
							return err
						}
						obj.SetManagedFields(tc.managedFields)
						return nil
					},
				}).
				Build()

			replicas, err := hpa.DesiredDeploymentReplicas(t.Context(), cl, tc.owner, key)
			if tc.expectedErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, replicas)
		})
	}
}
