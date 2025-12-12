package ops

import (
	"context"
	"fmt"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/pkg/metadata"
	sdkmocks "github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func TestKongCertificateToCertificateInput(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = configurationv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	ctx := context.Background()

	tests := []struct {
		name        string
		cert        *configurationv1alpha1.KongCertificate
		clientObjs  []client.Object
		wantErr     string
		wantCert    string
		wantKey     string
		wantCertAlt string
		wantKeyAlt  string
		wantTags    []string
	}{
		{
			name: "valid secret with all keys including alt",
			cert: &configurationv1alpha1.KongCertificate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongCertificate",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cert1",
					Namespace:  "ns",
					Generation: 1,
					UID:        "uid-1",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type:         lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
					SecretRef:    &commonv1alpha1.NamespacedRef{Name: "mysecret", Namespace: lo.ToPtr("ns")},
					SecretRefAlt: &commonv1alpha1.NamespacedRef{Name: "mysecret-alt", Namespace: lo.ToPtr("ns")},
				},
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "mysecret", Namespace: "ns"},
					Data: map[string][]byte{
						"tls.crt": []byte("certdata"),
						"tls.key": []byte("keydata"),
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "mysecret-alt", Namespace: "ns"},
					Data: map[string][]byte{
						"tls.crt": []byte("altcert"),
						"tls.key": []byte("altkey"),
					},
				},
			},
			wantCert:    "certdata",
			wantKey:     "keydata",
			wantCertAlt: "altcert",
			wantKeyAlt:  "altkey",
			wantTags: []string{
				"k8s-generation:1",
				"k8s-kind:KongCertificate",
				"k8s-name:cert1",
				"k8s-uid:uid-1",
				"k8s-version:v1alpha1",
				"k8s-group:configuration.konghq.com",
				"k8s-namespace:ns",
			},
		},
		{
			name: "valid secret without alt keys",
			cert: &configurationv1alpha1.KongCertificate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongCertificate",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cert2",
					Namespace:  "ns",
					Generation: 2,
					UID:        "uid-2",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type:      lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
					SecretRef: &commonv1alpha1.NamespacedRef{Name: "mysecret2", Namespace: lo.ToPtr("ns")},
				},
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "mysecret2", Namespace: "ns"},
					Data: map[string][]byte{
						"tls.crt": []byte("certdata2"),
						"tls.key": []byte("keydata2"),
					},
				},
			},
			wantCert: "certdata2",
			wantKey:  "keydata2",
			wantTags: []string{
				"k8s-generation:2",
				"k8s-kind:KongCertificate",
				"k8s-name:cert2",
				"k8s-uid:uid-2",
				"k8s-version:v1alpha1",
				"k8s-group:configuration.konghq.com",
				"k8s-namespace:ns",
			},
		},
		{
			name: "inline type uses Spec fields directly with custom tags",
			cert: &configurationv1alpha1.KongCertificate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongCertificate",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cert-direct",
					Namespace:  "ns",
					Generation: 3,
					UID:        "uid-3",
					Annotations: map[string]string{
						metadata.AnnotationKeyTags: "tag1,tag2,duplicate",
					},
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert:    "direct-cert",
						Key:     "direct-key",
						CertAlt: "direct-cert-alt",
						KeyAlt:  "direct-key-alt",
						Tags:    []string{"tag3", "tag4", "duplicate"},
					},
				},
			},
			clientObjs:  []client.Object{},
			wantCert:    "direct-cert",
			wantKey:     "direct-key",
			wantCertAlt: "direct-cert-alt",
			wantKeyAlt:  "direct-key-alt",
			wantTags: []string{
				"k8s-generation:3",
				"k8s-kind:KongCertificate",
				"k8s-name:cert-direct",
				"k8s-uid:uid-3",
				"k8s-version:v1alpha1",
				"k8s-group:configuration.konghq.com",
				"k8s-namespace:ns",
				"tag1",
				"tag2",
				"tag3",
				"tag4",
				"duplicate",
			},
		},
		{
			name: "missing SecretRef returns error",
			cert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{Name: "cert3", Namespace: "ns"},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type:      lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
					SecretRef: nil,
				},
			},
			clientObjs: []client.Object{},
			wantErr:    "secretRef is nil",
		},
		{
			name: "missing Secret returns error",
			cert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{Name: "cert4", Namespace: "ns"},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type:      lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
					SecretRef: &commonv1alpha1.NamespacedRef{Name: "notfound", Namespace: lo.ToPtr("ns")},
				},
			},
			clientObjs: []client.Object{},
			wantErr:    "failed to fetch Secret ns/notfound",
		},
		{
			name: "missing tls.crt key returns error",
			cert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{Name: "cert5", Namespace: "ns"},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type:      lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
					SecretRef: &commonv1alpha1.NamespacedRef{Name: "badsecret", Namespace: lo.ToPtr("ns")},
				},
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "badsecret", Namespace: "ns"},
					Data:       map[string][]byte{"tls.key": []byte("keydata")},
				},
			},
			wantErr: "missing key 'tls.crt'",
		},
		{
			name: "missing tls.key key returns error",
			cert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{Name: "cert6", Namespace: "ns"},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type:      lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
					SecretRef: &commonv1alpha1.NamespacedRef{Name: "badsecret2", Namespace: lo.ToPtr("ns")},
				},
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "badsecret2", Namespace: "ns"},
					Data:       map[string][]byte{"tls.crt": []byte("certdata")},
				},
			},
			wantErr: "missing key 'tls.key'",
		},
		{
			name: "missing alt Secret returns error",
			cert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{Name: "cert7", Namespace: "ns"},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type:         lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
					SecretRef:    &commonv1alpha1.NamespacedRef{Name: "mysecret", Namespace: lo.ToPtr("ns")},
					SecretRefAlt: &commonv1alpha1.NamespacedRef{Name: "notfound-alt", Namespace: lo.ToPtr("ns")},
				},
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "mysecret", Namespace: "ns"},
					Data: map[string][]byte{
						"tls.crt": []byte("cert"),
						"tls.key": []byte("key"),
					},
				},
			},
			wantErr: "failed to fetch Secret ns/notfound-alt",
		},
		{
			name: "missing tls.crt in alt Secret returns error",
			cert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{Name: "cert8", Namespace: "ns"},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type:         lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
					SecretRef:    &commonv1alpha1.NamespacedRef{Name: "mysecret", Namespace: lo.ToPtr("ns")},
					SecretRefAlt: &commonv1alpha1.NamespacedRef{Name: "bad-alt", Namespace: lo.ToPtr("ns")},
				},
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "mysecret", Namespace: "ns"},
					Data: map[string][]byte{
						"tls.crt": []byte("cert"),
						"tls.key": []byte("key"),
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "bad-alt", Namespace: "ns"},
					Data: map[string][]byte{
						"tls.key": []byte("key"),
					},
				},
			},
			wantErr: "missing key 'tls.crt'",
		},
		{
			name: "missing tls.key in alt Secret returns error",
			cert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{Name: "cert9", Namespace: "ns"},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type:         lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
					SecretRef:    &commonv1alpha1.NamespacedRef{Name: "mysecret", Namespace: lo.ToPtr("ns")},
					SecretRefAlt: &commonv1alpha1.NamespacedRef{Name: "bad-alt2", Namespace: lo.ToPtr("ns")},
				},
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "mysecret", Namespace: "ns"},
					Data: map[string][]byte{
						"tls.crt": []byte("cert"),
						"tls.key": []byte("key"),
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "bad-alt2", Namespace: "ns"},
					Data: map[string][]byte{
						"tls.crt": []byte("cert"),
					},
				},
			},
			wantErr: "missing key 'tls.key'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.clientObjs...).Build()
			input, err := kongCertificateToCertificateInput(ctx, cl, tt.cert)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantCert, input.Cert)
			require.Equal(t, tt.wantKey, input.Key)

			if tt.wantCertAlt != "" {
				require.NotNil(t, input.CertAlt)
				require.Equal(t, tt.wantCertAlt, *input.CertAlt)
			} else if input.CertAlt != nil {
				require.Empty(t, *input.CertAlt)
			}

			if tt.wantKeyAlt != "" {
				require.NotNil(t, input.KeyAlt)
				require.Equal(t, tt.wantKeyAlt, *input.KeyAlt)
			} else if input.KeyAlt != nil {
				require.Empty(t, *input.KeyAlt)
			}

			// Verify tags are generated.
			require.NotEmpty(t, input.Tags)
			if len(tt.wantTags) > 0 {
				require.ElementsMatch(t, tt.wantTags, input.Tags)
			}
		})
	}
}

func TestFetchTLSDataFromSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	ctx := context.Background()

	tests := []struct {
		name            string
		parentNamespace string
		secretRef       *commonv1alpha1.NamespacedRef
		clientObjs      []client.Object
		wantErr         string
		wantCert        string
		wantKey         string
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
				Name:      "tls-secret",
				Namespace: nil,
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "parent-ns",
					},
					Data: map[string][]byte{
						"tls.crt": []byte("parent-ns-cert"),
						"tls.key": []byte("parent-ns-key"),
					},
				},
			},
			wantCert: "parent-ns-cert",
			wantKey:  "parent-ns-key",
		},
		{
			name:            "uses parent namespace when secretRef.Namespace is empty string",
			parentNamespace: "parent-ns",
			secretRef: &commonv1alpha1.NamespacedRef{
				Name:      "tls-secret",
				Namespace: lo.ToPtr(""),
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "parent-ns",
					},
					Data: map[string][]byte{
						"tls.crt": []byte("parent-ns-cert"),
						"tls.key": []byte("parent-ns-key"),
					},
				},
			},
			wantCert: "parent-ns-cert",
			wantKey:  "parent-ns-key",
		},
		{
			name:            "valid secret with tls.crt and tls.key",
			parentNamespace: "default",
			secretRef: &commonv1alpha1.NamespacedRef{
				Name:      "tls-secret",
				Namespace: lo.ToPtr("default"),
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"tls.crt": []byte("cert-content"),
						"tls.key": []byte("key-content"),
					},
				},
			},
			wantCert: "cert-content",
			wantKey:  "key-content",
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
			name:            "secret missing tls.crt key",
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
						"tls.key": []byte("key-content"),
					},
				},
			},
			wantErr: "secret default/incomplete-secret is missing key 'tls.crt'",
		},
		{
			name:            "secret missing tls.key key",
			parentNamespace: "default",
			secretRef: &commonv1alpha1.NamespacedRef{
				Name:      "incomplete-secret2",
				Namespace: lo.ToPtr("default"),
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "incomplete-secret2",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"tls.crt": []byte("cert-content"),
					},
				},
			},
			wantErr: "secret default/incomplete-secret2 is missing key 'tls.key'",
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
			wantErr: "secret default/empty-secret is missing key 'tls.crt'",
		},
		{
			name:            "secret with empty values",
			parentNamespace: "default",
			secretRef: &commonv1alpha1.NamespacedRef{
				Name:      "empty-values",
				Namespace: lo.ToPtr("test-ns"),
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "empty-values",
						Namespace: "test-ns",
					},
					Data: map[string][]byte{
						"tls.crt": []byte(""),
						"tls.key": []byte(""),
					},
				},
			},
			wantCert: "",
			wantKey:  "",
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
						"tls.crt": []byte("cross-ns-cert"),
						"tls.key": []byte("cross-ns-key"),
					},
				},
			},
			wantCert: "cross-ns-cert",
			wantKey:  "cross-ns-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.clientObjs...).Build()
			certData, keyData, err := fetchTLSDataFromSecret(ctx, cl, tt.parentNamespace, tt.secretRef)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantCert, certData)
			require.Equal(t, tt.wantKey, keyData)
		})
	}
}

func TestUpdateCertificate(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = configurationv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	ctx := context.Background()

	tests := []struct {
		name       string
		cert       *configurationv1alpha1.KongCertificate
		clientObjs []client.Object
		setupMock  func(*sdkmocks.MockCertificatesSDK)
		wantErr    string
	}{
		{
			name: "successful update with inline certificate",
			cert: &configurationv1alpha1.KongCertificate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongCertificate",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cert",
					Namespace:  "default",
					Generation: 1,
					UID:        "test-uid",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "cert-data",
						Key:  "key-data",
					},
				},
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: "konnect-cert-id",
						},
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock: func(sdk *sdkmocks.MockCertificatesSDK) {
				sdk.EXPECT().UpsertCertificate(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpsertCertificateRequest) bool {
					return req.ControlPlaneID == "cp-1" &&
						req.CertificateID == "konnect-cert-id" &&
						req.Certificate.Cert == "cert-data" &&
						req.Certificate.Key == "key-data"
				})).Return(&sdkkonnectops.UpsertCertificateResponse{}, nil)
			},
		},
		{
			name: "successful update with secretRef",
			cert: &configurationv1alpha1.KongCertificate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongCertificate",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cert",
					Namespace:  "default",
					Generation: 1,
					UID:        "test-uid",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type:      lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
					SecretRef: &commonv1alpha1.NamespacedRef{Name: "tls-secret", Namespace: lo.ToPtr("default")},
				},
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: "konnect-cert-id",
						},
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "tls-secret", Namespace: "default"},
					Data: map[string][]byte{
						"tls.crt": []byte("secret-cert-data"),
						"tls.key": []byte("secret-key-data"),
					},
				},
			},
			setupMock: func(sdk *sdkmocks.MockCertificatesSDK) {
				sdk.EXPECT().UpsertCertificate(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpsertCertificateRequest) bool {
					return req.ControlPlaneID == "cp-1" &&
						req.CertificateID == "konnect-cert-id" &&
						req.Certificate.Cert == "secret-cert-data" &&
						req.Certificate.Key == "secret-key-data"
				})).Return(&sdkkonnectops.UpsertCertificateResponse{}, nil)
			},
		},
		{
			name: "missing control plane ID returns error",
			cert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "cert-data",
						Key:  "key-data",
					},
				},
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{},
				},
			},
			clientObjs: []client.Object{},
			setupMock:  func(sdk *sdkmocks.MockCertificatesSDK) {},
			wantErr:    "can't update KongCertificate",
		},
		{
			name: "failed to fetch secret returns error",
			cert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type:      lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
					SecretRef: &commonv1alpha1.NamespacedRef{Name: "missing-secret", Namespace: lo.ToPtr("default")},
				},
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: "konnect-cert-id",
						},
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock:  func(sdk *sdkmocks.MockCertificatesSDK) {},
			wantErr:    "failed to fetch Secret default/missing-secret",
		},
		{
			name: "SDK upsert error is wrapped",
			cert: &configurationv1alpha1.KongCertificate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongCertificate",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cert",
					Namespace:  "default",
					Generation: 1,
					UID:        "test-uid",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "cert-data",
						Key:  "key-data",
					},
				},
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: "konnect-cert-id",
						},
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock: func(sdk *sdkmocks.MockCertificatesSDK) {
				sdk.EXPECT().UpsertCertificate(mock.Anything, mock.Anything).Return(
					nil,
					fmt.Errorf("konnect API error"),
				)
			},
			wantErr: "failed to update KongCertificate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.clientObjs...).Build()
			sdk := sdkmocks.NewMockCertificatesSDK(t)
			tt.setupMock(sdk)

			err := updateCertificate(ctx, cl, sdk, tt.cert)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestCreateCertificate(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = configurationv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	ctx := context.Background()

	tests := []struct {
		name       string
		cert       *configurationv1alpha1.KongCertificate
		clientObjs []client.Object
		setupMock  func(*sdkmocks.MockCertificatesSDK)
		wantErr    string
		wantID     string
	}{
		{
			name: "successful create with inline certificate",
			cert: &configurationv1alpha1.KongCertificate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongCertificate",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cert",
					Namespace:  "default",
					Generation: 1,
					UID:        "test-uid",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "cert-data",
						Key:  "key-data",
					},
				},
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock: func(sdk *sdkmocks.MockCertificatesSDK) {
				sdk.EXPECT().ListCertificate(mock.Anything, mock.Anything).Return(
					&sdkkonnectops.ListCertificateResponse{
						Object: &sdkkonnectops.ListCertificateResponseBody{
							Data: []sdkkonnectcomp.Certificate{},
						},
					},
					nil,
				)
				sdk.EXPECT().CreateCertificate(mock.Anything, "cp-1", mock.MatchedBy(func(cert sdkkonnectcomp.Certificate) bool {
					return cert.Cert == "cert-data" && cert.Key == "key-data"
				})).Return(&sdkkonnectops.CreateCertificateResponse{
					Certificate: &sdkkonnectcomp.Certificate{
						ID: lo.ToPtr("new-cert-id"),
					},
				}, nil)
			},
			wantID: "new-cert-id",
		},
		{
			name: "successful create with secretRef",
			cert: &configurationv1alpha1.KongCertificate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongCertificate",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cert",
					Namespace:  "default",
					Generation: 1,
					UID:        "test-uid",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type:      lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
					SecretRef: &commonv1alpha1.NamespacedRef{Name: "tls-secret", Namespace: lo.ToPtr("default")},
				},
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "tls-secret", Namespace: "default"},
					Data: map[string][]byte{
						"tls.crt": []byte("secret-cert-data"),
						"tls.key": []byte("secret-key-data"),
					},
				},
			},
			setupMock: func(sdk *sdkmocks.MockCertificatesSDK) {
				sdk.EXPECT().ListCertificate(mock.Anything, mock.Anything).Return(
					&sdkkonnectops.ListCertificateResponse{
						Object: &sdkkonnectops.ListCertificateResponseBody{
							Data: []sdkkonnectcomp.Certificate{},
						},
					},
					nil,
				)
				sdk.EXPECT().CreateCertificate(mock.Anything, "cp-1", mock.MatchedBy(func(cert sdkkonnectcomp.Certificate) bool {
					return cert.Cert == "secret-cert-data" && cert.Key == "secret-key-data"
				})).Return(&sdkkonnectops.CreateCertificateResponse{
					Certificate: &sdkkonnectcomp.Certificate{
						ID: lo.ToPtr("new-cert-id-from-secret"),
					},
				}, nil)
			},
			wantID: "new-cert-id-from-secret",
		},
		{
			name: "certificate already exists returns existing ID",
			cert: &configurationv1alpha1.KongCertificate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongCertificate",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cert",
					Namespace:  "default",
					Generation: 1,
					UID:        "test-uid",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "cert-data",
						Key:  "key-data",
					},
				},
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock: func(sdk *sdkmocks.MockCertificatesSDK) {
				sdk.EXPECT().ListCertificate(mock.Anything, mock.Anything).Return(
					&sdkkonnectops.ListCertificateResponse{
						Object: &sdkkonnectops.ListCertificateResponseBody{
							Data: []sdkkonnectcomp.Certificate{
								{
									ID: lo.ToPtr("existing-cert-id"),
								},
							},
						},
					},
					nil,
				)
			},
			wantID: "existing-cert-id",
		},
		{
			name: "missing control plane ID returns error",
			cert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "cert-data",
						Key:  "key-data",
					},
				},
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{},
				},
			},
			clientObjs: []client.Object{},
			setupMock:  func(sdk *sdkmocks.MockCertificatesSDK) {},
			wantErr:    "can't create KongCertificate",
		},
		{
			name: "list error is propagated",
			cert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "cert-data",
						Key:  "key-data",
					},
				},
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock: func(sdk *sdkmocks.MockCertificatesSDK) {
				sdk.EXPECT().ListCertificate(mock.Anything, mock.Anything).Return(
					nil,
					fmt.Errorf("konnect list error"),
				)
			},
			wantErr: "failed to list",
		},
		{
			name: "existing cert without ID returns error",
			cert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "cert-data",
						Key:  "key-data",
					},
				},
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock: func(sdk *sdkmocks.MockCertificatesSDK) {
				sdk.EXPECT().ListCertificate(mock.Anything, mock.Anything).Return(
					&sdkkonnectops.ListCertificateResponse{
						Object: &sdkkonnectops.ListCertificateResponseBody{
							Data: []sdkkonnectcomp.Certificate{
								{
									ID: nil,
								},
							},
						},
					},
					nil,
				)
			},
			wantErr: "found a cert without ID",
		},
		{
			name: "failed to fetch secret returns error",
			cert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type:      lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeSecretRef),
					SecretRef: &commonv1alpha1.NamespacedRef{Name: "missing-secret", Namespace: lo.ToPtr("default")},
				},
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock: func(sdk *sdkmocks.MockCertificatesSDK) {
				sdk.EXPECT().ListCertificate(mock.Anything, mock.Anything).Return(
					&sdkkonnectops.ListCertificateResponse{
						Object: &sdkkonnectops.ListCertificateResponseBody{
							Data: []sdkkonnectcomp.Certificate{},
						},
					},
					nil,
				)
			},
			wantErr: "failed to fetch Secret default/missing-secret",
		},
		{
			name: "SDK create error is wrapped",
			cert: &configurationv1alpha1.KongCertificate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongCertificate",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cert",
					Namespace:  "default",
					Generation: 1,
					UID:        "test-uid",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "cert-data",
						Key:  "key-data",
					},
				},
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock: func(sdk *sdkmocks.MockCertificatesSDK) {
				sdk.EXPECT().ListCertificate(mock.Anything, mock.Anything).Return(
					&sdkkonnectops.ListCertificateResponse{
						Object: &sdkkonnectops.ListCertificateResponseBody{
							Data: []sdkkonnectcomp.Certificate{},
						},
					},
					nil,
				)
				sdk.EXPECT().CreateCertificate(mock.Anything, mock.Anything, mock.Anything).Return(
					nil,
					fmt.Errorf("konnect API error"),
				)
			},
			wantErr: "failed to create KongCertificate",
		},
		{
			name: "nil response returns error",
			cert: &configurationv1alpha1.KongCertificate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongCertificate",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cert",
					Namespace:  "default",
					Generation: 1,
					UID:        "test-uid",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type: lo.ToPtr(configurationv1alpha1.KongCertificateSourceTypeInline),
					KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
						Cert: "cert-data",
						Key:  "key-data",
					},
				},
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						ControlPlaneID: "cp-1",
					},
				},
			},
			clientObjs: []client.Object{},
			setupMock: func(sdk *sdkmocks.MockCertificatesSDK) {
				sdk.EXPECT().ListCertificate(mock.Anything, mock.Anything).Return(
					&sdkkonnectops.ListCertificateResponse{
						Object: &sdkkonnectops.ListCertificateResponseBody{
							Data: []sdkkonnectcomp.Certificate{},
						},
					},
					nil,
				)
				sdk.EXPECT().CreateCertificate(mock.Anything, mock.Anything, mock.Anything).Return(
					&sdkkonnectops.CreateCertificateResponse{
						Certificate: nil,
					},
					nil,
				)
			},
			wantErr: "failed creating",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.clientObjs...).Build()
			sdk := sdkmocks.NewMockCertificatesSDK(t)
			tt.setupMock(sdk)

			err := createCertificate(ctx, cl, sdk, tt.cert)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantID, tt.cert.GetKonnectID())
		})
	}
}

func TestAdoptKongCertificateOverride(t *testing.T) {
	ctx := context.Background()
	sdk := sdkmocks.NewMockCertificatesSDK(t)
	sdk.EXPECT().GetCertificate(mock.Anything, "konnect-cert-id", "cp-1").Return(
		&sdkkonnectops.GetCertificateResponse{
			Certificate: &sdkkonnectcomp.Certificate{
				Cert: "cert-data",
				Key:  "key-data",
			},
		},
		nil,
	)
	sdk.EXPECT().UpsertCertificate(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpsertCertificateRequest) bool {
		return req.ControlPlaneID == "cp-1" &&
			req.CertificateID == "konnect-cert-id" &&
			req.Certificate.Cert == "cert-data" &&
			req.Certificate.Key == "key-data"
	})).Return(&sdkkonnectops.UpsertCertificateResponse{}, nil)

	cert := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cert",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-cert-id",
				},
			},
			KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
				Cert: "cert-data",
				Key:  "key-data",
			},
		},
		Status: configurationv1alpha1.KongCertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	scheme := runtime.NewScheme()
	_ = configurationv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	err := adoptCertificate(ctx, cl, sdk, cert)
	require.NoError(t, err)
	assert.Equal(t, "konnect-cert-id", cert.GetKonnectID())
}

func TestAdoptKongCertificateMatchSuccess(t *testing.T) {
	ctx := context.Background()
	certAlt := "alt-cert"
	keyAlt := "alt-key"
	sdk := sdkmocks.NewMockCertificatesSDK(t)
	sdk.EXPECT().GetCertificate(mock.Anything, "konnect-cert-id", "cp-1").Return(
		&sdkkonnectops.GetCertificateResponse{
			Certificate: &sdkkonnectcomp.Certificate{
				Cert:    "cert-data",
				Key:     "key-data",
				CertAlt: &certAlt,
				KeyAlt:  &keyAlt,
			},
		},
		nil,
	)

	cert := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cert",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-cert-id",
				},
			},
			KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
				Cert:    "cert-data",
				Key:     "key-data",
				CertAlt: certAlt,
				KeyAlt:  keyAlt,
			},
		},
		Status: configurationv1alpha1.KongCertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	scheme := runtime.NewScheme()
	_ = configurationv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	err := adoptCertificate(ctx, cl, sdk, cert)
	require.NoError(t, err)
	assert.Equal(t, "konnect-cert-id", cert.GetKonnectID())
}

func TestAdoptKongCertificateMatchMismatch(t *testing.T) {
	ctx := context.Background()
	sdk := sdkmocks.NewMockCertificatesSDK(t)
	sdk.EXPECT().GetCertificate(mock.Anything, "konnect-cert-id", "cp-1").Return(
		&sdkkonnectops.GetCertificateResponse{
			Certificate: &sdkkonnectcomp.Certificate{
				Cert: "other-cert",
				Key:  "key-data",
			},
		},
		nil,
	)

	cert := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cert",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-cert-id",
				},
			},
			KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
				Cert: "cert-data",
				Key:  "key-data",
			},
		},
		Status: configurationv1alpha1.KongCertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	scheme := runtime.NewScheme()
	_ = configurationv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	err := adoptCertificate(ctx, cl, sdk, cert)
	require.Error(t, err)
	var notMatchErr KonnectEntityAdoptionNotMatchError
	assert.ErrorAs(t, err, &notMatchErr)
	assert.Empty(t, cert.GetKonnectID())
}

func TestAdoptKongCertificateUIDConflict(t *testing.T) {
	ctx := context.Background()
	sdk := sdkmocks.NewMockCertificatesSDK(t)
	sdk.EXPECT().GetCertificate(mock.Anything, "konnect-cert-id", "cp-1").Return(
		&sdkkonnectops.GetCertificateResponse{
			Certificate: &sdkkonnectcomp.Certificate{
				Cert: "cert-data",
				Key:  "key-data",
				Tags: []string{"k8s-uid:other-uid"},
			},
		},
		nil,
	)

	cert := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cert",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-cert-id",
				},
			},
			KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
				Cert: "cert-data",
				Key:  "key-data",
			},
		},
		Status: configurationv1alpha1.KongCertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	scheme := runtime.NewScheme()
	_ = configurationv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	err := adoptCertificate(ctx, cl, sdk, cert)
	require.Error(t, err)
	var uidConflict KonnectEntityAdoptionUIDTagConflictError
	assert.ErrorAs(t, err, &uidConflict)
	assert.Empty(t, cert.GetKonnectID())
}

func TestAdoptKongCertificateFetchFailure(t *testing.T) {
	ctx := context.Background()
	sdk := sdkmocks.NewMockCertificatesSDK(t)
	sdk.EXPECT().GetCertificate(mock.Anything, "konnect-cert-id", "cp-1").Return(
		(*sdkkonnectops.GetCertificateResponse)(nil),
		&sdkkonnecterrs.NotFoundError{},
	)

	cert := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cert",
			Namespace: "default",
			UID:       "uid-1",
		},
		Spec: configurationv1alpha1.KongCertificateSpec{
			Adopt: &commonv1alpha1.AdoptOptions{
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: "konnect-cert-id",
				},
			},
			KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
				Cert: "cert-data",
				Key:  "key-data",
			},
		},
		Status: configurationv1alpha1.KongCertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-1",
			},
		},
	}

	scheme := runtime.NewScheme()
	_ = configurationv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	err := adoptCertificate(ctx, cl, sdk, cert)
	require.Error(t, err)
	var fetchErr KonnectEntityAdoptionFetchError
	assert.ErrorAs(t, err, &fetchErr)
	assert.Empty(t, cert.GetKonnectID())
}
