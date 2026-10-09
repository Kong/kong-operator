package konnect

import (
	"context"
	"errors"
	"fmt"
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

func TestKonnectExtensionDeletingCertificateSnapshot(t *testing.T) {
	for _, name := range []string{"extension", strings.Repeat("a", 253)} {
		t.Run(name, func(t *testing.T) {
			r, ext := dpCertTestReconciler(t, "certificate")
			ext.Name = name
			var source corev1.Secret
			require.NoError(t, r.Get(t.Context(), client.ObjectKey{Namespace: ext.Namespace, Name: "certificate"}, &source))
			_, first, err := r.ensureCertificateSnapshot(t.Context(), ext, &source)
			require.NoError(t, err)
			first.Finalizers = []string{KonnectCleanupFinalizer, consts.KonnectExtensionSecretInUseFinalizer}
			require.NoError(t, r.Update(t.Context(), first))
			require.NoError(t, r.Delete(t.Context(), first))

			result, replacement, err := r.ensureCertificateSnapshot(t.Context(), ext, &source)
			require.NoError(t, err)
			assert.Equal(t, op.Created, result)
			assert.NotEqual(t, first.Name, replacement.Name)
			assert.Empty(t, validation.IsDNS1123Subdomain(replacement.Name))
			assert.Equal(t, source.Data, replacement.Data)
			assert.Equal(t, new(true), replacement.Immutable)
			for range 3 {
				restarted := *r
				result, same, err := restarted.ensureCertificateSnapshot(t.Context(), ext, &source)
				require.NoError(t, err)
				assert.Equal(t, op.Noop, result)
				assert.Equal(t, replacement.Name, same.Name)
			}
			var retained corev1.Secret
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(first), &retained))
			assert.Equal(t, first.Finalizers, retained.Finalizers, "the old mount must survive until consumer migration")
			retained.Finalizers = nil
			require.NoError(t, r.Update(t.Context(), &retained))
			result, same, err := r.ensureCertificateSnapshot(t.Context(), ext, &source)
			require.NoError(t, err)
			assert.Equal(t, op.Noop, result)
			assert.Equal(t, replacement.Name, same.Name, "do not switch back when the content-derived name becomes free")

			replacement.Finalizers = []string{KonnectCleanupFinalizer}
			require.NoError(t, r.Update(t.Context(), replacement))
			require.NoError(t, r.Delete(t.Context(), replacement))
			result, next, err := r.ensureCertificateSnapshot(t.Context(), ext, &source)
			require.NoError(t, err)
			assert.Equal(t, op.Created, result)
			assert.NotEqual(t, replacement.Name, next.Name, "deleting a replacement must also recover")
			assert.Equal(t, source.Data, next.Data)
		})
	}
}

func TestKonnectExtensionDeletingSnapshotConflict(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*corev1.Secret)
	}{
		{name: "foreign owner", mutate: func(s *corev1.Secret) { s.OwnerReferences[0].UID = "foreign-extension" }},
		{name: "different certificate", mutate: func(s *corev1.Secret) { s.Data[corev1.TLSCertKey] = []byte("other") }},
		{name: "different key", mutate: func(s *corev1.Secret) { s.Data[corev1.TLSPrivateKeyKey] = []byte("other") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, ext := dpCertTestReconciler(t, "certificate")
			var source corev1.Secret
			require.NoError(t, r.Get(t.Context(), client.ObjectKey{Namespace: ext.Namespace, Name: "certificate"}, &source))
			_, snapshot, err := r.ensureCertificateSnapshot(t.Context(), ext, &source)
			require.NoError(t, err)
			// A mutable fixture lets the fake client model a conflicting object
			// at the desired name; production snapshots themselves are immutable.
			snapshot.Immutable = nil
			snapshot.Finalizers = []string{KonnectCleanupFinalizer, consts.KonnectExtensionSecretInUseFinalizer}
			tt.mutate(snapshot)
			require.NoError(t, r.Update(t.Context(), snapshot))
			require.NoError(t, r.Delete(t.Context(), snapshot))
			_, _, err = r.ensureCertificateSnapshot(t.Context(), ext, &source)
			require.ErrorContains(t, err, "conflicts with the desired generation")
			var retained corev1.Secret
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(snapshot), &retained))
			assert.Equal(t, snapshot.Finalizers, retained.Finalizers)
			assert.Equal(t, snapshot.Data, retained.Data)
		})
	}
}

func TestKonnectExtensionSelectsReusableSnapshot(t *testing.T) {
	for _, tt := range []struct {
		name        string
		currentName string
		reverse     bool
		want        string
	}{
		{name: "published snapshot visited first", currentName: "z-published", reverse: true, want: "z-published"},
		{name: "published snapshot visited last", currentName: "z-published", want: "z-published"},
		{name: "pending snapshots in ascending order", want: "a-pending"},
		{name: "pending snapshots in descending order", reverse: true, want: "a-pending"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, ext := dpCertTestReconciler(t, "certificate")
			var source corev1.Secret
			require.NoError(t, r.Get(t.Context(), client.ObjectKey{Namespace: ext.Namespace, Name: "certificate"}, &source))
			_, original, err := r.ensureCertificateSnapshot(t.Context(), ext, &source)
			require.NoError(t, err)
			candidates := make([]corev1.Secret, 0, 2)
			for _, name := range []string{"a-pending", "z-published"} {
				candidate := original.DeepCopy()
				candidate.Name = name
				candidate.ResourceVersion, candidate.UID = "", ""
				require.NoError(t, r.Create(t.Context(), candidate))
				candidates = append(candidates, *candidate)
			}
			original.Finalizers = []string{KonnectCleanupFinalizer}
			require.NoError(t, r.Update(t.Context(), original))
			require.NoError(t, r.Delete(t.Context(), original))
			ext.Status.DataPlaneClientAuth = &konnectv1alpha2.DataPlaneClientAuthStatus{
				CertificateSecretRef: &konnectv1alpha2.SecretRef{Name: tt.currentName},
			}
			if tt.reverse {
				candidates[0], candidates[1] = candidates[1], candidates[0]
			}
			live, ok := r.Client.(client.WithWatch)
			require.True(t, ok)
			r.apiReader = interceptor.NewClient(live, interceptor.Funcs{
				List: func(ctx context.Context, _ client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if secrets, ok := list.(*corev1.SecretList); ok {
						secrets.Items = candidates
						return nil
					}
					return live.List(ctx, list, opts...)
				},
			})
			result, selected, err := r.ensureCertificateSnapshot(t.Context(), ext, &source)
			require.NoError(t, err)
			assert.Equal(t, op.Noop, result)
			assert.Equal(t, tt.want, selected.Name)
		})
	}
}

func TestIsReusableManualCertificateSnapshot(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*corev1.Secret)
		want   bool
	}{
		{name: "matching owned snapshot", mutate: func(*corev1.Secret) {}, want: true},
		{name: "source Secret", mutate: func(s *corev1.Secret) { s.Name = "certificate" }},
		{name: "deleting snapshot", mutate: func(s *corev1.Secret) { s.DeletionTimestamp = new(metav1.Now()) }},
		{name: "foreign owner", mutate: func(s *corev1.Secret) { s.OwnerReferences[0].UID = "other-extension" }},
		{name: "Automatic Secret", mutate: func(s *corev1.Secret) {
			s.Labels[consts.SecretProvisioningLabelKey] = consts.SecretProvisioningAutomaticLabelValue
		}},
		{name: "different certificate", mutate: func(s *corev1.Secret) { s.Data[corev1.TLSCertKey] = []byte("other") }},
		{name: "different key", mutate: func(s *corev1.Secret) { s.Data[corev1.TLSPrivateKeyKey] = []byte("other") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, ext := dpCertTestReconciler(t, "certificate")
			var source corev1.Secret
			require.NoError(t, r.Get(t.Context(), client.ObjectKey{Namespace: ext.Namespace, Name: "certificate"}, &source))
			_, snapshot, err := r.ensureCertificateSnapshot(t.Context(), ext, &source)
			require.NoError(t, err)
			tt.mutate(snapshot)
			assert.Equal(t, tt.want, isReusableManualCertificateSnapshot(ext, &source, snapshot))
		})
	}
}

func TestKonnectExtensionSnapshotReadFailure(t *testing.T) {
	r, ext := dpCertTestReconciler(t, "certificate")
	var source corev1.Secret
	require.NoError(t, r.Get(t.Context(), client.ObjectKey{Namespace: ext.Namespace, Name: "certificate"}, &source))
	live, ok := r.Client.(client.WithWatch)
	require.True(t, ok)
	readErr := errors.New("snapshot read failed")
	r.apiReader = interceptor.NewClient(live, interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return readErr
		},
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			t.Fatal("a failed snapshot read must not fall through to candidate selection")
			return nil
		},
	})
	result, snapshot, err := r.ensureCertificateSnapshot(t.Context(), ext, &source)
	require.ErrorIs(t, err, readErr)
	assert.Equal(t, op.Noop, result)
	assert.Nil(t, snapshot)
	var secrets corev1.SecretList
	require.NoError(t, live.List(t.Context(), &secrets))
	assert.Len(t, secrets.Items, 1)
}

func TestKonnectExtensionRequeuesFutureDeletionTimestamp(t *testing.T) {
	for _, hasDependent := range []bool{false, true} {
		t.Run(fmt.Sprintf("dependent=%t", hasDependent), func(t *testing.T) {
			registration := dpCertTestObject("extension", "certificate")
			r, ext := dpCertTestReconciler(t, "certificate", registration)
			ext.Finalizers = []string{KonnectCleanupFinalizer}
			require.NoError(t, r.Update(t.Context(), ext))
			if hasDependent {
				require.NoError(t, r.Create(t.Context(), &operatorv1beta1.DataPlane{
					ObjectMeta: metav1.ObjectMeta{Name: "dp", Namespace: ext.Namespace},
					Spec: operatorv1beta1.DataPlaneSpec{DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Extensions: []commonv1alpha1.ExtensionRef{{
							Group: konnectv1alpha2.GroupVersion.Group, Kind: konnectv1alpha2.KonnectExtensionKind,
							NamespacedRef: commonv1alpha1.NamespacedRef{Name: ext.Name},
						}},
					}},
				}))
			}
			ext.DeletionTimestamp = new(metav1.NewTime(time.Now().Add(time.Minute)))
			result, err := r.Reconcile(t.Context(), ext)
			require.NoError(t, err)
			assert.Greater(t, result.RequeueAfter, 55*time.Second)
			assert.LessOrEqual(t, result.RequeueAfter, time.Minute)
			assert.Len(t, dpCertTestList(t, r.Client), 1)
			ext.DeletionTimestamp = new(metav1.NewTime(time.Now().Add(-time.Second)))
			_, err = r.Reconcile(t.Context(), ext)
			require.NoError(t, err)
			if hasDependent {
				assert.Len(t, dpCertTestList(t, r.Client), 1, "dependents must still block deletion")
			} else {
				assert.Empty(t, dpCertTestList(t, r.Client), "cleanup must run after the timestamp passes")
			}
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
			source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: ext.Namespace},
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
			caCert, caKey := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("ca"), certificate.WithCATrue())
			require.NoError(t, r.Create(t.Context(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "ca", Namespace: ext.Namespace},
				Data: map[string][]byte{
					corev1.TLSCertKey: caCert, corev1.TLSPrivateKeyKey: caKey,
				},
			}))
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
				ObjectMeta: metav1.ObjectMeta{
					Name: "previous", Namespace: ext.Namespace,
					CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
					Labels: map[string]string{SecretKonnectDataPlaneCertificateLabel: "true",
						consts.SecretProvisioningLabelKey: consts.SecretProvisioningAutomaticLabelValue},
				},
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
		otherPod    bool
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
		{name: "unrelated Pod mounting Manual source Secret", otherPod: true, want: true},
		{name: "completed rollout", want: true},
		{name: "scaled to zero without old Pods", scaledZero: true, oldTemplate: true, want: true},
		{name: "scaled to zero with old Pods", scaledZero: true, oldPod: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, ext := dpCertTestReconciler(t, "new")
			dp := &operatorv1beta1.DataPlane{ObjectMeta: metav1.ObjectMeta{Name: "dp", Namespace: ext.Namespace, UID: types.UID("dp")},
				Spec: operatorv1beta1.DataPlaneSpec{DataPlaneOptions: operatorv1beta1.DataPlaneOptions{Extensions: []commonv1alpha1.ExtensionRef{
					{Group: konnectv1alpha2.GroupVersion.Group, Kind: konnectv1alpha2.KonnectExtensionKind,
						NamespacedRef: commonv1alpha1.NamespacedRef{Name: ext.Name}},
				}}}}
			require.NoError(t, r.Create(t.Context(), dp))
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "dp", Namespace: ext.Namespace, UID: types.UID("deployment"), Generation: 2},
				Spec: appsv1.DeploymentSpec{Replicas: new(int32(1)),
					Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Volumes: []corev1.Volume{{
						Name:         consts.KongClusterCertVolume,
						VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "new"}},
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
			if tt.oldPod || tt.otherPod {
				volumeName := consts.KongClusterCertVolume
				if tt.otherPod {
					volumeName = "unrelated-tls"
				}
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "old-pod", Namespace: ext.Namespace, Finalizers: []string{"test/hold"}},
					Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: volumeName,
						VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "certificate"}}}}},
				}
				require.NoError(t, r.Create(t.Context(), pod))
				if tt.oldPod {
					require.NoError(t, r.Delete(t.Context(), pod))
				}
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
	_, err = r.retireCertificateGenerations(t.Context(), ext, &corev1.Secret{})
	require.ErrorIs(t, err, apiErr)
}

func TestKonnectExtensionConfirmsDependentAbsenceBeforeCleanup(t *testing.T) {
	for _, kind := range []string{"DataPlane", "ControlPlane"} {
		for _, deleting := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/deleting=%t", kind, deleting), func(t *testing.T) {
				registration := dpCertTestObject("extension", "certificate")
				r, ext := dpCertTestReconciler(t, "certificate", registration)
				refs := []commonv1alpha1.ExtensionRef{{
					Group: konnectv1alpha2.GroupVersion.Group, Kind: konnectv1alpha2.KonnectExtensionKind,
					NamespacedRef: commonv1alpha1.NamespacedRef{Name: ext.Name},
				}}
				switch kind {
				case "DataPlane":
					require.NoError(t, r.Create(t.Context(), &operatorv1beta1.DataPlane{
						ObjectMeta: metav1.ObjectMeta{Name: "dependent", Namespace: ext.Namespace},
						Spec:       operatorv1beta1.DataPlaneSpec{DataPlaneOptions: operatorv1beta1.DataPlaneOptions{Extensions: refs}},
					}))
				case "ControlPlane":
					require.NoError(t, r.Create(t.Context(), &gwtypes.ControlPlane{
						ObjectMeta: metav1.ObjectMeta{Name: "dependent", Namespace: ext.Namespace},
						Spec:       gwtypes.ControlPlaneSpec{Extensions: refs},
					}))
				}
				ext.Finalizers = []string{KonnectCleanupFinalizer, consts.ExtensionInUseFinalizer}
				require.NoError(t, r.Update(t.Context(), ext))
				live, ok := r.Client.(client.WithWatch)
				require.True(t, ok)
				r.Client = interceptor.NewClient(live, interceptor.Funcs{
					List: func(ctx context.Context, _ client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						switch list := list.(type) {
						case *operatorv1beta1.DataPlaneList:
							list.Items = nil
							return nil
						case *gwtypes.ControlPlaneList:
							list.Items = nil
							return nil
						}
						return live.List(ctx, list, opts...)
					},
				})
				if deleting {
					ext.DeletionTimestamp = new(metav1.NewTime(time.Now().Add(-time.Second)))
				}
				_, err := r.Reconcile(t.Context(), ext)
				require.NoError(t, err)
				assert.Contains(t, ext.Finalizers, consts.ExtensionInUseFinalizer,
					"a cached absence must not release the finalizer while a live dependent exists")
				require.NoError(t, live.Get(t.Context(), client.ObjectKeyFromObject(registration), registration))
				assert.True(t, registration.DeletionTimestamp.IsZero(), "a live dependent must retain its Konnect registration")
			})
		}
	}
}

func TestKonnectExtensionKeepsCertificateUntilControlPlaneApplied(t *testing.T) {
	r, ext := dpCertTestReconciler(t, "new")
	cp := &gwtypes.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{Name: "cp", Namespace: ext.Namespace},
		Spec: gwtypes.ControlPlaneSpec{Extensions: []commonv1alpha1.ExtensionRef{
			{Group: konnectv1alpha2.GroupVersion.Group, Kind: konnectv1alpha2.KonnectExtensionKind,
				NamespacedRef: commonv1alpha1.NamespacedRef{Name: ext.Name}},
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
		ObjectMeta: metav1.ObjectMeta{Name: "dp", Namespace: ext.Namespace, UID: "dp"},
		Spec: operatorv1beta1.DataPlaneSpec{DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
			Extensions: []commonv1alpha1.ExtensionRef{
				{Group: konnectv1alpha2.GroupVersion.Group, Kind: konnectv1alpha2.KonnectExtensionKind,
					NamespacedRef: commonv1alpha1.NamespacedRef{Name: ext.Name}},
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
		ObjectMeta: metav1.ObjectMeta{
			Name: "old", Namespace: ext.Namespace,
			Labels:     map[string]string{SecretKonnectDataPlaneCertificateLabel: "true"},
			Finalizers: []string{KonnectCleanupFinalizer, consts.KonnectExtensionSecretInUseFinalizer},
		},
		Data: map[string][]byte{corev1.TLSCertKey: []byte("old")},
	}
	require.NoError(t, controllerutil.SetControllerReference(ext, oldSecret, r.Scheme()))
	require.NoError(t, r.Create(t.Context(), oldSecret))
	current := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "new"}, Data: map[string][]byte{corev1.TLSCertKey: []byte("new")}}
	pending, err := r.retireCertificateGenerations(t.Context(), ext, current)
	require.NoError(t, err)
	assert.True(t, pending)
	var held configurationv1alpha1.KongDataPlaneClientCertificate
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(previous), &held))
	assert.False(t, held.DeletionTimestamp.IsZero())
	var retained corev1.Secret
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(oldSecret), &retained))
	assert.Equal(t, oldSecret.Finalizers, retained.Finalizers)
	held.Finalizers = nil
	require.NoError(t, r.Update(t.Context(), &held))
	for range 3 {
		_, err := r.retireCertificateGenerations(t.Context(), ext, current)
		require.NoError(t, err)
	}
	assert.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(oldSecret), &retained)))
}

func TestKonnectExtensionRetirementCleansAllOldSecrets(t *testing.T) {
	r, ext := dpCertTestReconciler(t, "new")
	for _, name := range []string{"old-a", "old-b"} {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: ext.Namespace,
				Labels:     map[string]string{SecretKonnectDataPlaneCertificateLabel: "true"},
				Finalizers: []string{KonnectCleanupFinalizer, consts.KonnectExtensionSecretInUseFinalizer},
			},
			Data: map[string][]byte{corev1.TLSCertKey: []byte(name)},
		}
		require.NoError(t, controllerutil.SetControllerReference(ext, secret, r.Scheme()))
		require.NoError(t, r.Create(t.Context(), secret))
	}
	current := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "current"}, Data: map[string][]byte{corev1.TLSCertKey: []byte("new")}}
	pending, err := r.retireCertificateGenerations(t.Context(), ext, current)
	require.NoError(t, err)
	assert.True(t, pending)
	for _, name := range []string{"old-a", "old-b"} {
		var secret corev1.Secret
		require.NoError(t, r.Get(t.Context(), client.ObjectKey{Namespace: ext.Namespace, Name: name}, &secret))
		assert.Empty(t, secret.Finalizers, "all generations should release finalizers in the same pass")
	}
	pending, err = r.retireCertificateGenerations(t.Context(), ext, current)
	require.NoError(t, err)
	assert.True(t, pending)
	for _, name := range []string{"old-a", "old-b"} {
		var secret corev1.Secret
		assert.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKey{Namespace: ext.Namespace, Name: name}, &secret)))
	}
	pending, err = r.retireCertificateGenerations(t.Context(), ext, current)
	require.NoError(t, err)
	assert.False(t, pending)
}

func TestKonnectExtensionSkipsConsumerReadsWithoutOldGenerations(t *testing.T) {
	for _, staleSecret := range []bool{false, true} {
		name := "steady state"
		if staleSecret {
			name = "stale Secret without registration"
		}
		t.Run(name, func(t *testing.T) {
			r, ext := dpCertTestReconciler(t, "new", dpCertTestObject("extension", "new"))
			var source corev1.Secret
			require.NoError(t, r.Get(t.Context(), client.ObjectKey{Namespace: ext.Namespace, Name: "certificate"}, &source))
			_, _, err := r.ensureCertificateSnapshot(t.Context(), ext, &source)
			require.NoError(t, err)
			if staleSecret {
				old := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: ext.Namespace,
						Labels: map[string]string{SecretKonnectDataPlaneCertificateLabel: "true"}},
					Data: map[string][]byte{corev1.TLSCertKey: []byte("old")},
				}
				require.NoError(t, controllerutil.SetControllerReference(ext, old, r.Scheme()))
				require.NoError(t, r.Create(t.Context(), old))
			}
			live, ok := r.Client.(client.WithWatch)
			require.True(t, ok)
			listCalls, workloadLists := 0, 0
			r.apiReader = interceptor.NewClient(live, interceptor.Funcs{
				List: func(ctx context.Context, _ client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					listCalls++
					switch list.(type) {
					case *corev1.PodList, *appsv1.DeploymentList:
						workloadLists++
					}
					return live.List(ctx, list, opts...)
				},
			})
			for range 16 {
				require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
				_, err := r.Reconcile(t.Context(), ext)
				require.NoError(t, err)
			}
			if staleSecret {
				assert.Positive(t, workloadLists, "stale owned Secrets still require migration checks")
				var old corev1.Secret
				assert.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKey{Namespace: ext.Namespace, Name: "old"}, &old)))
			} else {
				require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
				assert.True(t, k8sutils.HasConditionTrue(konnectv1alpha2.KonnectExtensionReadyConditionType, ext))
				listCalls, workloadLists = 0, 0
				_, err := r.Reconcile(t.Context(), ext)
				require.NoError(t, err)
				assert.Equal(t, 2, listCalls, "steady-state live reads should only list certificates and owned Secrets")
				assert.Zero(t, workloadLists, "steady state must skip rollout and Pod scans")
			}
		})
	}
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
		ObjectMeta: metav1.ObjectMeta{
			Name: "issued", Namespace: ext.Namespace, CreationTimestamp: metav1.Now(),
			Labels: map[string]string{
				SecretKonnectDataPlaneCertificateLabel: "true", consts.SecretProvisioningLabelKey: consts.SecretProvisioningAutomaticLabelValue,
			},
			Annotations: map[string]string{automaticCertificateIssuedAtAnnotation: time.Now().UTC().Format(time.RFC3339Nano)},
		},
		Immutable: new(true),
		Data:      map[string][]byte{corev1.TLSCertKey: cert, corev1.TLSPrivateKeyKey: key},
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
		ObjectMeta: metav1.ObjectMeta{
			Name: "issued", Namespace: ext.Namespace,
			Labels: map[string]string{
				SecretKonnectDataPlaneCertificateLabel: "true", consts.SecretProvisioningLabelKey: consts.SecretProvisioningAutomaticLabelValue,
			},
		},
		Immutable: new(true),
		Data:      map[string][]byte{corev1.TLSCertKey: cert, corev1.TLSPrivateKeyKey: key},
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
		ObjectMeta: metav1.ObjectMeta{
			Name: "previous", Namespace: ext.Namespace,
			Labels:     map[string]string{SecretKonnectDataPlaneCertificateLabel: "true"},
			Finalizers: []string{KonnectCleanupFinalizer, consts.KonnectExtensionSecretInUseFinalizer},
		},
		Data: map[string][]byte{corev1.TLSCertKey: []byte("old")},
	}
	require.NoError(t, controllerutil.SetControllerReference(ext, oldSecret, r.Scheme()))
	require.NoError(t, r.Create(t.Context(), oldSecret))
	dp := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{Name: "dp", Namespace: ext.Namespace, UID: "dp"},
		Spec: operatorv1beta1.DataPlaneSpec{DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
			Extensions: []commonv1alpha1.ExtensionRef{
				{Group: konnectv1alpha2.GroupVersion.Group, Kind: konnectv1alpha2.KonnectExtensionKind,
					NamespacedRef: commonv1alpha1.NamespacedRef{Name: ext.Name}},
			},
		}},
	}
	require.NoError(t, r.Create(t.Context(), dp))
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "dp", Namespace: ext.Namespace, Generation: 1},
		Spec: appsv1.DeploymentSpec{Replicas: new(int32(1)), Paused: true,
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Volumes: []corev1.Volume{
				{Name: consts.KongClusterCertVolume, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: snapshot.Name}}},
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
		ObjectMeta: metav1.ObjectMeta{Name: "terminating", Namespace: ext.Namespace, Finalizers: []string{"test/hold"}},
		Spec: corev1.PodSpec{Volumes: []corev1.Volume{
			{Name: consts.KongClusterCertVolume, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: oldSecret.Name}}},
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

func TestKonnectExtensionRecoversDeletingCurrentSnapshot(t *testing.T) {
	r, ext := dpCertTestReconciler(t, "certificate")
	reconcile := func(program bool) {
		t.Helper()
		for range 16 {
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
			_, err := r.Reconcile(t.Context(), ext)
			require.NoError(t, err)
			if program {
				for _, registration := range dpCertTestList(t, r.Client) {
					if registration.GetKonnectID() == "" {
						registration.Status = dpCertTestProgrammedStatus("original-certificate-id")
						require.NoError(t, r.Status().Update(t.Context(), &registration))
					}
				}
			}
		}
	}
	reconcile(true)
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
	require.True(t, k8sutils.HasConditionTrue(konnectv1alpha2.KonnectExtensionReadyConditionType, ext))
	var original corev1.Secret
	require.NoError(t, r.Get(t.Context(), client.ObjectKey{
		Namespace: ext.Namespace, Name: ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name,
	}, &original))
	require.Contains(t, original.Finalizers, KonnectCleanupFinalizer)
	require.Contains(t, original.Finalizers, consts.KonnectExtensionSecretInUseFinalizer)
	dp := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{Name: "dp", Namespace: ext.Namespace, UID: "dp"},
		Spec: operatorv1beta1.DataPlaneSpec{DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
			Extensions: []commonv1alpha1.ExtensionRef{{
				Group: konnectv1alpha2.GroupVersion.Group, Kind: konnectv1alpha2.KonnectExtensionKind,
				NamespacedRef: commonv1alpha1.NamespacedRef{Name: ext.Name},
			}},
		}},
	}
	require.NoError(t, r.Create(t.Context(), dp))
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "dp", Namespace: ext.Namespace, Generation: 1},
		Spec: appsv1.DeploymentSpec{Replicas: new(int32(1)),
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Volumes: []corev1.Volume{
				{Name: consts.KongClusterCertVolume, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: original.Name}}},
			}}}},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
	}
	require.NoError(t, controllerutil.SetControllerReference(dp, deployment, r.Scheme()))
	require.NoError(t, r.Create(t.Context(), deployment))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "old-pod", Namespace: ext.Namespace, Finalizers: []string{"test/hold"}},
		Spec:       deployment.Spec.Template.Spec,
	}
	require.NoError(t, r.Create(t.Context(), pod))
	require.NoError(t, r.Delete(t.Context(), &original))
	reconcile(false)
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
	replacementName := ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name
	require.NotEqual(t, original.Name, replacementName)
	assert.True(t, k8sutils.HasConditionTrue(konnectv1alpha2.KonnectExtensionReadyConditionType, ext))
	var held corev1.Secret
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(&original), &held))
	assert.Equal(t, original.Finalizers, held.Finalizers)
	registrations := dpCertTestList(t, r.Client)
	require.Len(t, registrations, 1, "unchanged certificate contents must reuse the existing Konnect registration")
	registration := registrations["extension"]
	assert.Equal(t, "original-certificate-id", registration.GetKonnectID())
	assert.True(t, registration.DeletionTimestamp.IsZero())

	deployment.Spec.Template.Spec.Volumes[0].Secret.SecretName = replacementName
	require.NoError(t, r.Update(t.Context(), deployment))
	require.NoError(t, r.Delete(t.Context(), pod))
	reconcile(false)
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(&original), &held))
	assert.Equal(t, original.Finalizers, held.Finalizers, "terminating old Pods still protect the deleting snapshot")
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(pod), pod))
	pod.Finalizers = nil
	require.NoError(t, r.Update(t.Context(), pod))
	reconcile(false)
	assert.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(&original), &held)))
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
	assert.Equal(t, replacementName, ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name)
	assert.True(t, k8sutils.HasConditionTrue(konnectv1alpha2.KonnectExtensionReadyConditionType, ext))
	assert.Len(t, dpCertTestList(t, r.Client), 1)
	var source corev1.Secret
	require.NoError(t, r.Get(t.Context(), client.ObjectKey{Namespace: ext.Namespace, Name: "certificate"}, &source))
	assert.Equal(t, original.Data, source.Data)
	assert.Empty(t, source.Finalizers)
}
