package patch

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

func TestApplyPatchIfNonEmpty(t *testing.T) {
	testcases := []struct {
		name      string
		dataPlane *operatorv1beta1.DataPlane
		// No easy way to make the function type in struct generic to test update for
		// other types of objects e.g. Deployments generically.
		// There might be a way to achieve this but with serious refactoring of
		// these tests.
		generateHPAFunc func(t *testing.T, dataplane *operatorv1beta1.DataPlane) *autoscalingv2.HorizontalPodAutoscaler
		changeHPAFunc   func(t *testing.T, hpa *autoscalingv2.HorizontalPodAutoscaler)
		assertHPAFunc   func(t *testing.T, hpa *autoscalingv2.HorizontalPodAutoscaler)
		updated         bool
		wantErr         bool
		wantResult      op.Result
	}{
		{
			name: "when no changes are needed no patch is being made",
			dataPlane: &operatorv1beta1.DataPlane{
				APIVersion: operatorv1beta1.SchemeGroupVersion.Group + "/" + operatorv1beta1.SchemeGroupVersion.Version,
				Kind:       "DataPlane",
				Name:       "test-dataplane",
				Namespace:  "test-namespace",
				UID:        types.UID(uuid.NewString()),
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								Scaling: &operatorv1beta1.Scaling{
									HorizontalScaling: &operatorv1beta1.HorizontalScaling{
										MaxReplicas: 3,
										Metrics: []autoscalingv2.MetricSpec{
											{
												Type: autoscalingv2.ResourceMetricSourceType,
												Resource: &autoscalingv2.ResourceMetricSource{
													Name: "cpu",
													Target: autoscalingv2.MetricTarget{
														Type:               autoscalingv2.UtilizationMetricType,
														AverageUtilization: new(int32(20)),
													},
												},
											},
										},
									},
								},
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  consts.DataPlaneProxyContainerName,
												Image: "kong:3.4",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			generateHPAFunc: func(t *testing.T, dataplane *operatorv1beta1.DataPlane) *autoscalingv2.HorizontalPodAutoscaler {
				hs := dataplane.Spec.Deployment.Scaling.HorizontalScaling
				hpa, err := k8sresources.GenerateHPA(dataplane, &commonv1alpha1.HorizontalScaling{
					MinReplicas: hs.MinReplicas,
					MaxReplicas: hs.MaxReplicas,
					Metrics:     hs.Metrics,
					Behavior:    hs.Behavior,
				}, "test-deployment")
				require.NoError(t, err)
				return hpa
			},
			assertHPAFunc: func(t *testing.T, hpa *autoscalingv2.HorizontalPodAutoscaler) {
				require.Equal(t, "test-deployment", hpa.Spec.ScaleTargetRef.Name)
			},
			updated:    false,
			wantErr:    false,
			wantResult: op.Noop,
		},
		{
			name: "when changes are applied to the generated HPA a patch is being made",
			dataPlane: &operatorv1beta1.DataPlane{
				APIVersion: operatorv1beta1.SchemeGroupVersion.Group + "/" + operatorv1beta1.SchemeGroupVersion.Version,
				Kind:       "DataPlane",
				Name:       "test-dataplane",
				Namespace:  "test-namespace",
				UID:        types.UID(uuid.NewString()),
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								Scaling: &operatorv1beta1.Scaling{
									HorizontalScaling: &operatorv1beta1.HorizontalScaling{
										MaxReplicas: 3,
										Metrics: []autoscalingv2.MetricSpec{
											{
												Type: autoscalingv2.ResourceMetricSourceType,
												Resource: &autoscalingv2.ResourceMetricSource{
													Name: "cpu",
													Target: autoscalingv2.MetricTarget{
														Type:               autoscalingv2.UtilizationMetricType,
														AverageUtilization: new(int32(20)),
													},
												},
											},
										},
									},
								},
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  consts.DataPlaneProxyContainerName,
												Image: "kong:3.4",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			generateHPAFunc: func(t *testing.T, dataplane *operatorv1beta1.DataPlane) *autoscalingv2.HorizontalPodAutoscaler {
				hs := dataplane.Spec.Deployment.Scaling.HorizontalScaling
				hpa, err := k8sresources.GenerateHPA(dataplane, &commonv1alpha1.HorizontalScaling{
					MinReplicas: hs.MinReplicas,
					MaxReplicas: hs.MaxReplicas,
					Metrics:     hs.Metrics,
					Behavior:    hs.Behavior,
				}, "test-deployment")
				require.NoError(t, err)
				return hpa
			},
			changeHPAFunc: func(t *testing.T, hpa *autoscalingv2.HorizontalPodAutoscaler) {
				hpa.Spec.MinReplicas = new(int32(2))
			},
			assertHPAFunc: func(t *testing.T, hpa *autoscalingv2.HorizontalPodAutoscaler) {
				require.Equal(t, int32(2), *hpa.Spec.MinReplicas)
				require.Equal(t, "test-deployment", hpa.Spec.ScaleTargetRef.Name)
			},
			updated:    true,
			wantErr:    false,
			wantResult: op.Updated,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()

			require.NoError(t, operatorv1beta1.AddToScheme(scheme))
			require.NoError(t, autoscalingv2.AddToScheme(scheme))

			log := logr.Discard()
			hpa := tc.generateHPAFunc(t, tc.dataPlane)
			old := hpa.DeepCopy()
			if tc.changeHPAFunc != nil {
				tc.changeHPAFunc(t, hpa)
			}

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.dataPlane, hpa).
				Build()

			result, _, err := ApplyPatchIfNotEmpty(t.Context(), fakeClient, log, hpa, old, tc.updated)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantResult, result)
			if tc.assertHPAFunc != nil {
				tc.assertHPAFunc(t, hpa)
			}
		})
	}
}

// TestApplyStatusPatchIfNotEmptyStaleCopyConflicts verifies that a status patch
// built from a stale copy carries the optimistic-lock resourceVersion, and that
// the API server rejecting it leaves conditions written by other actors intact.
// A JSON merge patch replaces the whole conditions array, so without the lock a
// stale patch would delete the other actor's condition.
func TestApplyStatusPatchIfNotEmptyStaleCopyConflicts(t *testing.T) {
	foreignCondition := metav1.Condition{
		Type:   "example.com/Foreign",
		Status: metav1.ConditionTrue,
		Reason: "Seeded",
	}

	// Live state on the server: a newer write than the one the stale copy saw.
	live := &gwtypes.Gateway{
		Name:            "test-gateway",
		Namespace:       "test-namespace",
		ResourceVersion: "10",
		Generation:      1,
		Status: gatewayv1.GatewayStatus{
			Conditions: []metav1.Condition{foreignCondition},
		},
	}
	// Stale copy: older resourceVersion, no foreign condition.
	stale := live.DeepCopy()
	stale.ResourceVersion = "9"
	stale.Status.Conditions = nil

	updated := stale.DeepCopy()
	updated.Status.Conditions = []metav1.Condition{
		{
			Type:               string(gatewayv1.GatewayConditionAccepted),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.GatewayReasonAccepted),
			ObservedGeneration: 1,
		},
	}

	var conflictSeen bool
	cl := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(live).
		WithStatusSubresource(live).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(
				ctx context.Context,
				cl client.Client,
				_ string,
				obj client.Object,
				p client.Patch,
				opts ...client.SubResourcePatchOption,
			) error {
				data, err := p.Data(obj)
				if err != nil {
					return err
				}
				var patchData struct {
					Metadata struct {
						ResourceVersion string `json:"resourceVersion"`
					} `json:"metadata"`
				}
				if err := json.Unmarshal(data, &patchData); err != nil {
					return err
				}
				if patchData.Metadata.ResourceVersion == "" {
					t.Error("expected the patch to carry metadata.resourceVersion (optimistic lock)")
				}
				liveObj := &gwtypes.Gateway{}
				if err := cl.Get(ctx, client.ObjectKeyFromObject(obj), liveObj); err != nil {
					return err
				}
				if patchData.Metadata.ResourceVersion != liveObj.ResourceVersion {
					conflictSeen = true
					return apierrors.NewConflict(
						schema.GroupResource{Group: gatewayv1.GroupName, Resource: "gateways"},
						obj.GetName(),
						errors.New("the object has been modified"),
					)
				}
				return cl.Status().Patch(ctx, obj, p, opts...)
			},
		}).
		Build()

	res, err := ApplyStatusPatchIfNotEmpty(
		t.Context(),
		cl,
		logr.Discard(),
		updated,
		stale,
		client.MergeFromWithOptimisticLock{},
	)
	// The helper returns the conflict, and the reconcilers turn it into a
	// requeue so the next reconcile reads the current object.
	require.Error(t, err)
	assert.True(t, apierrors.IsConflict(err))
	assert.True(t, conflictSeen, "expected the stale patch to conflict on resourceVersion")
	assert.Equal(t, op.Noop, res)

	persisted := &gwtypes.Gateway{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(live), persisted))
	require.Len(t, persisted.Status.Conditions, 1)
	assert.Equal(t, foreignCondition.Type, persisted.Status.Conditions[0].Type)
}

// TestApplyStatusPatchIfNotEmptyNoopWithOptimisticLock verifies that the
// optimistic lock does not turn an unchanged object into a patch: writing the
// resourceVersion alone would bump it and re-enqueue the object forever.
func TestApplyStatusPatchIfNotEmptyNoopWithOptimisticLock(t *testing.T) {
	gw := &gwtypes.Gateway{
		Name:            "test-gateway",
		Namespace:       "test-namespace",
		ResourceVersion: "10",
		Generation:      1,
	}

	patched := false
	cl := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(gw).
		WithStatusSubresource(gw).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(
				_ context.Context,
				_ client.Client,
				_ string,
				_ client.Object,
				_ client.Patch,
				_ ...client.SubResourcePatchOption,
			) error {
				patched = true
				return nil
			},
		}).
		Build()

	res, err := ApplyStatusPatchIfNotEmpty(
		t.Context(),
		cl,
		logr.Discard(),
		gw.DeepCopy(),
		gw.DeepCopy(),
		client.MergeFromWithOptimisticLock{},
	)
	require.NoError(t, err)
	assert.False(t, patched, "an unchanged object must not be patched")
	assert.Equal(t, op.Noop, res)
}

// TestApplyStatusPatchIfNotEmptyIgnoresResourceVersionOnlyDiff verifies that a
// status write earlier in the same reconcile, which moves the resourceVersion,
// does not make the next write look non-empty. Writing the version alone would
// bump it again and keep re-enqueueing the object.
func TestApplyStatusPatchIfNotEmptyIgnoresResourceVersionOnlyDiff(t *testing.T) {
	base := &gwtypes.Gateway{
		Name:            "test-gateway",
		Namespace:       "test-namespace",
		ResourceVersion: "1",
		Generation:      1,
	}
	existing := base.DeepCopy()
	existing.ResourceVersion = "2"

	patched := false
	cl := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(existing).
		WithStatusSubresource(existing).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(
				_ context.Context,
				_ client.Client,
				_ string,
				_ client.Object,
				_ client.Patch,
				_ ...client.SubResourcePatchOption,
			) error {
				patched = true
				return nil
			},
		}).
		Build()

	res, err := ApplyStatusPatchIfNotEmpty(
		t.Context(),
		cl,
		logr.Discard(),
		existing,
		base,
		client.MergeFromWithOptimisticLock{},
	)
	require.NoError(t, err)
	assert.False(t, patched, "a resourceVersion-only difference must not be patched")
	assert.Equal(t, op.Noop, res)
}
