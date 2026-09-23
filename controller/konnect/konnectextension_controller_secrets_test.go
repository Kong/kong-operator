package konnect

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestKonnectExtensionSharedSecretCleanup(t *testing.T) {
	for _, tt := range []struct {
		name           string
		controlPlane   bool
		deletingSecret bool
		statusRefOnly  bool
	}{
		{name: "missing ControlPlane"},
		{name: "missing ControlPlane and deleting Secret", deletingSecret: true},
		{name: "existing ControlPlane", controlPlane: true},
		{name: "existing ControlPlane and deleting Secret", controlPlane: true, deletingSecret: true},
		{name: "peer still references Secret in status", deletingSecret: true, statusRefOnly: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, secret, first, second := sharedSecretCleanupFixture(t, tt.controlPlane, tt.deletingSecret, true)
			if tt.statusRefOnly {
				second.Status.DataPlaneClientAuth = &konnectv1alpha2.DataPlaneClientAuthStatus{
					CertificateSecretRef: &konnectv1alpha2.SecretRef{Name: secret.Name},
				}
				require.NoError(t, r.Status().Update(t.Context(), second))
				second.Spec.ClientAuth.CertificateSecret.CertificateSecretRef.Name = "replacement-certificate"
				require.NoError(t, r.Update(t.Context(), second))
			}

			for range 16 {
				if !reconcileExtensionForCleanup(t, r, first) {
					break
				}
				if !tt.statusRefOnly {
					assertSharedSecretProtected(t, r.Client, secret)
				}
			}
			if tt.statusRefOnly {
				assertExtensionDeleted(t, r.Client, first)
				var got corev1.Secret
				assert.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(secret), &got)))
				var surviving konnectv1alpha2.KonnectExtension
				require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(second), &surviving))
				assert.True(t, surviving.DeletionTimestamp.IsZero())
				assert.Contains(t, surviving.Finalizers, KonnectCleanupFinalizer)
				return
			}
			assertExtensionDeleted(t, r.Client, first)
			assertSharedSecretProtected(t, r.Client, secret)
			var surviving konnectv1alpha2.KonnectExtension
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(second), &surviving))
			assert.True(t, surviving.DeletionTimestamp.IsZero())
			assert.Contains(t, surviving.Finalizers, KonnectCleanupFinalizer)

			// The last extension must release the shared finalizers, including
			// when its status still points at the Secret used before a spec change.
			require.NoError(t, r.Delete(t.Context(), &surviving))
			for range 16 {
				if !reconcileExtensionForCleanup(t, r, second) {
					break
				}
			}
			assertExtensionDeleted(t, r.Client, second)
			var got corev1.Secret
			err := r.Get(t.Context(), client.ObjectKeyFromObject(secret), &got)
			if tt.deletingSecret {
				assert.True(t, apierrors.IsNotFound(err), "expected Secret deletion, got %v", err)
			} else {
				require.NoError(t, err)
				assert.Empty(t, got.Finalizers)
				assert.Equal(t, secret.Data, got.Data)
			}
		})
	}
}

func TestKonnectExtensionSharedSecretWaitsForPeerCertificateCleanup(t *testing.T) {
	r, secret, first, second := sharedSecretCleanupFixture(t, false, true, true)
	require.NoError(t, r.Delete(t.Context(), second))
	certificate := &configurationv1alpha1.KongDataPlaneClientCertificate{
		Name:       "peer-certificate",
		Namespace:  secret.Namespace,
		Finalizers: []string{KonnectCleanupFinalizer},
		Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
			Cert: string(secret.Data[consts.TLSCRT]),
		},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: konnectv1alpha2.GroupVersion.String(),
			Kind:       konnectv1alpha2.KonnectExtensionKind,
			Name:       second.Name,
			UID:        second.UID,
		}},
	}
	require.NoError(t, r.Create(t.Context(), certificate))

	// The cached view still sees the peer as active. Cleanup decisions must
	// use the live reader, otherwise the first extension could leave too soon.
	liveClient, ok := r.Client.(client.WithWatch)
	require.True(t, ok)
	r.Client = interceptor.NewClient(liveClient, interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if extensions, ok := list.(*konnectv1alpha2.KonnectExtensionList); ok {
				extensions.Items = []konnectv1alpha2.KonnectExtension{*second.DeepCopy()}
				return nil
			}
			return cl.List(ctx, list, opts...)
		},
	})
	for range 8 {
		require.True(t, reconcileExtensionForCleanup(t, r, first))
	}
	assertSharedSecretProtected(t, liveClient, secret)
	var current konnectv1alpha2.KonnectExtension
	require.NoError(t, liveClient.Get(t.Context(), client.ObjectKeyFromObject(first), &current))
	assert.Contains(t, current.Finalizers, KonnectCleanupFinalizer)

	for range 8 {
		require.True(t, reconcileExtensionForCleanup(t, r, second))
	}
	require.NoError(t, liveClient.Get(t.Context(), client.ObjectKeyFromObject(certificate), certificate))
	require.False(t, certificate.DeletionTimestamp.IsZero())
	assertSharedSecretProtected(t, liveClient, secret)

	// Simulate the certificate controller completing remote cleanup.
	certificate.Finalizers = nil
	require.NoError(t, liveClient.Update(t.Context(), certificate))
	r.Client = liveClient
	reconcileSharedExtensionsConcurrently(t, r, first, second)
	assertExtensionDeleted(t, liveClient, first)
	assertExtensionDeleted(t, liveClient, second)
	var got corev1.Secret
	assert.True(t, apierrors.IsNotFound(liveClient.Get(t.Context(), client.ObjectKeyFromObject(secret), &got)))
}

func TestKonnectExtensionSharedSecretConcurrentCleanup(t *testing.T) {
	r, secret, first, second := sharedSecretCleanupFixture(t, false, true, true)
	require.NoError(t, r.Delete(t.Context(), second))
	reconcileSharedExtensionsConcurrently(t, r, first, second)
	assertExtensionDeleted(t, r.Client, first)
	assertExtensionDeleted(t, r.Client, second)
	var got corev1.Secret
	assert.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(secret), &got)))
}

func TestKonnectExtensionSharedSecretWaitsForOwnCertificateCleanup(t *testing.T) {
	r, secret, first, _ := sharedSecretCleanupFixture(t, true, true, true)
	// Even a certificate without a Konnect ID must finish deletion before the
	// extension can leave and hand Secret protection to an active peer.
	certificate := &configurationv1alpha1.KongDataPlaneClientCertificate{
		Name: "unprovisioned-certificate", Namespace: secret.Namespace,
		Finalizers: []string{KonnectCleanupFinalizer},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: konnectv1alpha2.GroupVersion.String(),
			Kind:       konnectv1alpha2.KonnectExtensionKind,
			Name:       first.Name,
			UID:        first.UID,
		}},
	}
	require.NoError(t, r.Create(t.Context(), certificate))
	for range 16 {
		require.True(t, reconcileExtensionForCleanup(t, r, first))
	}
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(certificate), certificate))
	require.False(t, certificate.DeletionTimestamp.IsZero())
	assertSharedSecretProtected(t, r.Client, secret)

	certificate.Finalizers = nil
	require.NoError(t, r.Update(t.Context(), certificate))
	for range 16 {
		if !reconcileExtensionForCleanup(t, r, first) {
			break
		}
	}
	assertExtensionDeleted(t, r.Client, first)
	assertSharedSecretProtected(t, r.Client, secret)
}

func TestKonnectExtensionSharedSecretLookupFailurePreservesFinalizers(t *testing.T) {
	r, secret, first, _ := sharedSecretCleanupFixture(t, false, true, true)
	liveClient, ok := r.Client.(client.WithWatch)
	require.True(t, ok)
	lookupErr := errors.New("extension lookup failed")
	r.apiReader = interceptor.NewClient(liveClient, interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return lookupErr
		},
	})
	var reconcileErr error
	for range 8 {
		var current konnectv1alpha2.KonnectExtension
		require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(first), &current))
		_, reconcileErr = r.Reconcile(t.Context(), &current)
		if reconcileErr != nil {
			break
		}
	}
	require.ErrorIs(t, reconcileErr, lookupErr)
	assertSharedSecretProtected(t, liveClient, secret)
	var current konnectv1alpha2.KonnectExtension
	require.NoError(t, liveClient.Get(t.Context(), client.ObjectKeyFromObject(first), &current))
	assert.Contains(t, current.Finalizers, KonnectCleanupFinalizer)
}

func TestKonnectExtensionStatusOnlyPeerWaitsForMatchingCertificate(t *testing.T) {
	r, secret, first, second := sharedSecretCleanupFixture(t, false, true, true)
	var peer konnectv1alpha2.KonnectExtension
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(second), &peer))
	peer.Status.DataPlaneClientAuth = &konnectv1alpha2.DataPlaneClientAuthStatus{
		CertificateSecretRef: &konnectv1alpha2.SecretRef{Name: secret.Name},
	}
	require.NoError(t, r.Status().Update(t.Context(), &peer))
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(second), &peer))
	peer.Spec.ClientAuth.CertificateSecret.CertificateSecretRef.Name = "replacement-certificate"
	require.NoError(t, r.Update(t.Context(), &peer))

	certificate := &configurationv1alpha1.KongDataPlaneClientCertificate{
		Name:      "peer-certificate",
		Namespace: secret.Namespace,
		Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
			Cert: string(secret.Data[consts.TLSCRT]),
		},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: konnectv1alpha2.GroupVersion.String(),
			Kind:       konnectv1alpha2.KonnectExtensionKind,
			Name:       second.Name,
			UID:        second.UID,
		}},
	}
	require.NoError(t, r.Create(t.Context(), certificate))

	for range 8 {
		require.True(t, reconcileExtensionForCleanup(t, r, first))
	}
	assertSharedSecretProtected(t, r.Client, secret)
	var waiting konnectv1alpha2.KonnectExtension
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(first), &waiting))
	assert.Contains(t, waiting.Finalizers, KonnectCleanupFinalizer)

	require.NoError(t, r.Delete(t.Context(), certificate))
	for range 16 {
		if !reconcileExtensionForCleanup(t, r, first) {
			break
		}
	}
	assertExtensionDeleted(t, r.Client, first)
	var got corev1.Secret
	assert.True(t, apierrors.IsNotFound(r.Get(t.Context(), client.ObjectKeyFromObject(secret), &got)))
}

func TestKonnectExtensionStaleSecretOwnerDoesNotBlockCleanup(t *testing.T) {
	r, secret, first, second := sharedSecretCleanupFixture(t, false, false, true)
	var peer konnectv1alpha2.KonnectExtension
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(second), &peer))
	peer.Spec.ClientAuth.CertificateSecret.CertificateSecretRef.Name = "replacement-certificate"
	require.NoError(t, r.Update(t.Context(), &peer))
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(second), &peer))
	peer.Status.DataPlaneClientAuth = &konnectv1alpha2.DataPlaneClientAuthStatus{
		CertificateSecretRef: &konnectv1alpha2.SecretRef{Name: "replacement-certificate"},
	}
	require.NoError(t, r.Status().Update(t.Context(), &peer))

	var oldSecret corev1.Secret
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(secret), &oldSecret))
	oldSecret.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: konnectv1alpha2.GroupVersion.String(),
		Kind:       konnectv1alpha2.KonnectExtensionKind,
		Name:       second.Name,
		UID:        second.UID,
		Controller: new(true),
	}}
	require.NoError(t, r.Update(t.Context(), &oldSecret))

	for range 16 {
		if !reconcileExtensionForCleanup(t, r, first) {
			break
		}
	}
	assertExtensionDeleted(t, r.Client, first)
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(secret), &oldSecret))
	assert.Empty(t, oldSecret.Finalizers)
}

func TestKonnectExtensionMissingControlPlanePreservesSharedSecret(t *testing.T) {
	r, secret, first, _ := sharedSecretCleanupFixture(t, false, false, false)
	for range 8 {
		require.True(t, reconcileExtensionForCleanup(t, r, first))
	}
	assertSharedSecretProtected(t, r.Client, secret)
}

func TestKonnectExtensionCleanupHandlesAllOwnedSecretsBeforeDeletion(t *testing.T) {
	r, sharedSecret, first, second := sharedSecretCleanupFixture(t, false, false, false)

	automatic := konnectv1alpha2.AutomaticSecretProvisioning
	var deletingExtension konnectv1alpha2.KonnectExtension
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(first), &deletingExtension))
	deletingExtension.Spec.ClientAuth.CertificateSecret.Provisioning = &automatic
	deletingExtension.Spec.ClientAuth.CertificateSecret.CertificateSecretRef = nil
	require.NoError(t, r.Update(t.Context(), &deletingExtension))
	require.NoError(t, r.Delete(t.Context(), &deletingExtension))

	var shared corev1.Secret
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(sharedSecret), &shared))
	shared.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: konnectv1alpha2.GroupVersion.String(),
		Kind:       konnectv1alpha2.KonnectExtensionKind,
		Name:       first.Name,
		UID:        first.UID,
		Controller: new(true),
	}}
	require.NoError(t, r.Update(t.Context(), &shared))

	staleSecret := shared.DeepCopy()
	staleSecret.ResourceVersion = ""
	staleSecret.UID = ""
	staleSecret.Name = "stale-owned-certificate"
	staleSecret.Data = map[string][]byte{consts.TLSCRT: []byte("stale-certificate")}
	require.NoError(t, r.Create(t.Context(), staleSecret))

	for range 16 {
		if !reconcileExtensionForCleanup(t, r, first) {
			break
		}
	}
	assertExtensionDeleted(t, r.Client, first)

	var gotShared corev1.Secret
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(sharedSecret), &gotShared))
	assertSharedSecretProtected(t, r.Client, sharedSecret)
	require.Len(t, gotShared.OwnerReferences, 1)
	assert.Equal(t, second.Name, gotShared.OwnerReferences[0].Name)
	assert.Equal(t, second.UID, gotShared.OwnerReferences[0].UID)

	var gotStale corev1.Secret
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(staleSecret), &gotStale))
	assert.Empty(t, gotStale.Finalizers)
}

func sharedSecretCleanupFixture(t *testing.T, controlPlane, deletingSecret, deletingFirst bool) (
	*KonnectExtensionReconciler, *corev1.Secret, *konnectv1alpha2.KonnectExtension, *konnectv1alpha2.KonnectExtension,
) {
	t.Helper()
	secret := &corev1.Secret{
		Name:       "shared-certificate",
		Namespace:  "default",
		Labels:     map[string]string{SecretKonnectDataPlaneCertificateLabel: "true"},
		Data:       map[string][]byte{consts.TLSCRT: []byte("certificate")},
		Finalizers: []string{consts.KonnectExtensionSecretInUseFinalizer, KonnectCleanupFinalizer},
	}
	if deletingSecret {
		secret.DeletionTimestamp = new(metav1.Now())
	}
	newExtension := func(name string) *konnectv1alpha2.KonnectExtension {
		return &konnectv1alpha2.KonnectExtension{
			Name: name, Namespace: secret.Namespace, UID: types.UID(name),
			Finalizers: []string{KonnectCleanupFinalizer},
			Spec: konnectv1alpha2.KonnectExtensionSpec{
				ClientAuth: &konnectv1alpha2.KonnectExtensionClientAuth{
					CertificateSecret: konnectv1alpha2.CertificateSecret{
						Provisioning:         new(konnectv1alpha2.ManualSecretProvisioning),
						CertificateSecretRef: &konnectv1alpha2.SecretRef{Name: secret.Name},
					},
				},
				Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
					ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
						Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
							Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: "control-plane"},
						},
					},
				},
			},
		}
	}
	first, second := newExtension("extension-a"), newExtension("extension-b")
	if deletingFirst {
		first.DeletionTimestamp = new(metav1.Now())
	}
	objects := []client.Object{secret, first, second}
	if controlPlane {
		objects = append(objects,
			&konnectv1alpha2.KonnectGatewayControlPlane{
				Name: "control-plane", Namespace: secret.Namespace,
				Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
					KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{Name: "auth"},
					},
				},
				Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
					KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "control-plane-id"},
					Conditions:          []metav1.Condition{{Type: konnectv1alpha1.KonnectEntityProgrammedConditionType, Status: metav1.ConditionTrue}},
				},
			},
			&konnectv1alpha1.KonnectAPIAuthConfiguration{
				Name: "auth", Namespace: secret.Namespace,
				Status: konnectv1alpha1.KonnectAPIAuthConfigurationStatus{
					Conditions: []metav1.Condition{{
						Type:   konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
						Status: metav1.ConditionTrue,
						Reason: konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid,
					}},
				},
			},
		)
	}
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).
		WithStatusSubresource(first, second).
		WithIndex(&operatorv1beta1.DataPlane{}, index.KonnectExtensionIndex, func(client.Object) []string { return nil }).
		WithIndex(&gwtypes.ControlPlane{}, index.KonnectExtensionIndex, func(client.Object) []string { return nil }).
		WithIndex(&configurationv1alpha1.KongDataPlaneClientCertificate{}, index.IndexFieldKongDataPlaneClientCertificateOnKonnectExtensionOwner,
			func(obj client.Object) []string {
				var names []string
				for _, owner := range obj.GetOwnerReferences() {
					names = append(names, owner.Name)
				}
				return names
			}).Build()
	return &KonnectExtensionReconciler{Client: cl, apiReader: cl}, secret, first, second
}

func reconcileExtensionForCleanup(t *testing.T, r *KonnectExtensionReconciler, ext *konnectv1alpha2.KonnectExtension) bool {
	t.Helper()
	var current konnectv1alpha2.KonnectExtension
	err := r.Get(t.Context(), client.ObjectKeyFromObject(ext), &current)
	if apierrors.IsNotFound(err) {
		return false
	}
	require.NoError(t, err)
	_, err = r.Reconcile(t.Context(), &current)
	require.NoError(t, err)
	return true
}

func reconcileSharedExtensionsConcurrently(t *testing.T, r *KonnectExtensionReconciler, extensions ...*konnectv1alpha2.KonnectExtension) {
	t.Helper()
	for range 16 {
		results := make(chan error, len(extensions))
		start := make(chan struct{})
		for _, ext := range extensions {
			go func() {
				<-start
				var current konnectv1alpha2.KonnectExtension
				err := r.Get(t.Context(), client.ObjectKeyFromObject(ext), &current)
				if err == nil {
					_, err = r.Reconcile(t.Context(), &current)
				}
				results <- client.IgnoreNotFound(err)
			}()
		}
		close(start)
		for range extensions {
			require.NoError(t, <-results)
		}
	}
}

func assertSharedSecretProtected(t *testing.T, cl client.Client, secret *corev1.Secret) {
	t.Helper()
	var got corev1.Secret
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(secret), &got))
	assert.Contains(t, got.Finalizers, consts.KonnectExtensionSecretInUseFinalizer)
	assert.Contains(t, got.Finalizers, KonnectCleanupFinalizer)
	assert.Equal(t, secret.Data, got.Data)
}

func assertExtensionDeleted(t *testing.T, cl client.Client, ext *konnectv1alpha2.KonnectExtension) {
	t.Helper()
	var got konnectv1alpha2.KonnectExtension
	err := cl.Get(t.Context(), client.ObjectKeyFromObject(ext), &got)
	assert.True(t, apierrors.IsNotFound(err), "expected %s deletion, got %v", ext.Name, err)
}

// TestKonnectExtensionFinalizerPrecedesSecretFinalizers verifies the invariant that
// protects against orphaned certificate Secrets in Terminating namespaces: by the time
// the extension's reconcile stamps any cleanup finalizer on the certificate Secret, the
// extension itself must already carry the konnect-cleanup finalizer. Only the extension's
// own reconcile can release the Secret's finalizers, so an extension that could be
// garbage-collected before acquiring its own finalizer would leave the Secret (and hence
// the whole namespace) permanently stuck in Terminating.
func TestKonnectExtensionFinalizerPrecedesSecretFinalizers(t *testing.T) {
	const (
		namespace = "default"
		extName   = "extension"
	)

	secret := &corev1.Secret{
		Name:      "certificate",
		Namespace: namespace,
		Labels:    map[string]string{SecretKonnectDataPlaneCertificateLabel: "true"},
		Data:      map[string][]byte{consts.TLSCRT: []byte("certificate")},
	}
	ext := &konnectv1alpha2.KonnectExtension{
		Name: extName, Namespace: namespace, UID: types.UID(extName),
		Spec: konnectv1alpha2.KonnectExtensionSpec{
			ClientAuth: &konnectv1alpha2.KonnectExtensionClientAuth{
				CertificateSecret: konnectv1alpha2.CertificateSecret{
					Provisioning:         new(konnectv1alpha2.ManualSecretProvisioning),
					CertificateSecretRef: &konnectv1alpha2.SecretRef{Name: secret.Name},
				},
			},
			Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
				ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
					Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
						Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
							Name: "control-plane",
						},
					},
				},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).
		WithObjects(
			secret,
			ext,
			&konnectv1alpha2.KonnectGatewayControlPlane{
				Name: "control-plane", Namespace: namespace,
				Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
					KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{Name: "auth"},
					},
				},
				Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
					KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "control-plane-id"},
					Conditions:          []metav1.Condition{{Type: konnectv1alpha1.KonnectEntityProgrammedConditionType, Status: metav1.ConditionTrue}},
				},
			},
			&konnectv1alpha1.KonnectAPIAuthConfiguration{
				Name: "auth", Namespace: namespace,
				Status: konnectv1alpha1.KonnectAPIAuthConfigurationStatus{
					Conditions: []metav1.Condition{{
						Type:   konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
						Status: metav1.ConditionTrue,
						Reason: konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid,
					}},
				},
			},
		).
		WithStatusSubresource(ext).
		WithIndex(&operatorv1beta1.DataPlane{}, index.KonnectExtensionIndex, func(client.Object) []string { return nil }).
		WithIndex(&gwtypes.ControlPlane{}, index.KonnectExtensionIndex, func(client.Object) []string { return nil }).
		WithIndex(&configurationv1alpha1.KongDataPlaneClientCertificate{}, index.IndexFieldKongDataPlaneClientCertificateOnKonnectExtensionOwner,
			func(obj client.Object) []string {
				var names []string
				for _, owner := range obj.GetOwnerReferences() {
					names = append(names, owner.Name)
				}
				return names
			}).Build()
	r := &KonnectExtensionReconciler{Client: cl, apiReader: cl}

	extNN := client.ObjectKeyFromObject(ext)
	secretNN := client.ObjectKeyFromObject(secret)
	for range 16 {
		var current konnectv1alpha2.KonnectExtension
		require.NoError(t, r.Get(t.Context(), extNN, &current))
		_, err := r.Reconcile(t.Context(), &current)
		require.NoError(t, err)

		var gotExt konnectv1alpha2.KonnectExtension
		require.NoError(t, r.Get(t.Context(), extNN, &gotExt))

		var gotSecret corev1.Secret
		require.NoError(t, r.Get(t.Context(), secretNN, &gotSecret))
		if controllerutil.ContainsFinalizer(&gotSecret, consts.KonnectExtensionSecretInUseFinalizer) ||
			controllerutil.ContainsFinalizer(&gotSecret, KonnectCleanupFinalizer) {
			require.Contains(t, gotExt.Finalizers, KonnectCleanupFinalizer,
				"the certificate Secret gained a cleanup finalizer while the KonnectExtension had none: "+
					"deleting the extension now would orphan the Secret in a Terminating namespace")
			return
		}
	}
	t.Fatal("the certificate Secret never gained a cleanup finalizer")
}
