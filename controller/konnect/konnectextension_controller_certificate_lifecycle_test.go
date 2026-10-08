package konnect

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

func TestKonnectExtensionCertificateSnapshot(t *testing.T) {
	for _, tt := range []struct {
		name    string
		extName string
	}{
		{name: "standard name", extName: "extension"},
		{name: "maximum length", extName: strings.Repeat("a", 253)},
		{name: "truncated at separator", extName: strings.Repeat("a", 219) + "." + strings.Repeat("b", 33)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, ext := dpCertTestReconciler(t, "certificate-v1")
			ext.Name = tt.extName
			r.SecretLabelSelector = "test.konghq.com/secret"
			var source corev1.Secret
			require.NoError(t, r.Get(t.Context(), client.ObjectKey{Namespace: ext.Namespace, Name: "certificate"}, &source))
			before := source.DeepCopy()
			result, first, err := r.ensureCertificateSnapshot(t.Context(), ext, &source)
			require.NoError(t, err)
			assert.Equal(t, op.Created, result)
			assert.NotEqual(t, source.Name, first.Name)
			assert.Empty(t, validation.IsDNS1123Subdomain(first.Name))
			assert.Equal(t, new(true), first.Immutable)
			assert.Equal(t, source.Data, first.Data)
			assert.Equal(t, "true", first.Labels[r.SecretLabelSelector])
			assert.True(t, metav1.IsControlledBy(first, ext))

			// Repeated reconciliation and operator restarts reuse the same snapshot.
			restarted := &KonnectExtensionReconciler{Client: r.Client, apiReader: r.apiReader}
			result, same, err := restarted.ensureCertificateSnapshot(t.Context(), ext, &source)
			require.NoError(t, err)
			assert.Equal(t, op.Noop, result)
			assert.Equal(t, first.Name, same.Name)

			source.Data[corev1.TLSCertKey] = []byte("certificate-v2")
			source.Data[corev1.TLSPrivateKeyKey] = []byte("private-key-v2")
			require.NoError(t, r.Update(t.Context(), &source))
			_, second, err := r.ensureCertificateSnapshot(t.Context(), ext, &source)
			require.NoError(t, err)
			assert.NotEqual(t, first.Name, second.Name)
			var retained corev1.Secret
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(first), &retained))
			assert.Equal(t, before.Data, retained.Data)
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(&source), &source))
			assert.Equal(t, before.Labels, source.Labels)
			assert.Empty(t, source.Finalizers)
		})
	}
}

func TestKonnectExtensionInvalidSnapshotSource(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*corev1.Secret)
	}{
		{name: "missing certificate", mutate: func(s *corev1.Secret) { delete(s.Data, corev1.TLSCertKey) }},
		{name: "missing key", mutate: func(s *corev1.Secret) { delete(s.Data, corev1.TLSPrivateKeyKey) }},
		{name: "deleting source", mutate: func(s *corev1.Secret) { s.DeletionTimestamp = new(metav1.Now()) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, ext := dpCertTestReconciler(t, "certificate")
			source := &corev1.Secret{Name: "source", Namespace: ext.Namespace,
				Data: map[string][]byte{corev1.TLSCertKey: []byte("certificate"), corev1.TLSPrivateKeyKey: []byte("key")}}
			tt.mutate(source)
			_, _, err := r.ensureCertificateSnapshot(t.Context(), ext, source)
			require.Error(t, err)
			var all corev1.SecretList
			require.NoError(t, r.List(t.Context(), &all))
			assert.Len(t, all.Items, 1, "invalid input must not create a snapshot")
		})
	}
}

func TestKonnectExtensionAutomaticCertificateRenewal(t *testing.T) {
	for _, tt := range []struct {
		name     string
		expired  bool
		deleting bool
		invalid  bool
		subject  bool
		legacy   bool
		renew    bool
	}{
		{name: "unexpired certificate is reused"},
		{name: "expired certificate is renewed", expired: true, renew: true},
		{name: "deleting certificate is replaced", deleting: true, renew: true},
		{name: "malformed certificate is replaced", invalid: true, renew: true},
		{name: "unexpected subject is replaced", subject: true, renew: true},
		{name: "legacy certificate becomes immutable", legacy: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, ext := dpCertTestReconciler(t, "unused")
			ext.Spec.ClientAuth.CertificateSecret.Provisioning = new(konnectv1alpha2.AutomaticSecretProvisioning)
			ext.Spec.ClientAuth.CertificateSecret.CertificateSecretRef = nil
			r.CertTTL = 24 * time.Hour
			r.CertExpirationMargin = time.Hour
			r.ClusterCASecretName, r.ClusterCASecretNamespace = "ca", ext.Namespace
			require.NoError(t, r.Create(t.Context(), certificate.MustGenerateCASecret(ext.Namespace, "ca", "ca")))
			cert, key := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName(ext.Name + "." + ext.Namespace))
			if tt.expired {
				cert, key = certificate.MustGenerateCertPEMFormat(
					certificate.WithCommonName(ext.Name+"."+ext.Namespace), certificate.WithAlreadyExpired())
			}
			if tt.invalid {
				cert = []byte("not a PEM certificate")
			}
			if tt.subject {
				cert, key = certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("other.default"))
			}
			previous := &corev1.Secret{
				Name: "previous", Namespace: ext.Namespace,
				CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
				Labels: map[string]string{SecretKonnectDataPlaneCertificateLabel: "true",
					consts.SecretProvisioningLabelKey: consts.SecretProvisioningAutomaticLabelValue},
				Data:      map[string][]byte{corev1.TLSCertKey: cert, corev1.TLSPrivateKeyKey: key},
				Immutable: new(!tt.legacy),
			}
			require.NoError(t, controllerutil.SetControllerReference(ext, previous, r.Scheme()))
			if tt.deleting {
				previous.Finalizers = []string{KonnectCleanupFinalizer}
			}
			require.NoError(t, r.Create(t.Context(), previous))
			if tt.deleting {
				require.NoError(t, r.Delete(t.Context(), previous))
			}
			ext.Status.DataPlaneClientAuth = &konnectv1alpha2.DataPlaneClientAuthStatus{
				CertificateSecretRef: &konnectv1alpha2.SecretRef{Name: previous.Name},
			}
			result, current, err := r.ensureCertificateSecret(t.Context(), ext)
			require.NoError(t, err)
			switch {
			case tt.renew:
				assert.Equal(t, op.Created, result)
				assert.NotEqual(t, previous.Name, current.Name)
				assert.NotEqual(t, cert, current.Data[corev1.TLSCertKey])
				assert.Equal(t, new(true), current.Immutable)
			case tt.legacy:
				assert.Equal(t, op.Updated, result)
				assert.Equal(t, new(true), current.Immutable)
			default:
				assert.Equal(t, op.Noop, result)
				assert.Equal(t, previous.Name, current.Name)
			}
			var old corev1.Secret
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(previous), &old))
			assert.Equal(t, previous.Data, old.Data, "renewal must not change or delete the old identity")
			for range 3 {
				restarted := *r
				result, same, err := restarted.ensureCertificateSecret(t.Context(), ext)
				require.NoError(t, err)
				assert.Equal(t, op.Noop, result)
				assert.Equal(t, current.Name, same.Name, "pending generation must survive restarts without duplicate issuance")
			}
		})
	}
}

func TestKonnectExtensionCertificateConsumersMigrated(t *testing.T) {
	for _, tt := range []struct {
		name        string
		missing     bool
		oldTemplate bool
		staleStatus bool
		partial     bool
		oldPod      bool
		scaledZero  bool
		paused      bool
		want        bool
	}{
		{name: "missing Deployment without old Pods", missing: true, want: true},
		{name: "missing Deployment with old Pod", missing: true, oldPod: true},
		{name: "old Pod template", oldTemplate: true},
		{name: "unobserved generation", staleStatus: true},
		{name: "partial rollout", partial: true},
		{name: "paused rollout", paused: true},
		{name: "terminating old Pod", oldPod: true},
		{name: "completed rollout", want: true},
		{name: "scaled to zero without old Pods", scaledZero: true, oldTemplate: true, want: true},
		{name: "scaled to zero with old Pods", scaledZero: true, oldPod: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, ext := dpCertTestReconciler(t, "new")
			dp := &operatorv1beta1.DataPlane{Name: "dp", Namespace: ext.Namespace, UID: types.UID("dp"),
				Spec: operatorv1beta1.DataPlaneSpec{DataPlaneOptions: operatorv1beta1.DataPlaneOptions{Extensions: []commonv1alpha1.ExtensionRef{
					{Group: konnectv1alpha2.GroupVersion.Group, Kind: konnectv1alpha2.KonnectExtensionKind, Name: ext.Name},
				}}}}
			require.NoError(t, r.Create(t.Context(), dp))
			deployment := &appsv1.Deployment{
				Name: "dp", Namespace: ext.Namespace, UID: types.UID("deployment"), Generation: 2,
				Spec: appsv1.DeploymentSpec{Replicas: new(int32(1)),
					Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Volumes: []corev1.Volume{{
						Name:   consts.KongClusterCertVolume,
						Secret: &corev1.SecretVolumeSource{SecretName: "new"},
					}}}}},
				Status: appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
			}
			require.NoError(t, controllerutil.SetControllerReference(dp, deployment, r.Scheme()))
			if tt.oldTemplate {
				deployment.Spec.Template.Spec.Volumes[0].Secret.SecretName = "old"
			}
			if tt.staleStatus {
				deployment.Status.ObservedGeneration = 1
			}
			if tt.partial {
				deployment.Status.Replicas = 2
			}
			deployment.Spec.Paused = tt.paused
			if tt.scaledZero {
				deployment.Spec.Replicas = new(int32(0))
			}
			if !tt.missing {
				require.NoError(t, r.Create(t.Context(), deployment))
			}
			if tt.oldPod {
				pod := &corev1.Pod{
					Name: "old-pod", Namespace: ext.Namespace,
					Finalizers: []string{"test/hold"},
					Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: consts.KongClusterCertVolume,
						Secret: &corev1.SecretVolumeSource{SecretName: "certificate"}}}},
				}
				require.NoError(t, r.Create(t.Context(), pod))
				require.NoError(t, r.Delete(t.Context(), pod))
			}
			got, err := r.certificateConsumersMigrated(t.Context(), ext, "new")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestKonnectExtensionCertificateCleanupUsesLiveReader(t *testing.T) {
	r, ext := dpCertTestReconciler(t, "new")
	live, ok := r.Client.(client.WithWatch)
	require.True(t, ok)
	apiErr := errors.New("API server unavailable")
	r.apiReader = interceptor.NewClient(live, interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error { return apiErr },
	})
	_, err := r.certificateConsumersMigrated(t.Context(), ext, "new")
	require.ErrorIs(t, err, apiErr)
	err = r.retireCertificateGenerations(t.Context(), ext, &corev1.Secret{})
	require.ErrorIs(t, err, apiErr)
}

func TestKonnectExtensionKeepsCertificateUntilControlPlaneApplied(t *testing.T) {
	r, ext := dpCertTestReconciler(t, "new")
	cp := &gwtypes.ControlPlane{
		Name: "cp", Namespace: ext.Namespace,
		Spec: gwtypes.ControlPlaneSpec{Extensions: []commonv1alpha1.ExtensionRef{
			{Group: konnectv1alpha2.GroupVersion.Group, Kind: konnectv1alpha2.KonnectExtensionKind, Name: ext.Name},
		}},
	}
	require.NoError(t, r.Create(t.Context(), cp))
	got, err := r.certificateConsumersMigrated(t.Context(), ext, "new")
	require.NoError(t, err)
	assert.False(t, got)
	cp.Annotations = map[string]string{consts.KonnectClientCertificateSecretAnnotation: "new"}
	require.NoError(t, r.Update(t.Context(), cp))
	got, err = r.certificateConsumersMigrated(t.Context(), ext, "new")
	require.NoError(t, err)
	assert.True(t, got)
}

func TestKonnectExtensionRetiresCertificateWithoutDeployment(t *testing.T) {
	old := dpCertTestObject("extension", "old")
	old.Finalizers = []string{KonnectCleanupFinalizer}
	current := dpCertTestObject(dataPlaneClientCertificateName("extension",
		[]configurationv1alpha1.KongDataPlaneClientCertificate{*old}, "new"), "new")
	r, ext := dpCertTestReconciler(t, "new", old, current)
	dp := &operatorv1beta1.DataPlane{
		Name: "dp", Namespace: ext.Namespace, UID: "dp",
		Spec: operatorv1beta1.DataPlaneSpec{DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
			Extensions: []commonv1alpha1.ExtensionRef{
				{Group: konnectv1alpha2.GroupVersion.Group, Kind: konnectv1alpha2.KonnectExtensionKind, Name: ext.Name},
			},
		}},
	}
	require.NoError(t, r.Create(t.Context(), dp))

	for range 16 {
		require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
		_, err := r.Reconcile(t.Context(), ext)
		require.NoError(t, err)
	}
	var retired configurationv1alpha1.KongDataPlaneClientCertificate
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(old), &retired))
	assert.False(t, retired.DeletionTimestamp.IsZero(), "a missing Deployment without old Pods must not stall retirement")
}

func TestKonnectExtensionRetirementWaitsForKonnectDeletion(t *testing.T) {
	previous := dpCertTestObject("previous", "old")
	previous.Finalizers = []string{KonnectCleanupFinalizer}
	r, ext := dpCertTestReconciler(t, "new", previous)
	oldSecret := &corev1.Secret{
		Name: "old", Namespace: ext.Namespace,
		Labels:     map[string]string{SecretKonnectDataPlaneCertificateLabel: "true"},
		Finalizers: []string{KonnectCleanupFinalizer, consts.KonnectExtensionSecretInUseFinalizer},
		Data:       map[string][]byte{corev1.TLSCertKey: []byte("old")},
	}
	require.NoError(t, controllerutil.SetControllerReference(ext, oldSecret, r.Scheme()))
	require.NoError(t, r.Create(t.Context(), oldSecret))
	current := &corev1.Secret{Name: "new", Data: map[string][]byte{corev1.TLSCertKey: []byte("new")}}
	require.NoError(t, r.retireCertificateGenerations(t.Context(), ext, current))
	var held configurationv1alpha1.KongDataPlaneClientCertificate
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(previous), &held))
	assert.False(t, held.DeletionTimestamp.IsZero())
	var retained corev1.Secret
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(oldSecret), &retained))
	assert.Equal(t, oldSecret.Finalizers, retained.Finalizers)
	held.Finalizers = nil
	require.NoError(t, r.Update(t.Context(), &held))
	for range 3 {
		require.NoError(t, r.retireCertificateGenerations(t.Context(), ext, current))
	}
	assert.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(oldSecret), &retained)))
}

func TestKonnectExtensionWaitsForRegistrationBeforePublishingSnapshot(t *testing.T) {
	r, ext := dpCertTestReconciler(t, "new", dpCertTestObject("extension", "old"))
	ext.Status.DataPlaneClientAuth = &konnectv1alpha2.DataPlaneClientAuthStatus{
		CertificateSecretRef: &konnectv1alpha2.SecretRef{Name: "previous"},
	}
	require.NoError(t, r.Status().Update(t.Context(), ext))
	for range 16 {
		require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
		_, err := r.Reconcile(t.Context(), ext)
		require.NoError(t, err)
	}
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
	assert.Equal(t, "previous", ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name)
	assert.Len(t, dpCertTestList(t, r.Client), 2)
}

func TestKonnectExtensionRejectsCertificateWithoutRenewalInterval(t *testing.T) {
	cert, key := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("extension.default"))
	r, ext := dpCertTestReconciler(t, "unused")
	ext.Spec.ClientAuth.CertificateSecret.Provisioning = new(konnectv1alpha2.AutomaticSecretProvisioning)
	ext.Spec.ClientAuth.CertificateSecret.CertificateSecretRef = nil
	secret := &corev1.Secret{
		Name: "issued", Namespace: ext.Namespace, CreationTimestamp: metav1.Now(), Immutable: new(true),
		Labels: map[string]string{
			SecretKonnectDataPlaneCertificateLabel: "true", consts.SecretProvisioningLabelKey: consts.SecretProvisioningAutomaticLabelValue,
		},
		Annotations: map[string]string{automaticCertificateIssuedAtAnnotation: time.Now().UTC().Format(time.RFC3339Nano)},
		Data:        map[string][]byte{corev1.TLSCertKey: cert, corev1.TLSPrivateKeyKey: key},
	}
	require.NoError(t, controllerutil.SetControllerReference(ext, secret, r.Scheme()))
	require.NoError(t, r.Create(t.Context(), secret))
	expires, err := r.certificateRenewalTime(secret)
	require.NoError(t, err)
	r.CertExpirationMargin = time.Until(expires) + time.Minute
	for range 3 {
		_, _, err := r.ensureCertificateSecret(t.Context(), ext)
		require.ErrorContains(t, err, "has no renewal interval")
	}
	owned, err := r.listOwnedCertificateSecrets(t.Context(), ext)
	require.NoError(t, err)
	assert.Len(t, owned, 1, "invalid TTL/margin must not cause an issuance loop")
}

func TestKonnectExtensionSchedulesRenewalBeforeSyncPeriod(t *testing.T) {
	cert, key := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("extension.default"))
	r, ext := dpCertTestReconciler(t, "unused", dpCertTestObject("extension", string(cert)))
	ext.Spec.ClientAuth.CertificateSecret.Provisioning = new(konnectv1alpha2.AutomaticSecretProvisioning)
	ext.Spec.ClientAuth.CertificateSecret.CertificateSecretRef = nil
	require.NoError(t, r.Update(t.Context(), ext))
	r.SyncPeriod = time.Hour
	secret := &corev1.Secret{
		Name: "issued", Namespace: ext.Namespace, Immutable: new(true),
		Labels: map[string]string{
			SecretKonnectDataPlaneCertificateLabel: "true", consts.SecretProvisioningLabelKey: consts.SecretProvisioningAutomaticLabelValue,
		},
		Data: map[string][]byte{corev1.TLSCertKey: cert, corev1.TLSPrivateKeyKey: key},
	}
	require.NoError(t, controllerutil.SetControllerReference(ext, secret, r.Scheme()))
	require.NoError(t, r.Create(t.Context(), secret))
	expires, err := r.certificateRenewalTime(secret)
	require.NoError(t, err)
	r.CertExpirationMargin = time.Until(expires) - 30*time.Second
	var result ctrl.Result
	for range 16 {
		require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
		result, err = r.Reconcile(t.Context(), ext)
		require.NoError(t, err)
	}
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
	assert.True(t, k8sutils.HasConditionTrue(konnectv1alpha2.KonnectExtensionReadyConditionType, ext))
	assert.Greater(t, result.RequeueAfter, 15*time.Second)
	assert.LessOrEqual(t, result.RequeueAfter, 30*time.Second)
}

func TestKonnectExtensionRejectsForeignCertificateNameCollision(t *testing.T) {
	foreign := dpCertTestObject("extension", "new")
	foreign.OwnerReferences[0].UID = "different-extension"
	r, ext := dpCertTestReconciler(t, "new", foreign)
	var source corev1.Secret
	require.NoError(t, r.Get(t.Context(), client.ObjectKey{Namespace: ext.Namespace, Name: "certificate"}, &source))
	_, _, err := r.ensureCertificateSnapshot(t.Context(), ext, &source)
	require.NoError(t, err)
	ext.Status.Conditions = []metav1.Condition{
		{Type: konnectv1alpha2.KonnectExtensionReadyConditionType, Status: metav1.ConditionTrue},
		{Type: konnectv1alpha1.DataPlaneCertificateProvisionedConditionType, Status: metav1.ConditionTrue},
	}
	require.NoError(t, r.Status().Update(t.Context(), ext))
	for range 16 {
		require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
		_, err = r.Reconcile(t.Context(), ext)
		if err != nil {
			break
		}
	}
	require.ErrorContains(t, err, "conflicts with the requested generation")
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
	assert.True(t, k8sutils.HasConditionFalse(konnectv1alpha2.KonnectExtensionReadyConditionType, ext))
	assert.True(t, k8sutils.HasConditionFalse(konnectv1alpha1.DataPlaneCertificateProvisionedConditionType, ext))
	assert.Len(t, dpCertTestList(t, r.Client), 1)
}

func TestKonnectExtensionReconciliationPreservesOverlapUntilOldPodsExit(t *testing.T) {
	old := dpCertTestObject("extension", "old")
	old.Finalizers = []string{KonnectCleanupFinalizer}
	current := dpCertTestObject(dataPlaneClientCertificateName("extension",
		[]configurationv1alpha1.KongDataPlaneClientCertificate{*old}, "new"), "new")
	r, ext := dpCertTestReconciler(t, "new", old, current)
	var source corev1.Secret
	require.NoError(t, r.Get(t.Context(), client.ObjectKey{Namespace: ext.Namespace, Name: "certificate"}, &source))
	_, snapshot, err := r.ensureCertificateSnapshot(t.Context(), ext, &source)
	require.NoError(t, err)
	oldSecret := &corev1.Secret{
		Name: "previous", Namespace: ext.Namespace,
		Labels:     map[string]string{SecretKonnectDataPlaneCertificateLabel: "true"},
		Finalizers: []string{KonnectCleanupFinalizer, consts.KonnectExtensionSecretInUseFinalizer},
		Data:       map[string][]byte{corev1.TLSCertKey: []byte("old")},
	}
	require.NoError(t, controllerutil.SetControllerReference(ext, oldSecret, r.Scheme()))
	require.NoError(t, r.Create(t.Context(), oldSecret))
	dp := &operatorv1beta1.DataPlane{
		Name: "dp", Namespace: ext.Namespace, UID: "dp",
		Spec: operatorv1beta1.DataPlaneSpec{DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
			Extensions: []commonv1alpha1.ExtensionRef{
				{Group: konnectv1alpha2.GroupVersion.Group, Kind: konnectv1alpha2.KonnectExtensionKind, Name: ext.Name},
			},
		}},
	}
	require.NoError(t, r.Create(t.Context(), dp))
	deployment := &appsv1.Deployment{
		Name: "dp", Namespace: ext.Namespace, Generation: 1,
		Spec: appsv1.DeploymentSpec{Replicas: new(int32(1)), Paused: true,
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Volumes: []corev1.Volume{
				{Name: consts.KongClusterCertVolume, Secret: &corev1.SecretVolumeSource{SecretName: snapshot.Name}},
			}}}},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
	}
	require.NoError(t, controllerutil.SetControllerReference(dp, deployment, r.Scheme()))
	require.NoError(t, r.Create(t.Context(), deployment))
	reconcile := func() {
		t.Helper()
		for range 16 {
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
			_, err := r.Reconcile(t.Context(), ext)
			require.NoError(t, err)
		}
	}
	reconcile()
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
	assert.Equal(t, snapshot.Name, ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name)
	assert.True(t, k8sutils.HasConditionTrue(konnectv1alpha2.KonnectExtensionReadyConditionType, ext))
	assert.Len(t, dpCertTestList(t, r.Client), 2)

	pod := &corev1.Pod{
		Name: "terminating", Namespace: ext.Namespace, Finalizers: []string{"test/hold"},
		Spec: corev1.PodSpec{Volumes: []corev1.Volume{
			{Name: consts.KongClusterCertVolume, Secret: &corev1.SecretVolumeSource{SecretName: oldSecret.Name}},
		}},
	}
	require.NoError(t, r.Create(t.Context(), pod))
	require.NoError(t, r.Delete(t.Context(), pod))
	deployment.Spec.Paused = false
	require.NoError(t, r.Update(t.Context(), deployment))
	reconcile()
	var held configurationv1alpha1.KongDataPlaneClientCertificate
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(old), &held))
	assert.True(t, held.DeletionTimestamp.IsZero(), "even a completed rollout must retain trust for terminating Pods")

	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(pod), pod))
	pod.Finalizers = nil
	require.NoError(t, r.Update(t.Context(), pod))
	reconcile()
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(old), &held))
	assert.False(t, held.DeletionTimestamp.IsZero())
	var retained corev1.Secret
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(oldSecret), &retained))
	assert.Equal(t, oldSecret.Finalizers, retained.Finalizers)
	held.Finalizers = nil
	require.NoError(t, r.Update(t.Context(), &held))
	reconcile()
	assert.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(oldSecret), &retained)))
	assert.Len(t, dpCertTestList(t, r.Client), 1)
}
