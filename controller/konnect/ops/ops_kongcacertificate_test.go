package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/metadata"
)

func TestKongCACertificateToCACertificateInput_Tags(t *testing.T) {
	ctx := t.Context()
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()

	cert := &configurationv1alpha1.KongCACertificate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongCACertificate",
			APIVersion: "configuration.konghq.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "cert-1",
			Namespace:  "default",
			Generation: 2,
			UID:        k8stypes.UID(uuid.NewString()),
			Annotations: map[string]string{
				metadata.AnnotationKeyTags: "tag1,tag2,duplicate",
			},
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
				Cert: "cert",
				Tags: []string{"tag3", "tag4", "duplicate"},
			},
		},
	}
	output, err := kongCACertificateToCACertificateInput(ctx, cl, cert)
	require.NoError(t, err)
	expectedTags := []string{
		"k8s-generation:2",
		"k8s-kind:KongCACertificate",
		"k8s-name:cert-1",
		"k8s-uid:" + string(cert.GetUID()),
		"k8s-version:v1alpha1",
		"k8s-group:configuration.konghq.com",
		"k8s-namespace:default",
		"tag1",
		"tag2",
		"tag3",
		"tag4",
		"duplicate",
	}
	require.ElementsMatch(t, expectedTags, output.Tags)
}

func TestAdoptKongCACertificateOverride(t *testing.T) {
	ctx := t.Context()
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	sdk := mocks.NewMockCACertificatesSDK(t)
	sdk.EXPECT().GetCaCertificate(mock.Anything, "konnect-ca-id", "cp-1").Return(
		&sdkkonnectops.GetCaCertificateResponse{
			CACertificate: &sdkkonnectcomp.CACertificate{
				Cert: "ca-cert",
			},
		},
		nil,
	)
	sdk.EXPECT().UpsertCaCertificate(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpsertCaCertificateRequest) bool {
		return req.ControlPlaneID == "cp-1" &&
			req.CACertificateID == "konnect-ca-id" &&
			req.CACertificate.Cert == "ca-cert"
	})).Return(&sdkkonnectops.UpsertCaCertificateResponse{}, nil)

	cert := &configurationv1alpha1.KongCACertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-ca-id",
				},
			},
			KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
				Cert: "ca-cert",
			},
		},
		Status: configurationv1alpha1.KongCACertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptCACertificate(ctx, cl, sdk, cert)
	require.NoError(t, err)
	assert.Equal(t, "konnect-ca-id", cert.GetKonnectID())
}

func TestAdoptKongCACertificateMatchSuccess(t *testing.T) {
	ctx := t.Context()
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	sdk := mocks.NewMockCACertificatesSDK(t)
	sdk.EXPECT().GetCaCertificate(mock.Anything, "konnect-ca-id", "cp-1").Return(
		&sdkkonnectops.GetCaCertificateResponse{
			CACertificate: &sdkkonnectcomp.CACertificate{
				Cert: "ca-cert",
			},
		},
		nil,
	)

	cert := &configurationv1alpha1.KongCACertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-ca-id",
				},
			},
			KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
				Cert: "ca-cert",
			},
		},
		Status: configurationv1alpha1.KongCACertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptCACertificate(ctx, cl, sdk, cert)
	require.NoError(t, err)
	assert.Equal(t, "konnect-ca-id", cert.GetKonnectID())
}

func TestAdoptKongCACertificateMatchMismatch(t *testing.T) {
	ctx := t.Context()
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	sdk := mocks.NewMockCACertificatesSDK(t)
	sdk.EXPECT().GetCaCertificate(mock.Anything, "konnect-ca-id", "cp-1").Return(
		&sdkkonnectops.GetCaCertificateResponse{
			CACertificate: &sdkkonnectcomp.CACertificate{
				Cert: "other-cert",
			},
		},
		nil,
	)

	cert := &configurationv1alpha1.KongCACertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-ca-id",
				},
			},
			KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
				Cert: "ca-cert",
			},
		},
		Status: configurationv1alpha1.KongCACertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptCACertificate(ctx, cl, sdk, cert)
	require.Error(t, err)
	var notMatchErr KonnectEntityAdoptionNotMatchError
	assert.ErrorAs(t, err, &notMatchErr)
	assert.Empty(t, cert.GetKonnectID())
}

func TestAdoptKongCACertificateUIDConflict(t *testing.T) {
	ctx := t.Context()
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	sdk := mocks.NewMockCACertificatesSDK(t)
	sdk.EXPECT().GetCaCertificate(mock.Anything, "konnect-ca-id", "cp-1").Return(
		&sdkkonnectops.GetCaCertificateResponse{
			CACertificate: &sdkkonnectcomp.CACertificate{
				Cert: "ca-cert",
				Tags: []string{"k8s-uid:other-uid"},
			},
		},
		nil,
	)

	cert := &configurationv1alpha1.KongCACertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-ca-id",
				},
			},
			KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
				Cert: "ca-cert",
			},
		},
		Status: configurationv1alpha1.KongCACertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptCACertificate(ctx, cl, sdk, cert)
	require.Error(t, err)
	var uidConflict KonnectEntityAdoptionUIDTagConflictError
	assert.ErrorAs(t, err, &uidConflict)
	assert.Empty(t, cert.GetKonnectID())
}

func TestAdoptKongCACertificateFetchFailure(t *testing.T) {
	ctx := t.Context()
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	sdk := mocks.NewMockCACertificatesSDK(t)
	sdk.EXPECT().GetCaCertificate(mock.Anything, "konnect-ca-id", "cp-1").Return(
		(*sdkkonnectops.GetCaCertificateResponse)(nil),
		&sdkkonnecterrs.NotFoundError{},
	)

	cert := &configurationv1alpha1.KongCACertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-ca-id",
				},
			},
			KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
				Cert: "ca-cert",
			},
		},
		Status: configurationv1alpha1.KongCACertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	err := adoptCACertificate(ctx, cl, sdk, cert)
	require.Error(t, err)
	var fetchErr KonnectEntityAdoptionFetchError
	assert.ErrorAs(t, err, &fetchErr)
	assert.Empty(t, cert.GetKonnectID())
}

func TestFetchCACertDataFromSecret(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name            string
		parentNamespace string
		secretRef       *commonv1alpha1.NamespacedRef
		clientObjs      []client.Object
		wantErr         string
		wantCert        string
	}{
		{
			name:            "nil secretRef returns error",
			parentNamespace: "default",
			secretRef:       nil,
			clientObjs:      []client.Object{},
			wantErr:         "secretRef is nil",
		},
		{
			name:            "uses parent namespace when secretRef.Namespace is nil",
			parentNamespace: "parent-ns",
			secretRef: &commonv1alpha1.NamespacedRef{
				Name:      "ca-secret",
				Namespace: nil,
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ca-secret",
						Namespace: "parent-ns",
					},
					Data: map[string][]byte{
						"ca.crt": []byte("parent-ns-cert"),
					},
				},
			},
			wantCert: "parent-ns-cert",
		},
		{
			name:            "uses parent namespace when secretRef.Namespace is empty string",
			parentNamespace: "parent-ns",
			secretRef: &commonv1alpha1.NamespacedRef{
				Name:      "ca-secret",
				Namespace: lo.ToPtr(""),
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ca-secret",
						Namespace: "parent-ns",
					},
					Data: map[string][]byte{
						"ca.crt": []byte("parent-ns-cert"),
					},
				},
			},
			wantCert: "parent-ns-cert",
		},
		{
			name:            "valid secret with ca.crt",
			parentNamespace: "default",
			secretRef: &commonv1alpha1.NamespacedRef{
				Name:      "ca-secret",
				Namespace: lo.ToPtr("default"),
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ca-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"ca.crt": []byte("ca-cert-content"),
					},
				},
			},
			wantCert: "ca-cert-content",
		},
		{
			name:            "secret not found",
			parentNamespace: "default",
			secretRef: &commonv1alpha1.NamespacedRef{
				Name:      "missing-secret",
				Namespace: lo.ToPtr("default"),
			},
			clientObjs: []client.Object{},
			wantErr:    "failed to fetch Secret default/missing-secret",
		},
		{
			name:            "secret missing ca.crt key",
			parentNamespace: "default",
			secretRef: &commonv1alpha1.NamespacedRef{
				Name:      "incomplete-secret",
				Namespace: lo.ToPtr("default"),
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "incomplete-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"other-key": []byte("some-data"),
					},
				},
			},
			wantErr: "secret default/incomplete-secret is missing key 'ca.crt'",
		},
		{
			name:            "empty secret data",
			parentNamespace: "default",
			secretRef: &commonv1alpha1.NamespacedRef{
				Name:      "empty-secret",
				Namespace: lo.ToPtr("default"),
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "empty-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{},
				},
			},
			wantErr: "secret default/empty-secret is missing key 'ca.crt'",
		},
		{
			name:            "secret with empty ca.crt value",
			parentNamespace: "default",
			secretRef: &commonv1alpha1.NamespacedRef{
				Name:      "empty-cert",
				Namespace: lo.ToPtr("test-ns"),
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "empty-cert",
						Namespace: "test-ns",
					},
					Data: map[string][]byte{
						"ca.crt": []byte(""),
					},
				},
			},
			wantCert: "",
		},
		{
			name:            "secret in different namespace",
			parentNamespace: "default",
			secretRef: &commonv1alpha1.NamespacedRef{
				Name:      "cross-ns-secret",
				Namespace: lo.ToPtr("other-namespace"),
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cross-ns-secret",
						Namespace: "other-namespace",
					},
					Data: map[string][]byte{
						"ca.crt": []byte("cross-ns-cert"),
					},
				},
			},
			wantCert: "cross-ns-cert",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(tt.clientObjs...).Build()
			certData, err := fetchCACertDataFromSecret(ctx, cl, tt.parentNamespace, tt.secretRef)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantCert, certData)
		})
	}
}

func TestKongCACertificateToCACertificateInput(t *testing.T) {
	ctx := t.Context()

	t.Run("inline type uses cert from spec", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		cert := &configurationv1alpha1.KongCACertificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ca",
				Namespace: "default",
			},
			Spec: configurationv1alpha1.KongCACertificateSpec{
				Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeInline),
				KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
					Cert: "inline-ca-cert",
				},
			},
		}

		output, err := kongCACertificateToCACertificateInput(ctx, cl, cert)
		require.NoError(t, err)
		assert.Equal(t, "inline-ca-cert", output.Cert)
	})

	t.Run("secretRef type fetches cert from secret", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ca-tls-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"ca.crt": []byte("secret-ca-cert"),
			},
		}
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(secret).Build()

		cert := &configurationv1alpha1.KongCACertificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ca",
				Namespace: "default",
			},
			Spec: configurationv1alpha1.KongCACertificateSpec{
				Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
				SecretRef: &commonv1alpha1.NamespacedRef{
					Name: "ca-tls-secret",
				},
			},
		}

		output, err := kongCACertificateToCACertificateInput(ctx, cl, cert)
		require.NoError(t, err)
		assert.Equal(t, "secret-ca-cert", output.Cert)
	})

	t.Run("secretRef type with nil secretRef returns error", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		cert := &configurationv1alpha1.KongCACertificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ca",
				Namespace: "default",
			},
			Spec: configurationv1alpha1.KongCACertificateSpec{
				Type:      lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
				SecretRef: nil,
			},
		}

		_, err := kongCACertificateToCACertificateInput(ctx, cl, cert)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "secretRef is nil")
	})

	t.Run("secretRef type with missing secret returns error", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		cert := &configurationv1alpha1.KongCACertificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ca",
				Namespace: "default",
			},
			Spec: configurationv1alpha1.KongCACertificateSpec{
				Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
				SecretRef: &commonv1alpha1.NamespacedRef{
					Name: "missing-secret",
				},
			},
		}

		_, err := kongCACertificateToCACertificateInput(ctx, cl, cert)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch Secret")
	})

	t.Run("default type (nil) uses inline cert", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		cert := &configurationv1alpha1.KongCACertificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ca",
				Namespace: "default",
			},
			Spec: configurationv1alpha1.KongCACertificateSpec{
				Type: nil,
				KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
					Cert: "default-inline-cert",
				},
			},
		}

		output, err := kongCACertificateToCACertificateInput(ctx, cl, cert)
		require.NoError(t, err)
		assert.Equal(t, "default-inline-cert", output.Cert)
	})
}

func TestCreateCACertificate(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name       string
		cert       *configurationv1alpha1.KongCACertificate
		clientObjs []client.Object
		setupMock  func(*mocks.MockCACertificatesSDK)
		wantErr    string
		wantID     string
	}{
		{
			name: "successful create with inline certificate",
			cert: &configurationv1alpha1.KongCACertificate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongCACertificate",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-ca",
					Namespace:  "default",
					Generation: 1,
					UID:        "test-uid",
				},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeInline),
					KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
						Cert: "ca-cert-data",
					},
				},
				Status: configurationv1alpha1.KongCACertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock: func(sdk *mocks.MockCACertificatesSDK) {
				sdk.EXPECT().CreateCaCertificate(mock.Anything, "cp-1", mock.MatchedBy(func(cert sdkkonnectcomp.CACertificate) bool {
					return cert.Cert == "ca-cert-data"
				})).Return(&sdkkonnectops.CreateCaCertificateResponse{
					CACertificate: &sdkkonnectcomp.CACertificate{
						ID: lo.ToPtr("new-ca-id"),
					},
				}, nil)
			},
			wantID: "new-ca-id",
		},
		{
			name: "successful create with secretRef",
			cert: &configurationv1alpha1.KongCACertificate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongCACertificate",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-ca",
					Namespace:  "default",
					Generation: 1,
					UID:        "test-uid",
				},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					Type:      lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
					SecretRef: &commonv1alpha1.NamespacedRef{Name: "ca-secret", Namespace: lo.ToPtr("default")},
				},
				Status: configurationv1alpha1.KongCACertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "ca-secret", Namespace: "default"},
					Data: map[string][]byte{
						"ca.crt": []byte("secret-ca-cert-data"),
					},
				},
			},
			setupMock: func(sdk *mocks.MockCACertificatesSDK) {
				sdk.EXPECT().CreateCaCertificate(mock.Anything, "cp-1", mock.MatchedBy(func(cert sdkkonnectcomp.CACertificate) bool {
					return cert.Cert == "secret-ca-cert-data"
				})).Return(&sdkkonnectops.CreateCaCertificateResponse{
					CACertificate: &sdkkonnectcomp.CACertificate{
						ID: lo.ToPtr("new-ca-id-from-secret"),
					},
				}, nil)
			},
			wantID: "new-ca-id-from-secret",
		},
		{
			name: "missing control plane ID returns error",
			cert: &configurationv1alpha1.KongCACertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ca",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeInline),
					KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
						Cert: "ca-cert-data",
					},
				},
				Status: configurationv1alpha1.KongCACertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{},
				},
			},
			clientObjs: []client.Object{},
			setupMock:  func(sdk *mocks.MockCACertificatesSDK) {},
			wantErr:    "can't create KongCACertificate",
		},
		{
			name: "failed to fetch secret returns error",
			cert: &configurationv1alpha1.KongCACertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ca",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					Type:      lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
					SecretRef: &commonv1alpha1.NamespacedRef{Name: "missing-secret"},
				},
				Status: configurationv1alpha1.KongCACertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock:  func(sdk *mocks.MockCACertificatesSDK) {},
			wantErr:    "failed to fetch Secret",
		},
		{
			name: "SDK error returns wrapped error",
			cert: &configurationv1alpha1.KongCACertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ca",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeInline),
					KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
						Cert: "ca-cert-data",
					},
				},
				Status: configurationv1alpha1.KongCACertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock: func(sdk *mocks.MockCACertificatesSDK) {
				sdk.EXPECT().CreateCaCertificate(mock.Anything, "cp-1", mock.Anything).Return(
					nil,
					&sdkkonnecterrs.SDKError{Message: "API error", StatusCode: 500},
				)
			},
			wantErr: "failed to create KongCACertificate",
		},
		{
			name: "nil response returns error",
			cert: &configurationv1alpha1.KongCACertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ca",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeInline),
					KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
						Cert: "ca-cert-data",
					},
				},
				Status: configurationv1alpha1.KongCACertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock: func(sdk *mocks.MockCACertificatesSDK) {
				sdk.EXPECT().CreateCaCertificate(mock.Anything, "cp-1", mock.Anything).Return(nil, nil)
			},
			wantErr: "failed creating KongCACertificate",
		},
		{
			name: "empty ID in response returns error",
			cert: &configurationv1alpha1.KongCACertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ca",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeInline),
					KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
						Cert: "ca-cert-data",
					},
				},
				Status: configurationv1alpha1.KongCACertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock: func(sdk *mocks.MockCACertificatesSDK) {
				sdk.EXPECT().CreateCaCertificate(mock.Anything, "cp-1", mock.Anything).Return(
					&sdkkonnectops.CreateCaCertificateResponse{
						CACertificate: &sdkkonnectcomp.CACertificate{
							ID: lo.ToPtr(""),
						},
					},
					nil,
				)
			},
			wantErr: "failed creating KongCACertificate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tt.clientObjs...).
				Build()

			sdk := mocks.NewMockCACertificatesSDK(t)
			tt.setupMock(sdk)

			err := createCACertificate(ctx, cl, sdk, tt.cert)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, tt.cert.GetKonnectStatus().GetKonnectID())
		})
	}
}

func TestUpdateCACertificate(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name       string
		cert       *configurationv1alpha1.KongCACertificate
		clientObjs []client.Object
		setupMock  func(*mocks.MockCACertificatesSDK)
		wantErr    string
	}{
		{
			name: "successful update with inline certificate",
			cert: &configurationv1alpha1.KongCACertificate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongCACertificate",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-ca",
					Namespace:  "default",
					Generation: 1,
					UID:        "test-uid",
				},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeInline),
					KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
						Cert: "ca-cert-data",
					},
				},
				Status: configurationv1alpha1.KongCACertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: "konnect-ca-id",
						},
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock: func(sdk *mocks.MockCACertificatesSDK) {
				sdk.EXPECT().UpsertCaCertificate(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpsertCaCertificateRequest) bool {
					return req.ControlPlaneID == "cp-1" &&
						req.CACertificateID == "konnect-ca-id" &&
						req.CACertificate.Cert == "ca-cert-data"
				})).Return(&sdkkonnectops.UpsertCaCertificateResponse{}, nil)
			},
		},
		{
			name: "successful update with secretRef",
			cert: &configurationv1alpha1.KongCACertificate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongCACertificate",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-ca",
					Namespace:  "default",
					Generation: 1,
					UID:        "test-uid",
				},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					Type:      lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
					SecretRef: &commonv1alpha1.NamespacedRef{Name: "ca-secret", Namespace: lo.ToPtr("default")},
				},
				Status: configurationv1alpha1.KongCACertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: "konnect-ca-id",
						},
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "ca-secret", Namespace: "default"},
					Data: map[string][]byte{
						"ca.crt": []byte("secret-ca-cert-data"),
					},
				},
			},
			setupMock: func(sdk *mocks.MockCACertificatesSDK) {
				sdk.EXPECT().UpsertCaCertificate(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpsertCaCertificateRequest) bool {
					return req.ControlPlaneID == "cp-1" &&
						req.CACertificateID == "konnect-ca-id" &&
						req.CACertificate.Cert == "secret-ca-cert-data"
				})).Return(&sdkkonnectops.UpsertCaCertificateResponse{}, nil)
			},
		},
		{
			name: "missing control plane ID returns error",
			cert: &configurationv1alpha1.KongCACertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ca",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeInline),
					KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
						Cert: "ca-cert-data",
					},
				},
				Status: configurationv1alpha1.KongCACertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{},
				},
			},
			clientObjs: []client.Object{},
			setupMock:  func(sdk *mocks.MockCACertificatesSDK) {},
			wantErr:    "can't update KongCACertificate",
		},
		{
			name: "failed to fetch secret returns error",
			cert: &configurationv1alpha1.KongCACertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ca",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					Type:      lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeSecretRef),
					SecretRef: &commonv1alpha1.NamespacedRef{Name: "missing-secret"},
				},
				Status: configurationv1alpha1.KongCACertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: "konnect-ca-id",
						},
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock:  func(sdk *mocks.MockCACertificatesSDK) {},
			wantErr:    "failed to fetch Secret",
		},
		{
			name: "SDK error returns wrapped error",
			cert: &configurationv1alpha1.KongCACertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ca",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCACertificateSourceTypeInline),
					KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
						Cert: "ca-cert-data",
					},
				},
				Status: configurationv1alpha1.KongCACertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: "konnect-ca-id",
						},
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock: func(sdk *mocks.MockCACertificatesSDK) {
				sdk.EXPECT().UpsertCaCertificate(mock.Anything, mock.Anything).Return(
					nil,
					&sdkkonnecterrs.SDKError{Message: "API error", StatusCode: 500},
				)
			},
			wantErr: "failed to update KongCACertificate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tt.clientObjs...).
				Build()

			sdk := mocks.NewMockCACertificatesSDK(t)
			tt.setupMock(sdk)

			err := updateCACertificate(ctx, cl, sdk, tt.cert)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
		})
	}
}
