package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

// TestGetKonnectIDForUID_AIGatewayDataPlaneCertificateLegacyFallback covers
// the lookup of AI Gateway data plane certificates created before the operator
// labeled them: when no certificate carries the object's k8s-uid label, an
// unlabeled certificate with the same (Secret-resolved) certificate, title and
// description is matched, and nothing else.
func TestGetKonnectIDForUID_AIGatewayDataPlaneCertificateLegacyFallback(t *testing.T) {
	const (
		gatewayID = "gateway-1"
		certPEM   = "-----BEGIN CERTIFICATE-----\nowned\n-----END CERTIFICATE-----"
		otherPEM  = "-----BEGIN CERTIFICATE-----\nother\n-----END CERTIFICATE-----"
		title     = "dp-cert"
		uid       = "dp-cert-uid"
	)
	newObj := func() *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate {
		obj := &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
			Name:      "dp-cert",
			Namespace: "default",
			UID:       uid,
			Status: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateStatus{
				GatewayID: &aiconfigurationv1alpha1.KonnectEntityRef{ID: gatewayID},
			},
		}
		obj.Spec.APISpec.Title = title
		obj.Spec.APISpec.Cert = aiconfigurationv1alpha1.SensitiveDataSource{
			Type:      aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
			SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{Name: "tls", Key: "tls.crt"},
		}
		return obj
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(&corev1.Secret{
			Name:      "tls",
			Namespace: "default",
			Data:      map[string][]byte{"tls.crt": []byte(certPEM + "\n")},
		}).
		Build()
	cert := func(id, pem string, labels map[string]string) sdkkonnectcomp.AIGatewayDataPlaneClientCertificate {
		return sdkkonnectcomp.AIGatewayDataPlaneClientCertificate{ID: id, Title: title, Cert: pem, Labels: labels}
	}

	testCases := []struct {
		name string
		// The certificates Konnect lists. The generated lookup and the
		// fallback each list them once (the fallback only runs when the
		// generated lookup finds nothing).
		certs         []sdkkonnectcomp.AIGatewayDataPlaneClientCertificate
		fallbackRuns  bool
		expectedID    string
		expectedFound bool
	}{
		{
			name: "labeled certificate is found by its k8s-uid label, without the fallback",
			certs: []sdkkonnectcomp.AIGatewayDataPlaneClientCertificate{
				cert("unlabeled-same", certPEM, nil),
				cert("labeled", certPEM, map[string]string{KubernetesUIDLabelKey: uid}),
			},
			expectedID:    "labeled",
			expectedFound: true,
		},
		{
			name: "unlabeled certificate with the same content is found by the fallback",
			certs: []sdkkonnectcomp.AIGatewayDataPlaneClientCertificate{
				cert("unlabeled-other", otherPEM, nil),
				cert("unlabeled-same", certPEM, nil),
			},
			fallbackRuns:  true,
			expectedID:    "unlabeled-same",
			expectedFound: true,
		},
		{
			name: "unlabeled certificate with the same title but another certificate is not matched",
			certs: []sdkkonnectcomp.AIGatewayDataPlaneClientCertificate{
				cert("unlabeled-other", otherPEM, nil),
			},
			fallbackRuns: true,
		},
		{
			name: "certificate owned by another object (labeled) is not matched, even with the same content",
			certs: []sdkkonnectcomp.AIGatewayDataPlaneClientCertificate{
				cert("owned-by-other", certPEM, map[string]string{KubernetesUIDLabelKey: "other-uid"}),
			},
			fallbackRuns: true,
		},
	}

	// A CR that was never created in Konnect (e.g. its Secret is missing) has
	// no Konnect ID: deleting it looks the certificate up first. Not being
	// able to resolve the certificate must not block the deletion.
	t.Run("deletion is not blocked when the certificate Secret is missing", func(t *testing.T) {
		sdk := sdkmocks.NewMockSDKWrapperWithT(t)
		sdk.AIGatewayDataPlaneCertificatesSDK.EXPECT().
			ListAiGatewayDataPlaneCertificates(mock.Anything, sdkkonnectops.ListAiGatewayDataPlaneCertificatesRequest{GatewayID: gatewayID}).
			Return(&sdkkonnectops.ListAiGatewayDataPlaneCertificatesResponse{
				ListAIGatewayDataPlaneCertificatesResponse: &sdkkonnectcomp.ListAIGatewayDataPlaneCertificatesResponse{
					Data: []sdkkonnectcomp.AIGatewayDataPlaneClientCertificate{cert("unlabeled-other", otherPEM, nil)},
				},
			}, nil)

		emptyCl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		require.NoError(t, Delete(t.Context(), sdk, emptyCl, &metricsmocks.MockRecorder{}, newObj()))
	})

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk := sdkmocks.NewMockSDKWrapperWithT(t)
			listCalls := 1
			if tc.fallbackRuns {
				listCalls = 2
			}
			sdk.AIGatewayDataPlaneCertificatesSDK.EXPECT().
				ListAiGatewayDataPlaneCertificates(mock.Anything, sdkkonnectops.ListAiGatewayDataPlaneCertificatesRequest{GatewayID: gatewayID}).
				Return(&sdkkonnectops.ListAiGatewayDataPlaneCertificatesResponse{
					ListAIGatewayDataPlaneCertificatesResponse: &sdkkonnectcomp.ListAIGatewayDataPlaneCertificatesResponse{Data: tc.certs},
				}, nil).
				Times(listCalls)

			obj := newObj()
			id, err := getKonnectIDForUID(t.Context(), sdk, cl, obj)
			if tc.expectedFound {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedID, id)
				return
			}
			assert.Empty(t, id)
			var notFound EntityWithMatchingUIDNotFoundError
			require.ErrorAs(t, err, &notFound)
		})
	}
}
