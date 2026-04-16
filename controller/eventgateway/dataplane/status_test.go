package dataplane

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// -----------------------------------------------------------------
// ensureReadyStatus
// -----------------------------------------------------------------

func Test_ensureReadyStatus(t *testing.T) {
	scheme := managerscheme.Get()

	egdp := func() *eventgatewayv1alpha1.KegDataPlane {
		return &eventgatewayv1alpha1.KegDataPlane{
			ObjectMeta: metav1.ObjectMeta{Namespace: testCASecretNamespace, Name: testDPName},
		}
	}

	deploy := func(ready, total int32) *appsv1.Deployment {
		d := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Namespace: testCASecretNamespace, Name: testDPName},
		}
		d.Status.Replicas = total
		d.Status.ReadyReplicas = ready
		return d
	}

	tests := []struct {
		name              string
		objects           []client.Object
		buildClient       func(base client.WithWatch) client.Client
		wantErr           bool
		wantReadyStatus   metav1.ConditionStatus
		wantReplicas      int32
		wantReadyReplicas int32
	}{
		{
			name:            "deployment not found: Ready=False with DependenciesNotReady",
			objects:         nil,
			wantReadyStatus: metav1.ConditionFalse,
		},
		{
			name:              "deployment exists but zero ready: Ready=False",
			objects:           []client.Object{deploy(0, 2)},
			wantReadyStatus:   metav1.ConditionFalse,
			wantReplicas:      2,
			wantReadyReplicas: 0,
		},
		{
			name:              "deployment has ready replicas: Ready=True",
			objects:           []client.Object{deploy(2, 2)},
			wantReadyStatus:   metav1.ConditionTrue,
			wantReplicas:      2,
			wantReadyReplicas: 2,
		},
		{
			name:              "rolling update: some ready replicas: stays Ready=True",
			objects:           []client.Object{deploy(1, 2)},
			wantReadyStatus:   metav1.ConditionTrue,
			wantReplicas:      2,
			wantReadyReplicas: 1,
		},
		{
			name: "GET error propagated",
			buildClient: func(base client.WithWatch) client.Client {
				return interceptor.NewClient(base, interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						return assert.AnError
					},
				})
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.objects...).
				WithStatusSubresource(tc.objects...).
				Build()
			var cl client.Client = base
			if tc.buildClient != nil {
				cl = tc.buildClient(base)
			}

			dp := egdp()
			err := ensureReadyStatus(context.Background(), cl, dp)

			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			cond := apimeta.FindStatusCondition(dp.Status.Conditions, string(eventgatewayv1alpha1.ReadyType))
			require.NotNil(t, cond, "Ready condition must be set")
			assert.Equal(t, tc.wantReadyStatus, cond.Status)

			assert.Equal(t, tc.wantReplicas, dp.Status.Replicas)
			assert.Equal(t, tc.wantReadyReplicas, dp.Status.ReadyReplicas)
		})
	}
}

// egdpWithType wraps makeEGDP with TypeMeta set, which is required by
// ApplyStatusIfChanged to determine the object's GVK.
func egdpWithType() *eventgatewayv1alpha1.KegDataPlane {
	e := makeEGDP()
	e.TypeMeta = metav1.TypeMeta{
		APIVersion: "eventgateway.konghq.com/v1alpha1",
		Kind:       "KegDataPlane",
	}
	return e
}

// -----------------------------------------------------------------
// applyStatus
// -----------------------------------------------------------------

func Test_applyStatus(t *testing.T) {
	scheme := managerscheme.Get()
	tc := managedfields.NewDeducedTypeConverter()

	newReconciler := func(cl client.Client, rec *events.FakeRecorder) *Reconciler {
		return &Reconciler{
			Client:        cl,
			typeConverter: tc,
			eventRecorder: rec,
		}
	}

	tests := []struct {
		name          string
		buildClient   func(base client.WithWatch) client.Client
		preObjects    bool
		modifyEGDP    func(*eventgatewayv1alpha1.KegDataPlane)
		incomingErr   error
		wantErrJoined bool
		wantEvent     string
	}{
		{
			name:        "object not in cluster: status apply fails and error is joined",
			buildClient: func(base client.WithWatch) client.Client { return base },
			// egdp not pre-created → Get inside ApplyStatusIfChanged returns not-found → error
			wantErrJoined: true,
			wantEvent:     "StatusPatchFailed",
		},
		{
			name:        "status first apply: object pre-created, StatusUpdated event emitted",
			buildClient: func(base client.WithWatch) client.Client { return base },
			preObjects:  true,
			wantEvent:   "StatusUpdated",
		},
		{
			name:        "StatusUpdated event emitted when condition present",
			buildClient: func(base client.WithWatch) client.Client { return base },
			preObjects:  true,
			modifyEGDP: func(e *eventgatewayv1alpha1.KegDataPlane) {
				e.Status.Conditions = append(e.Status.Conditions, metav1.Condition{
					Type:               string(eventgatewayv1alpha1.ReadyType),
					Status:             metav1.ConditionTrue,
					Reason:             "Ready",
					LastTransitionTime: metav1.Now(),
				})
			},
			wantEvent: "StatusUpdated",
		},
		{
			name: "apply error is joined with incoming error",
			buildClient: func(base client.WithWatch) client.Client {
				return interceptor.NewClient(base, interceptor.Funcs{
					SubResourceApply: func(ctx context.Context, c client.Client, subResourceName string, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
						return assert.AnError
					},
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						return c.Get(ctx, key, obj, opts...)
					},
				})
			},
			preObjects:    true,
			incomingErr:   assert.AnError,
			wantErrJoined: true,
			wantEvent:     "StatusPatchFailed",
		},
	}

	for _, testcase := range tests {
		t.Run(testcase.name, func(t *testing.T) {
			egdp := egdpWithType()
			if testcase.modifyEGDP != nil {
				testcase.modifyEGDP(egdp)
			}

			var objects []client.Object
			if testcase.preObjects {
				objects = append(objects, egdp)
			}

			base := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(objects...).
				Build()

			recorder := events.NewFakeRecorder(10)
			r := newReconciler(testcase.buildClient(base), recorder)

			result := errors.Join(testcase.incomingErr, r.applyStatus(context.Background(), logr.Discard(), egdp))

			if testcase.wantErrJoined {
				require.Error(t, result)
			} else {
				require.NoError(t, result)
			}

			if testcase.wantEvent != "" {
				select {
				case event := <-recorder.Events:
					assert.Contains(t, event, testcase.wantEvent)
				default:
					t.Errorf("expected event containing %q but channel was empty", testcase.wantEvent)
				}
			} else {
				assert.Empty(t, recorder.Events)
			}
		})
	}
}
