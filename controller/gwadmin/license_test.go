package gwadmin

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func licenseCondition(t *testing.T, gw *operatorv1alpha1.GWAdmin) metav1.Condition {
	t.Helper()
	c, ok := k8sutils.GetCondition(kcfgconsts.ConditionType(operatorv1alpha1.GWAdminConditionTypeLicenseProvisioned), gw)
	require.True(t, ok, "no LicenseProvisioned condition")
	return c
}

func requireLicense(t *testing.T, gw *operatorv1alpha1.GWAdmin, status metav1.ConditionStatus, reason string) metav1.Condition {
	t.Helper()
	c := licenseCondition(t, gw)
	require.Equal(t, status, c.Status, "LicenseProvisioned: %s: %s", c.Reason, c.Message)
	require.Equal(t, reason, c.Reason, "LicenseProvisioned: %s", c.Message)
	return c
}

func (e *reconcileEnv) secret(t *testing.T, name string) (*corev1.Secret, error) {
	t.Helper()
	s := &corev1.Secret{}
	return s, e.cl.Get(t.Context(), client.ObjectKey{Namespace: testNamespace, Name: name}, s)
}

func (e *reconcileEnv) held(t *testing.T, name string) bool {
	t.Helper()
	s, err := e.secret(t, name)
	require.NoError(t, err)
	return slices.Contains(s.Finalizers, consts.GWAdminLicenseInUseFinalizer)
}

func (e *reconcileEnv) setLicense(t *testing.T, license []byte) {
	t.Helper()
	s, err := e.secret(t, testLicenseSecretName)
	require.NoError(t, err)
	s.Data[testLicenseKey] = license
	require.NoError(t, e.cl.Update(t.Context(), s))
}

// TestReconcile_License checks that gw-admin is pointed at the mounted
// license, that the license Secret is held, that an empty license is
// reported without blocking the Deployment, and that a license change doesn't
// roll the pods: gw-admin reads the mounted file again.
func TestReconcile_License(t *testing.T) {
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(nil))

	_, gw, err := e.reconcile(t)
	require.NoError(t, err)
	assert.Empty(t, requireLicense(t, gw, metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonLicenseProvisioned).Message)
	assert.True(t, e.held(t, testLicenseSecretName))

	pod := e.deployment(t).Spec.Template.Spec
	container := pod.Containers[0]
	assert.Contains(t, container.Env, corev1.EnvVar{Name: consts.GWAdminLicenseFileEnvVar, Value: "/etc/gw-admin/license/license.jwt"})
	assert.Contains(t, container.Env, corev1.EnvVar{Name: consts.GWAdminLicenseEntitlementsEnvVar, Value: "event_gateway"})
	assert.Contains(t, container.VolumeMounts, corev1.VolumeMount{Name: licenseVolumeName, MountPath: consts.GWAdminLicenseMountPath, ReadOnly: true})
	i := slices.IndexFunc(pod.Volumes, func(v corev1.Volume) bool { return v.Name == licenseVolumeName })
	require.GreaterOrEqual(t, i, 0)
	require.NotNil(t, pod.Volumes[i].Secret)
	assert.Equal(t, testLicenseSecretName, pod.Volumes[i].Secret.SecretName)
	assert.Equal(t, []corev1.KeyToPath{{Key: testLicenseKey, Path: "license.jwt"}}, pod.Volumes[i].Secret.Items)
	assert.Equal(t, new(true), pod.Volumes[i].Secret.Optional, "a missing license must fail gw-admin, not the volume")
	generation := e.deployment(t).Generation

	// Whatever the value, Kong Operator doesn't validate it: gw-admin does.
	e.setLicense(t, []byte("not-a-license"))
	_, gw, err = e.reconcile(t)
	require.NoError(t, err)
	requireLicense(t, gw, metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonLicenseProvisioned)
	assert.Equal(t, generation, e.deployment(t).Generation, "a license change must not roll the pods")

	// Empty: reported, the Deployment is still applied, unchanged.
	e.setLicense(t, []byte(" \n"))
	_, gw, err = e.reconcile(t)
	require.NoError(t, err)
	c := requireLicense(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonInvalidSecret)
	assert.Contains(t, c.Message, "is empty")
	ready := requireReady(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonDependenciesNotReady)
	assert.Equal(t, c.Message, ready.Message)
	assert.Equal(t, generation, e.deployment(t).Generation)
	assert.True(t, hasEvent(drainEvents(e.rec), "Warning "+operatorv1alpha1.GWAdminReasonInvalidSecret))

	// Fixed: no rollout either.
	e.setLicense(t, testLicenseToken)
	_, gw, err = e.reconcile(t)
	require.NoError(t, err)
	requireLicense(t, gw, metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonLicenseProvisioned)
	assert.Equal(t, generation, e.deployment(t).Generation, "a license change must not roll the pods")

	// Deleted while referenced: held, still used, and the user is told why
	// it is not gone.
	require.NoError(t, e.cl.Delete(t.Context(), licenseSecret(nil)))
	_, gw, err = e.reconcile(t)
	require.NoError(t, err)
	s, err := e.secret(t, testLicenseSecretName)
	require.NoError(t, err, "the referenced license Secret is held")
	assert.False(t, s.DeletionTimestamp.IsZero())
	c = requireLicense(t, gw, metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonLicenseProvisioned)
	assert.Contains(t, c.Message, "is being deleted")
}

// TestReconcile_LicenseRefSwitch checks that a license Secret no longer
// referenced is only released once no pod mounts it: the pods of a rollout
// onto another, missing, Secret keep the one they run.
func TestReconcile_LicenseRefSwitch(t *testing.T) {
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(nil))
	_, _, err := e.reconcile(t)
	require.NoError(t, err)
	e.markRolled(t)
	_, _, err = e.reconcile(t)
	require.NoError(t, err)
	require.True(t, e.held(t, testLicenseSecretName))

	gw := e.gw(t)
	gw.Spec.License.SecretRef.Name = "other-license"
	require.NoError(t, e.cl.Update(t.Context(), gw))
	_, gw, err = e.reconcile(t)
	require.NoError(t, err)
	c := requireLicense(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonSecretRefNotFound)
	assert.Contains(t, c.Message, `"konghq.com/secret" label`)
	requireReady(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonDependenciesNotReady)
	pod := e.deployment(t).Spec.Template.Spec
	i := slices.IndexFunc(pod.Volumes, func(v corev1.Volume) bool { return v.Name == licenseVolumeName })
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, "other-license", pod.Volumes[i].Secret.SecretName, "the pods roll onto the new Secret")

	// The rollout doesn't complete: the old pods keep their license. The
	// fake client doesn't bump the generation: the status tells the rollout
	// is in progress.
	d := e.deployment(t)
	e.patchDeploymentStatus(t, d, appsv1.DeploymentStatus{ObservedGeneration: d.Generation, Replicas: 2, UpdatedReplicas: 1})
	_, _, err = e.reconcile(t)
	require.NoError(t, err)
	assert.True(t, e.held(t, testLicenseSecretName), "held while the rollout is in progress")

	e.markRolled(t)
	_, _, err = e.reconcile(t)
	require.NoError(t, err)
	assert.False(t, e.held(t, testLicenseSecretName), "released once every pod runs the new Secret")
}

// TestReconcile_LicenseSharedAndCleanup checks that a license Secret shared
// by two GWAdmins is held until neither references it.
func TestReconcile_LicenseSharedAndCleanup(t *testing.T) {
	other := newGWAdmin(nil)
	other.Name, other.UID = "other", "other-uid"
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(nil), other)
	_, _, err := e.reconcile(t)
	require.NoError(t, err)
	o := &operatorv1alpha1.GWAdmin{}
	require.NoError(t, e.cl.Get(t.Context(), client.ObjectKeyFromObject(other), o))
	_, err = e.r.Reconcile(t.Context(), o)
	require.NoError(t, err)
	require.True(t, e.held(t, testLicenseSecretName))

	// gw goes: other still references the Secret.
	require.NoError(t, e.cl.Delete(t.Context(), e.gw(t)))
	_, err = e.r.Reconcile(t.Context(), e.gw(t))
	require.NoError(t, err)
	require.True(t, apierrors.IsNotFound(e.cl.Get(t.Context(), client.ObjectKey{Namespace: testNamespace, Name: "gw"}, &operatorv1alpha1.GWAdmin{})))
	assert.True(t, e.held(t, testLicenseSecretName), "still referenced by the other GWAdmin")

	// other goes too: released.
	require.NoError(t, e.cl.Get(t.Context(), client.ObjectKeyFromObject(other), o))
	require.NoError(t, e.cl.Delete(t.Context(), o))
	require.NoError(t, e.cl.Get(t.Context(), client.ObjectKeyFromObject(o), o))
	_, err = e.r.Reconcile(t.Context(), o)
	require.NoError(t, err)
	assert.False(t, e.held(t, testLicenseSecretName))
}

// TestReconcile_LicenseOperatorManagedSecret checks that a Secret Kong
// Operator provisions is refused as the license, and not held.
func TestReconcile_LicenseOperatorManagedSecret(t *testing.T) {
	gw := newGWAdmin(nil)
	gw.Spec.License.SecretRef.Name = cpTLSSecretName(gw)
	e := newReconcileEnv(t, interceptor.Funcs{}, gw)

	_, _, err := e.reconcile(t)
	require.NoError(t, err)
	_, gw, err = e.reconcile(t)
	require.NoError(t, err)
	requireLicense(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonSecretRefOperatorManaged)
	assert.False(t, e.held(t, cpTLSSecretName(gw)))
}

// TestReconcile_LicenseReportedOnEarlyReturn checks that the license is
// reported even when the TLS material stops the reconcile early.
func TestReconcile_LicenseReportedOnEarlyReturn(t *testing.T) {
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)}))

	_, gw, err := e.reconcile(t)
	require.NoError(t, err)
	assert.Equal(t, metav1.ConditionFalse, certificateCondition(gw).Status)
	requireLicense(t, gw, metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonLicenseProvisioned)
	assert.True(t, e.held(t, testLicenseSecretName))
}

// TestReconcile_LicenseConflicts checks that a conflict holding the license
// Secret doesn't keep the Deployment from being applied, and that one
// releasing it is retried.
func TestReconcile_LicenseConflicts(t *testing.T) {
	var conflicts int
	conflictOnSecret := func(ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
		if s, ok := obj.(*corev1.Secret); ok && s.Name == testLicenseSecretName && conflicts > 0 {
			conflicts--
			return apierrors.NewConflict(corev1.Resource("secrets"), s.Name, errors.New("modified"))
		}
		return c.Patch(ctx, obj, p, opts...)
	}
	conflicts = 1
	e := newReconcileEnv(t, interceptor.Funcs{Patch: conflictOnSecret}, newGWAdmin(nil))

	_, gw, err := e.reconcile(t)
	require.NoError(t, err)
	requireLicense(t, gw, metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonLicenseProvisioned)
	assert.False(t, e.held(t, testLicenseSecretName), "the conflict left the Secret unheld")
	require.NoError(t, e.cl.Get(t.Context(), client.ObjectKey{Namespace: testNamespace, Name: "gw"}, &appsv1.Deployment{}),
		"the Deployment is applied despite the conflict")
	_, _, err = e.reconcile(t)
	require.NoError(t, err)
	require.True(t, e.held(t, testLicenseSecretName))

	// A conflict releasing it on deletion: retried shortly.
	conflicts = 1
	require.NoError(t, e.cl.Delete(t.Context(), e.gw(t)))
	res, err := e.r.Reconcile(t.Context(), e.gw(t))
	require.NoError(t, err)
	assert.Equal(t, time.Second, res.RequeueAfter)
	_, err = e.r.Reconcile(t.Context(), e.gw(t))
	require.NoError(t, err)
	assert.False(t, e.held(t, testLicenseSecretName))
}

// TestReconcile_LicenseReadFailed checks that a failure to read the license
// Secret is reported and retried.
func TestReconcile_LicenseReadFailed(t *testing.T) {
	e := newReconcileEnv(t, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.Secret); ok && key.Name == testLicenseSecretName {
				return apierrors.NewForbidden(corev1.Resource("secrets"), key.Name, errors.New("denied"))
			}
			return c.Get(ctx, key, obj, opts...)
		},
	}, newGWAdmin(nil))

	_, gw, err := e.reconcile(t)
	require.Error(t, err)
	requireLicense(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonSecretReadFailed)
}

// TestReconcile_LicenseMissingFirst checks that the Deployment is created
// while the license is missing: its pods fail to start until it is fixed.
func TestReconcile_LicenseMissingFirst(t *testing.T) {
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(nil))
	require.NoError(t, e.cl.Delete(t.Context(), licenseSecret(nil)))

	_, gw, err := e.reconcile(t)
	require.NoError(t, err)
	requireLicense(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonSecretRefNotFound)
	requireReady(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonDependenciesNotReady)
	require.NoError(t, e.cl.Get(t.Context(), client.ObjectKey{Namespace: testNamespace, Name: "gw"}, &appsv1.Deployment{}))
}

// TestReconcile_LicenseNotReferenced checks that a GWAdmin stored before the
// license was required is reported, not retried as a failed read, and gets
// no license wiring, which a Secret volume can't be built for.
func TestReconcile_LicenseNotReferenced(t *testing.T) {
	gw := newGWAdmin(nil)
	gw.Spec.License = operatorv1alpha1.GWAdminLicense{}
	e := newReconcileEnv(t, interceptor.Funcs{}, gw)

	_, gw, err := e.reconcile(t)
	require.NoError(t, err)
	c := requireLicense(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonSecretRefNotFound)
	assert.Contains(t, c.Message, "spec.license.secretRef")
	requireReady(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonDependenciesNotReady)
	pod := e.deployment(t).Spec.Template.Spec
	assert.False(t, slices.ContainsFunc(pod.Volumes, func(v corev1.Volume) bool { return v.Name == licenseVolumeName }))
	assert.False(t, slices.ContainsFunc(pod.Containers[0].Env, func(e corev1.EnvVar) bool { return e.Name == consts.GWAdminLicenseFileEnvVar }))
	assert.NotContains(t, secretIndexValues(gw), testNamespace+"/")
}
