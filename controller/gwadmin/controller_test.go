package gwadmin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func readyCondition(gw *operatorv1alpha1.GWAdmin) *metav1.Condition {
	for i := range gw.Status.Conditions {
		if gw.Status.Conditions[i].Type == operatorv1alpha1.GWAdminConditionTypeReady {
			return &gw.Status.Conditions[i]
		}
	}
	return nil
}

func ownedBy(gw *operatorv1alpha1.GWAdmin, obj client.Object) client.Object {
	k8sutils.SetOwnerForObject(obj, gw)
	return obj
}

func TestFindForeignObject(t *testing.T) {
	gw := newGWAdmin(nil)
	ownedDeployment := ownedBy(gw, &appsv1.Deployment{Name: "gw", Namespace: testNamespace}).(*appsv1.Deployment)

	for _, tc := range []struct {
		name       string
		deployment *appsv1.Deployment
		objs       []client.Object
		want       string
	}{
		{name: "nothing exists yet"},
		{
			name:       "everything is owned",
			deployment: ownedDeployment,
			objs: []client.Object{
				ownedBy(gw, &corev1.Service{Name: "gw-api", Namespace: testNamespace}),
				ownedBy(gw, &corev1.Service{Name: "gw-cp", Namespace: testNamespace}),
			},
		},
		{
			name:       "foreign Deployment",
			deployment: &appsv1.Deployment{Name: "gw", Namespace: testNamespace},
			want:       `Deployment "gw" already exists and is not managed by this GWAdmin`,
		},
		{
			name: "Deployment owned, not controlled",
			deployment: &appsv1.Deployment{Name: "gw", Namespace: testNamespace, OwnerReferences: []metav1.OwnerReference{{
				APIVersion: operatorv1alpha1.SchemeGroupVersion.String(), Kind: "GWAdmin", Name: gw.Name, UID: gw.UID,
			}}},
			want: `Deployment "gw" already exists and is not managed by this GWAdmin`,
		},
		{
			name:       "foreign API Service",
			deployment: ownedDeployment,
			objs:       []client.Object{&corev1.Service{Name: "gw-api", Namespace: testNamespace}},
			want:       `Service "gw-api" already exists and is not managed by this GWAdmin`,
		},
		{
			name: "foreign cp Service",
			objs: []client.Object{
				ownedBy(gw, &corev1.Service{Name: "gw-api", Namespace: testNamespace}),
				&corev1.Service{Name: "gw-cp", Namespace: testNamespace},
			},
			want: `Service "gw-cp" already exists and is not managed by this GWAdmin`,
		},
		{
			name: "Service controlled by another GWAdmin with the same name",
			objs: []client.Object{&corev1.Service{Name: "gw-cp", Namespace: testNamespace, OwnerReferences: []metav1.OwnerReference{{
				APIVersion: operatorv1alpha1.SchemeGroupVersion.String(), Kind: "GWAdmin", Name: gw.Name, UID: "other-uid",
				Controller: new(true),
			}}}},
			want: `Service "gw-cp" is still owned by a deleted GWAdmin named "gw": waiting for the garbage collector to delete it`,
		},
		{
			name: "Service in another namespace",
			objs: []client.Object{&corev1.Service{Name: "gw-cp", Namespace: "other"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			r := newReconciler(t, &now, append(tc.objs, gw)...)
			got, err := r.findForeignObject(t.Context(), gw, tc.deployment)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("Get error", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return errors.New("boom")
			},
		}).Build()
		r := &Reconciler{Client: cl}
		_, err := r.findForeignObject(t.Context(), gw, nil)
		require.ErrorContains(t, err, "boom")
	})
}

// newReconcileClient returns a fake client serving the status subresource of
// GWAdmins, with interceptors.
func newReconcileClient(funcs interceptor.Funcs, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(managerscheme.Get()).
		WithStatusSubresource(&operatorv1alpha1.GWAdmin{}).
		WithObjects(objs...).
		WithInterceptorFuncs(funcs).
		Build()
}

func TestReconcile_ForeignObjectIsAResourceConflict(t *testing.T) {
	for _, tc := range []struct {
		name    string
		foreign client.Object
	}{
		{"Deployment", &appsv1.Deployment{Name: "gw", Namespace: testNamespace, Labels: map[string]string{"app": "someone-else"}}},
		{"API Service", &corev1.Service{Name: "gw-api", Namespace: testNamespace, Labels: map[string]string{"app": "someone-else"}}},
		{"cp Service", &corev1.Service{Name: "gw-cp", Namespace: testNamespace, Labels: map[string]string{"app": "someone-else"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			foreign := tc.foreign
			now := time.Now()
			var applied []string
			cl := newReconcileClient(interceptor.Funcs{
				Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
					applied = append(applied, "apply")
					return c.Apply(ctx, obj, opts...)
				},
			}, newGWAdmin(nil), foreign)
			r := &Reconciler{Client: cl, ClusterDomain: "cluster.local", Now: func() time.Time { return now }}

			gw := &operatorv1alpha1.GWAdmin{}
			require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: testNamespace, Name: "gw"}, gw))
			before := foreign.DeepCopyObject().(client.Object)
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(foreign), before))

			res, err := r.Reconcile(t.Context(), gw)
			require.NoError(t, err)
			assert.Equal(t, conflictRequeue, res.RequeueAfter)
			assert.Empty(t, applied, "nothing is applied over the foreign object")

			live := &operatorv1alpha1.GWAdmin{}
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(gw), live))
			cond := readyCondition(live)
			require.NotNil(t, cond, "status patched")
			assert.Equal(t, metav1.ConditionFalse, cond.Status)
			assert.Equal(t, operatorv1alpha1.GWAdminReasonResourceConflict, cond.Reason)
			assert.Contains(t, cond.Message, foreign.GetName())
			assert.Equal(t, metav1.ConditionTrue, certificateCondition(live).Status, "certificates are provisioned")

			after := foreign.DeepCopyObject().(client.Object)
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(foreign), after))
			assert.Equal(t, before.GetResourceVersion(), after.GetResourceVersion(), "left alone")
			assert.Empty(t, after.GetOwnerReferences())
		})
	}
}

func TestReconcile_FailedToApply(t *testing.T) {
	now := time.Now()
	cl := newReconcileClient(interceptor.Funcs{
		Apply: func(context.Context, client.WithWatch, runtime.ApplyConfiguration, ...client.ApplyOption) error {
			return errors.New("apply refused")
		},
	}, newGWAdmin(nil))
	r := &Reconciler{Client: cl, ClusterDomain: "cluster.local", Now: func() time.Time { return now }}

	gw := &operatorv1alpha1.GWAdmin{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: testNamespace, Name: "gw"}, gw))
	_, err := r.Reconcile(t.Context(), gw)
	require.ErrorContains(t, err, "apply refused")

	live := &operatorv1alpha1.GWAdmin{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(gw), live))
	cond := readyCondition(live)
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonFailedToApply, cond.Reason)
	assert.Contains(t, cond.Message, "apply refused")
}

func TestReconcile_StopRequeue(t *testing.T) {
	now := time.Now()
	gwObj := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA("missing")})
	cl := newReconcileClient(interceptor.Funcs{}, gwObj)
	r := &Reconciler{Client: cl, ClusterDomain: "cluster.local", Now: func() time.Time { return now }}

	gw := &operatorv1alpha1.GWAdmin{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(gwObj), gw))
	res, err := r.Reconcile(t.Context(), gw)
	require.NoError(t, err, "a user error is reported in the status, not retried with backoff")
	assert.Equal(t, maxRequeue, res.RequeueAfter)

	live := &operatorv1alpha1.GWAdmin{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(gw), live))
	assert.Equal(t, operatorv1alpha1.GWAdminReasonDependenciesNotReady, readyCondition(live).Reason)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonSecretRefNotFound, certificateCondition(live).Reason)
}
