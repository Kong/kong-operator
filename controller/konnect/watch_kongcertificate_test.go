package konnect

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestEnqueueKongCertificateForSecret(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mysecret",
			Namespace: "ns",
		},
	}
	cert := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{Name: "cert1", Namespace: "ns"},
		Spec: configurationv1alpha1.KongCertificateSpec{
			SecretRef: &commonv1alpha1.NamespacedRef{Name: "mysecret", Namespace: new("ns")},
		},
	}

	s := scheme.Get()

	testCases := []struct {
		name               string
		client             client.Client
		input              client.Object
		wantLen            int
		wantNil            bool
		wantNamespacedName *client.ObjectKey
	}{
		{
			name: "returns correct requests for matching secret",
			client: fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(secret, cert).
				WithIndex(&configurationv1alpha1.KongCertificate{}, index.IndexFieldKongCertificateReferencesSecrets, index.SecretOnKongCertificate).
				Build(),
			input:              secret,
			wantLen:            1,
			wantNil:            false,
			wantNamespacedName: &client.ObjectKey{Namespace: "ns", Name: "cert1"},
		},
		{
			name: "returns nil for non-Secret object",
			client: fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(secret, cert).
				WithIndex(&configurationv1alpha1.KongCertificate{}, index.IndexFieldKongCertificateReferencesSecrets, index.SecretOnKongCertificate).
				Build(),
			input:   cert, // passing KongCertificate instead of Secret
			wantLen: 0,
			wantNil: true,
		},
		{
			name: "returns empty for no matching certificates",
			client: fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "othersecret",
						Namespace: "ns",
					},
				}, cert).
				WithIndex(&configurationv1alpha1.KongCertificate{}, index.IndexFieldKongCertificateReferencesSecrets, index.SecretOnKongCertificate).
				Build(),
			input: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "othersecret",
					Namespace: "ns",
				},
			},
			wantLen: 0,
			wantNil: false,
		},
		{
			name: "returns nil if List returns error",
			client: fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(secret, cert).
				WithIndex(&configurationv1alpha1.KongCertificate{}, index.IndexFieldKongCertificateReferencesSecrets, index.SecretOnKongCertificate).
				WithInterceptorFuncs(interceptor.Funcs{
					List: func(ctx context.Context, _ client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						return assert.AnError
					},
				}).
				Build(),
			input:   secret,
			wantLen: 0,
			wantNil: true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			fn := enqueueKongCertificateForSecret(tt.client)
			requests := fn(context.Background(), tt.input)
			if tt.wantNil {
				require.Nil(t, requests)
			} else {
				require.Len(t, requests, tt.wantLen)
				if tt.wantNamespacedName != nil && tt.wantLen > 0 {
					req := requests[0]
					require.Equal(t, tt.wantNamespacedName.Name, req.Name)
					require.Equal(t, tt.wantNamespacedName.Namespace, req.Namespace)
				}
			}
		})
	}
}
