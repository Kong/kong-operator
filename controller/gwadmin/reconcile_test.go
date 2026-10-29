package gwadmin

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/pki"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// reconcileEnv runs Reconcile against a fake client that serves server-side
// apply through a type converter, so that a Deployment can be applied again.
type reconcileEnv struct {
	cl  client.WithWatch
	r   *Reconciler
	now time.Time
	rec *events.FakeRecorder
}

// withoutStatusOwnership wraps the Get of funcs so that the managed fields
// returned don't claim .status, nor an empty .spec.strategy, for an apply of
// the main resource: the fake client stores typed objects, whose zero status
// and strategy it then records as applied, which the API server never does.
// Left in, they make every apply after the first look like a change.
func withoutStatusOwnership(funcs interceptor.Funcs) interceptor.Funcs {
	get := funcs.Get
	funcs.Get = func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
		var err error
		if get != nil {
			err = get(ctx, c, key, obj, opts...)
		} else {
			err = c.Get(ctx, key, obj, opts...)
		}
		if err != nil {
			return err
		}
		mf := obj.GetManagedFields()
		for i := range mf {
			if mf[i].Subresource != "" || mf[i].FieldsV1 == nil {
				continue
			}
			fields := map[string]any{}
			if json.Unmarshal(mf[i].FieldsV1.GetRawBytes(), &fields) != nil {
				continue
			}
			delete(fields, "f:status")
			if spec, ok := fields["f:spec"].(map[string]any); ok {
				if strategy, ok := spec["f:strategy"].(map[string]any); ok && len(strategy) <= 1 {
					delete(spec, "f:strategy")
				}
			}
			raw, err := json.Marshal(fields)
			if err != nil {
				return err
			}
			mf[i].FieldsV1.SetRawBytes(raw)
		}
		obj.SetManagedFields(mf)
		return nil
	}
	return funcs
}

func newReconcileEnv(t *testing.T, funcs interceptor.Funcs, objs ...client.Object) *reconcileEnv {
	t.Helper()
	funcs = withoutStatusOwnership(funcs)
	tc := managedfields.NewDeducedTypeConverter()
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).
		WithTypeConverters(tc).
		WithReturnManagedFields().
		WithStatusSubresource(&operatorv1alpha1.GWAdmin{}).
		WithObjects(objs...).
		WithInterceptorFuncs(funcs).
		Build()
	e := &reconcileEnv{cl: cl, now: time.Now(), rec: events.NewFakeRecorder(100)}
	e.r = &Reconciler{
		Client: cl, ClusterDomain: "cluster.local", SecretLabelSelector: "konghq.com/secret",
		OperatorNamespace: testOperatorNamespace,
		TypeConverter:     tc, Now: func() time.Time { return e.now }, eventRecorder: e.rec,
	}
	return e
}

func (e *reconcileEnv) gw(t *testing.T) *operatorv1alpha1.GWAdmin {
	t.Helper()
	gw := &operatorv1alpha1.GWAdmin{}
	require.NoError(t, e.cl.Get(t.Context(), client.ObjectKey{Namespace: testNamespace, Name: "gw"}, gw))
	return gw
}

// reconcile runs Reconcile on the live GWAdmin and returns it as patched.
func (e *reconcileEnv) reconcile(t *testing.T) (ctrl.Result, *operatorv1alpha1.GWAdmin, error) {
	t.Helper()
	res, err := e.r.Reconcile(t.Context(), e.gw(t))
	return res, e.gw(t), err
}

func (e *reconcileEnv) deployment(t *testing.T) *appsv1.Deployment {
	t.Helper()
	d := &appsv1.Deployment{}
	require.NoError(t, e.cl.Get(t.Context(), client.ObjectKey{Namespace: testNamespace, Name: "gw"}, d))
	return d
}

// markRolled makes the Deployment status report that every pod runs its
// current template, as the Deployment controller would.
func (e *reconcileEnv) markRolled(t *testing.T) {
	t.Helper()
	d := e.deployment(t)
	replicas := int32(1)
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	e.patchDeploymentStatus(t, d, appsv1.DeploymentStatus{
		ObservedGeneration: d.Generation, Replicas: replicas, UpdatedReplicas: replicas,
		ReadyReplicas: replicas, AvailableReplicas: replicas, TerminatingReplicas: new(int32(0)),
	})
}

// patchDeploymentStatus sets the status of the Deployment d, as read, with a
// merge patch of the status subresource.
func (e *reconcileEnv) patchDeploymentStatus(t *testing.T, d *appsv1.Deployment, status appsv1.DeploymentStatus) {
	t.Helper()
	base := d.DeepCopy()
	d.Status = status
	require.NoError(t, e.cl.Status().Patch(t.Context(), d, client.MergeFrom(base)))
}

func (e *reconcileEnv) setImage(t *testing.T, image string) {
	t.Helper()
	gw := e.gw(t)
	gw.Spec.Deployment = &operatorv1alpha1.GWAdminDeploymentOptions{PodTemplateSpec: &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: consts.GWAdminContainerName, Image: image}}},
	}}
	require.NoError(t, e.cl.Update(t.Context(), gw))
}

func (e *reconcileEnv) image(t *testing.T) string {
	t.Helper()
	d := e.deployment(t)
	c := k8sutils.GetPodContainerByName(&d.Spec.Template.Spec, consts.GWAdminContainerName)
	require.NotNil(t, c)
	return c.Image
}

func requireReady(t *testing.T, gw *operatorv1alpha1.GWAdmin, status metav1.ConditionStatus, reason string) *metav1.Condition {
	t.Helper()
	cond := readyCondition(gw)
	require.NotNil(t, cond)
	require.Equal(t, status, cond.Status, "Ready: %s: %s", cond.Reason, cond.Message)
	require.Equal(t, reason, cond.Reason, "Ready: %s", cond.Message)
	return cond
}

// TestReconcile_ReadySemantics checks that Ready only turns True once the
// Deployment observed the spec just applied: a Deployment whose status still
// describes the previous spec must not make a spec change look Ready.
func TestReconcile_ReadySemantics(t *testing.T) {
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(nil))

	res, gw, err := e.reconcile(t)
	require.NoError(t, err)
	requireReady(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonWaitingToBecomeReady)
	assert.Equal(t, pki.MaxRenewalRequeue, res.RequeueAfter)

	// Created, not observed yet.
	_, gw, err = e.reconcile(t)
	require.NoError(t, err)
	if d := e.deployment(t); d.Status.ObservedGeneration < d.Generation {
		requireReady(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonWaitingToBecomeReady)
	}

	e.markRolled(t)
	_, gw, err = e.reconcile(t)
	require.NoError(t, err)
	requireReady(t, gw, metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonReady)

	// A spec change: the Deployment status still reports the previous spec
	// as observed, yet the new one was only just applied.
	e.setImage(t, "custom/gw-admin:2")
	d := e.deployment(t)
	require.GreaterOrEqual(t, d.Status.ObservedGeneration, d.Generation,
		"the Deployment status looks observed: only the apply tells the spec changed")
	_, gw, err = e.reconcile(t)
	require.NoError(t, err)
	assert.Equal(t, "custom/gw-admin:2", e.image(t))
	requireReady(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonWaitingToBecomeReady)

	e.markRolled(t)
	_, gw, err = e.reconcile(t)
	require.NoError(t, err)
	requireReady(t, gw, metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonReady)

	// The Deployment didn't observe its spec.
	d = e.deployment(t)
	st := d.Status
	st.ObservedGeneration = d.Generation - 1
	e.patchDeploymentStatus(t, d, st)
	_, gw, err = e.reconcile(t)
	require.NoError(t, err)
	requireReady(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonWaitingToBecomeReady)
}

// TestReconcile_StopKeepsApplying checks that a user error on the TLS
// material doesn't freeze the Deployment while the pods can still mount the
// material in place.
func TestReconcile_StopKeepsApplying(t *testing.T) {
	now := time.Now()
	src := userCPSecretFrom(t, userCASecret(t, "public"), now, 24*time.Hour, false)
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()}), src)

	_, _, err := e.reconcile(t)
	require.NoError(t, err)
	e.markRolled(t)
	_, gw, err := e.reconcile(t)
	require.NoError(t, err)
	requireReady(t, gw, metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonReady)
	drainEvents(e.rec)

	// The user's cp Secret now holds a certificate authority, and the spec
	// changes.
	live := &corev1.Secret{}
	require.NoError(t, e.cl.Get(t.Context(), client.ObjectKeyFromObject(src), live))
	live.Data = userCASecret(t, "not a server").Data
	require.NoError(t, e.cl.Update(t.Context(), live))
	e.setImage(t, "custom/gw-admin:2")

	res, gw, err := e.reconcile(t)
	require.NoError(t, err, "a user error is reported in the status")
	assert.Equal(t, pki.MaxRenewalRequeue, res.RequeueAfter)
	assert.Equal(t, "custom/gw-admin:2", e.image(t), "the Deployment is still applied")
	cert := certificateCondition(gw)
	require.NotNil(t, cert)
	assert.Equal(t, metav1.ConditionFalse, cert.Status)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonInvalidSecret, cert.Reason)
	ready := requireReady(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonDependenciesNotReady)
	assert.Equal(t, cert.Message, ready.Message)
	assert.NotNil(t, gw.Status.CP, "the status is still published")

	evts := drainEvents(e.rec)
	assert.True(t, hasEvent(evts, "Warning InvalidSecret"), "events: %v", evts)
	assert.False(t, hasEvent(evts, "Warning DependenciesNotReady"), "events: %v", evts)

	// No new warning while nothing changes.
	_, _, err = e.reconcile(t)
	require.NoError(t, err)
	assert.Empty(t, drainEvents(e.rec))
}

func TestReconcile_StopWithoutMountableMaterial(t *testing.T) {
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA("missing")}))
	res, gw, err := e.reconcile(t)
	require.NoError(t, err)
	assert.Equal(t, pki.MaxRenewalRequeue, res.RequeueAfter)
	requireReady(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonDependenciesNotReady)
	err = e.cl.Get(t.Context(), client.ObjectKey{Namespace: testNamespace, Name: "gw"}, &appsv1.Deployment{})
	assert.True(t, apierrors.IsNotFound(err), "no Deployment without the material it mounts: %v", err)
}

// TestReconcile_StalledRotation checks that, with the leaves blocked by a
// rollout that doesn't progress, Ready reports it while the Deployment keeps
// being applied.
func TestReconcile_StalledRotation(t *testing.T) {
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(nil))
	_, _, err := e.reconcile(t)
	require.NoError(t, err)
	e.markRolled(t)
	_, _, err = e.reconcile(t)
	require.NoError(t, err)

	// Rotate the certificate authorities: the API certificates must wait for
	// the pods to trust the new API CA.
	gw := e.gw(t)
	gw.Annotations = map[string]string{consts.GWAdminRotateCAsAnnotation: "1"}
	require.NoError(t, e.cl.Update(t.Context(), gw))

	// Phase 1 applies a Deployment trusting both CAs; it never rolls out.
	_, gw, err = e.reconcile(t)
	require.NoError(t, err)
	requireReady(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonWaitingToBecomeReady)
	trust := e.deployment(t).Spec.Template.Annotations[consts.GWAdminTrustChecksumAnnotation]
	d := e.deployment(t)
	st := d.Status
	st.UpdatedReplicas = 0
	e.patchDeploymentStatus(t, d, st)

	api := &corev1.Secret{}
	require.NoError(t, e.cl.Get(t.Context(), client.ObjectKey{Namespace: testNamespace, Name: "gw-api-tls"}, api))
	leaf := leafCertificate(api)
	e.now = renewalTime(leaf).Add(time.Minute)
	e.setImage(t, "custom/gw-admin:2")
	drainEvents(e.rec)

	res, gw, err := e.reconcile(t)
	require.NoError(t, err)
	cert := certificateCondition(gw)
	require.Equal(t, operatorv1alpha1.GWAdminReasonCARotationBlocked, cert.Reason)
	ready := requireReady(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonDependenciesNotReady)
	assert.Equal(t, cert.Message, ready.Message)
	assert.Equal(t, "custom/gw-admin:2", e.image(t), "the Deployment is still applied")
	assert.Equal(t, trust, e.deployment(t).Spec.Template.Annotations[consts.GWAdminTrustChecksumAnnotation])
	assert.Positive(t, res.RequeueAfter)
	assert.LessOrEqual(t, res.RequeueAfter, leaf.NotAfter.Sub(e.now))
	assert.True(t, hasEvent(drainEvents(e.rec), "Warning CARotationBlocked"))
}

func TestReconcile_CacheStale(t *testing.T) {
	gwObj := newGWAdmin(nil)
	gwObj.Status.Conditions = []metav1.Condition{{
		Type: operatorv1alpha1.GWAdminConditionTypeReady, Status: metav1.ConditionTrue,
		Reason: operatorv1alpha1.GWAdminReasonReady, LastTransitionTime: metav1.Now(),
	}}
	e := newReconcileEnv(t, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if obj.GetName() == "gwadmin-gw-uid-api-ca" {
				return apierrors.NewAlreadyExists(corev1.Resource("secrets"), obj.GetName())
			}
			return c.Create(ctx, obj, opts...)
		},
	}, gwObj)
	before := e.gw(t).Status

	res, gw, err := e.reconcile(t)
	require.NoError(t, err)
	assert.Equal(t, time.Second, res.RequeueAfter)
	assert.Equal(t, before, gw.Status, "the conditions are left alone")
	assert.Empty(t, drainEvents(e.rec))
}

func TestReconcile_TransientErrorKeepsConditions(t *testing.T) {
	gwObj := newGWAdmin(nil)
	gwObj.Status.Conditions = []metav1.Condition{{
		Type: operatorv1alpha1.GWAdminConditionTypeReady, Status: metav1.ConditionTrue,
		Reason: operatorv1alpha1.GWAdminReasonReady, LastTransitionTime: metav1.Now(),
	}}
	e := newReconcileEnv(t, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.Secret); ok {
				return errors.New("etcd is down")
			}
			return c.Get(ctx, key, obj, opts...)
		},
	}, gwObj)
	before := e.gw(t).Status

	_, gw, err := e.reconcile(t)
	require.ErrorContains(t, err, "etcd is down")
	assert.Equal(t, before, gw.Status)
}

func TestReconcile_NormalEvents(t *testing.T) {
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(nil))
	_, _, err := e.reconcile(t)
	require.NoError(t, err)
	evts := drainEvents(e.rec)
	assert.True(t, hasEvent(evts, "Normal CACreated"), "%v", evts)
	for _, name := range []string{"gw-api-tls", "gw-cp-tls", "gwadmin-gw-uid-operator-client-tls"} {
		assert.True(t, hasEvent(evts, "Normal CertificateIssued Issued certificate "+name+" (missing)"), "%v", evts)
	}
	assert.False(t, hasEvent(evts, "Normal CARotationStarted"), "the first CA is no rotation: %v", evts)
	assert.False(t, hasEvent(evts, "Warning"), "%v", evts)

	_, _, err = e.reconcile(t)
	require.NoError(t, err)
	assert.Empty(t, drainEvents(e.rec), "nothing happens on the next pass")

	// The CA is renewed.
	ca := &corev1.Secret{}
	require.NoError(t, e.cl.Get(t.Context(), client.ObjectKey{Namespace: testOperatorNamespace, Name: "gwadmin-gw-uid-cp-client-ca"}, ca))
	e.now = renewalTime(caCert(t, ca)).Add(time.Minute)
	_, _, err = e.reconcile(t)
	require.NoError(t, err)
	evts = drainEvents(e.rec)
	assert.True(t, hasEvent(evts, "Normal CARenewed"), "%v", evts)
	assert.True(t, hasEvent(evts, "Normal CARotationStarted"), "%v", evts)
}

func TestReconcile_ManualCAPublishedEvent(t *testing.T) {
	userCA := userCASecret(t, "user")
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(nil), userCA)
	_, _, err := e.reconcile(t)
	require.NoError(t, err)
	autoCA := caCert(t, publishedCASecret(t, e.cl))

	gw := e.gw(t)
	gw.Spec.TLS = &operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)}
	require.NoError(t, e.cl.Update(t.Context(), gw))
	drainEvents(e.rec)

	// Phase 1: the Deployment, never rolled, now trusts both: the Automatic
	// CA stays published.
	e.markRolled(t)
	_, _, err = e.reconcile(t)
	require.NoError(t, err)
	assert.False(t, hasEvent(drainEvents(e.rec), "Normal CAPublished"))
	assert.True(t, caCert(t, publishedCASecret(t, e.cl)).Equal(autoCA))

	// The pods trust the Manual CA: it is published.
	e.markRolled(t)
	_, _, err = e.reconcile(t)
	require.NoError(t, err)
	assert.True(t, hasEvent(drainEvents(e.rec), "Normal CAPublished"))
	assert.True(t, caCert(t, publishedCASecret(t, e.cl)).Equal(caCert(t, userCA)))
}

// publishedCASecret returns the Secret the cp client CA is published in.
func publishedCASecret(t *testing.T, cl client.Client) *corev1.Secret {
	t.Helper()
	s := &corev1.Secret{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: testOperatorNamespace, Name: "gwadmin-gw-uid-cp-client-ca"}, s))
	return s
}

func TestFindForeignObject_HPA(t *testing.T) {
	gw := newGWAdmin(nil)
	foreignHPA := &autoscalingv2.HorizontalPodAutoscaler{Name: "gw", Namespace: testNamespace}
	now := time.Now()

	r := newReconciler(t, &now, gw, foreignHPA)
	got, err := r.findForeignObject(t.Context(), gw, nil)
	require.NoError(t, err)
	assert.Empty(t, got, "not looked at without scaling")

	scaled := gw.DeepCopy()
	scaled.Spec.Deployment = &operatorv1alpha1.GWAdminDeploymentOptions{Scaling: &operatorv1alpha1.GWAdminScaling{
		HorizontalScaling: &commonv1alpha1.HorizontalScaling{MaxReplicas: 3},
	}}
	got, err = r.findForeignObject(t.Context(), scaled, nil)
	require.NoError(t, err)
	assert.Equal(t, `HorizontalPodAutoscaler "gw" already exists and is not managed by this GWAdmin`, got)

	owned := ownedBy(gw, &autoscalingv2.HorizontalPodAutoscaler{Name: "gw", Namespace: testNamespace})
	r = newReconciler(t, &now, gw, owned)
	got, err = r.findForeignObject(t.Context(), scaled, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestWarnOnFailedConditions(t *testing.T) {
	cond := func(typ string, status metav1.ConditionStatus, reason string) metav1.Condition {
		return metav1.Condition{Type: typ, Status: status, Reason: reason, Message: "details of " + reason}
	}
	const (
		cert  = operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned
		ready = operatorv1alpha1.GWAdminConditionTypeReady
	)
	f, tr := metav1.ConditionFalse, metav1.ConditionTrue

	for _, tc := range []struct {
		name string
		old  []metav1.Condition
		cur  []metav1.Condition
		want []string
	}{
		{name: "nothing"},
		{name: "all good", cur: []metav1.Condition{cond(cert, tr, "CertificateProvisioned"), cond(ready, tr, "Ready")}},
		{
			name: "turns False",
			old:  []metav1.Condition{cond(cert, tr, "CertificateProvisioned")},
			cur:  []metav1.Condition{cond(cert, f, operatorv1alpha1.GWAdminReasonInvalidSecret)},
			want: []string{"Warning InvalidSecret details of InvalidSecret"},
		},
		{
			name: "False from the start",
			cur:  []metav1.Condition{cond(cert, f, operatorv1alpha1.GWAdminReasonSecretConflict)},
			want: []string{"Warning SecretConflict details of SecretConflict"},
		},
		{
			name: "stays False for the same reason",
			old:  []metav1.Condition{cond(cert, f, operatorv1alpha1.GWAdminReasonInvalidSecret)},
			cur:  []metav1.Condition{{Type: cert, Status: f, Reason: operatorv1alpha1.GWAdminReasonInvalidSecret, Message: "another message"}},
		},
		{
			name: "False for another reason",
			old:  []metav1.Condition{cond(cert, f, operatorv1alpha1.GWAdminReasonInvalidSecret)},
			cur:  []metav1.Condition{cond(cert, f, operatorv1alpha1.GWAdminReasonCARotationBlocked)},
			want: []string{"Warning CARotationBlocked details of CARotationBlocked"},
		},
		{
			name: "quiet reasons",
			old:  []metav1.Condition{cond(ready, tr, "Ready")},
			cur: []metav1.Condition{
				cond(ready, f, operatorv1alpha1.GWAdminReasonDependenciesNotReady),
			},
		},
		{name: "waiting", cur: []metav1.Condition{cond(ready, f, operatorv1alpha1.GWAdminReasonWaitingToBecomeReady)}},
		{name: "pending", cur: []metav1.Condition{cond(ready, f, operatorv1alpha1.GWAdminReasonPending)}},
		{
			name: "Ready conflict, from waiting",
			old:  []metav1.Condition{cond(ready, f, operatorv1alpha1.GWAdminReasonWaitingToBecomeReady)},
			cur:  []metav1.Condition{cond(ready, f, operatorv1alpha1.GWAdminReasonResourceConflict)},
			want: []string{"Warning ResourceConflict details of ResourceConflict"},
		},
		{
			name: "both",
			cur: []metav1.Condition{
				cond(cert, f, operatorv1alpha1.GWAdminReasonSecretRefNotFound),
				cond(ready, f, operatorv1alpha1.GWAdminReasonFailedToApply),
			},
			want: []string{"Warning SecretRefNotFound details of SecretRefNotFound", "Warning FailedToApply details of FailedToApply"},
		},
		{
			name: "back to True",
			old:  []metav1.Condition{cond(cert, f, operatorv1alpha1.GWAdminReasonInvalidSecret)},
			cur:  []metav1.Condition{cond(cert, tr, "CertificateProvisioned")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := events.NewFakeRecorder(10)
			r := &Reconciler{eventRecorder: rec}
			gw := newGWAdmin(nil)
			gw.Status.Conditions = tc.cur
			r.warnOnFailedConditions(gw, &operatorv1alpha1.GWAdminStatus{Conditions: tc.old})
			assert.Equal(t, tc.want, drainEvents(rec))
		})
	}

	t.Run("no recorder", func(t *testing.T) {
		gw := newGWAdmin(nil)
		gw.Status.Conditions = []metav1.Condition{cond(cert, f, operatorv1alpha1.GWAdminReasonInvalidSecret)}
		(&Reconciler{}).warnOnFailedConditions(gw, &operatorv1alpha1.GWAdminStatus{})
	})
}

// TestOwnedSecretNamesNeverCollide checks that no suffix of the Secret names
// of a GWAdmin ends with another one: the Secrets of foo would otherwise
// collide with those of, e.g., foo-api.
func TestOwnedSecretNamesNeverCollide(t *testing.T) {
	gw := newGWAdmin(nil)
	suffixes := make([]string, 0, len(ownedSecretNames(gw)))
	for _, name := range ownedSecretNames(gw) {
		suffix, ok := strings.CutPrefix(name, gw.Name)
		require.True(t, ok, name)
		suffixes = append(suffixes, suffix)
	}
	for _, a := range suffixes {
		for _, b := range suffixes {
			if a != b {
				assert.False(t, strings.HasSuffix(a, b), "%q ends with %q", a, b)
			}
		}
	}
}

func TestSecretIndexValues(t *testing.T) {
	owned := []string{
		"ns/gw-api-ca-bundle", "ns/gw-api-tls", "ns/gw-cp-tls", "ns/gw-cp-server-ca-bundle",
		"ns/gw-cp-client-ca-bundle",
	}

	assert.Nil(t, secretIndexValues(&corev1.Secret{}), "not a GWAdmin")
	assert.Equal(t, owned, secretIndexValues(newGWAdmin(nil)))
	assert.Equal(t, append(owned, "ns/user-ca", "ns/user-cp"),
		secretIndexValues(newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName), CP: manualCP()})))
	assert.Equal(t, append(owned, "ns/user-cp"),
		secretIndexValues(newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})))

	// Manual without a reference, as stored before the CRD validation.
	broken := newGWAdmin(&operatorv1alpha1.GWAdminTLS{
		ClientCA: &operatorv1alpha1.GWAdminCertificateSecret{Provisioning: operatorv1alpha1.GWAdminCertificateProvisioningManual},
		CP:       &operatorv1alpha1.GWAdminServerCertificate{Provisioning: operatorv1alpha1.GWAdminCertificateProvisioningManual},
	})
	assert.Equal(t, owned, secretIndexValues(broken))

	opts := IndexOptions()
	require.Len(t, opts, 1)
	assert.Equal(t, IndexFieldGWAdminOnSecret, opts[0].Field)
	assert.IsType(t, &operatorv1alpha1.GWAdmin{}, opts[0].Object)
	assert.Equal(t, owned, opts[0].ExtractValueFn(newGWAdmin(nil)))
}

// TestReconcile_ManualCAKeyKongOperatorCantParse checks that a Manual CA
// whose key Kong Operator's own signer can't parse is accepted: Kong Operator
// never issues from it, it only publishes it for the data plane certificate
// issuers.
func TestReconcile_ManualCAKeyKongOperatorCantParse(t *testing.T) {
	ca := userCASecret(t, "ca")
	ecParams := pem.EncodeToMemory(&pem.Block{Type: "EC PARAMETERS", Bytes: []byte{0x06, 0x08, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07}})
	ca.Data[corev1.TLSPrivateKeyKey] = append(ecParams, ca.Data[corev1.TLSPrivateKeyKey]...)
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)}), ca)

	_, gw, err := e.reconcile(t)
	require.NoError(t, err)
	cert := certificateCondition(gw)
	require.NotNil(t, cert)
	assert.Equal(t, metav1.ConditionTrue, cert.Status, cert.Message)
	assert.True(t, caCert(t, publishedCASecret(t, e.cl)).Equal(caCert(t, ca)))
}
