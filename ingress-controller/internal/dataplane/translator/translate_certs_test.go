package translator

import (
	"sort"
	"testing"

	"github.com/go-logr/logr"
	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

func TestGetCertAlgorithm(t *testing.T) {
	rsaCert, _ := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("rsa.example.com"))
	ecdsaCert, _ := certificate.MustGenerateCertPEMFormat(
		certificate.WithCommonName("ecdsa.example.com"),
		certificate.WithKeyType(certificate.ECDSA),
	)

	algo, err := getCertAlgorithm(string(rsaCert))
	require.NoError(t, err)
	require.Equal(t, "RSA", algo.String())

	algo, err = getCertAlgorithm(string(ecdsaCert))
	require.NoError(t, err)
	require.Equal(t, "ECDSA", algo.String())

	_, err = getCertAlgorithm("not a pem block")
	require.Error(t, err)
}

func TestVerifyCertSANsMatch(t *testing.T) {
	rsaCert, _ := certificate.MustGenerateCertPEMFormat(
		certificate.WithCommonName("example.com"),
		certificate.WithDNSNames("example.com", "www.example.com"),
	)
	ecdsaCert, _ := certificate.MustGenerateCertPEMFormat(
		certificate.WithCommonName("example.com"),
		certificate.WithDNSNames("example.com", "www.example.com"),
		certificate.WithKeyType(certificate.ECDSA),
	)

	// same CN and SANs — should pass
	require.NoError(t, verifyCertSANsMatch(string(rsaCert), string(ecdsaCert)))

	// different CN — should fail
	other, _ := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("other.com"))
	require.Error(t, verifyCertSANsMatch(string(rsaCert), string(other)))

	// different SANs — should fail
	diffSAN, _ := certificate.MustGenerateCertPEMFormat(
		certificate.WithCommonName("example.com"),
		certificate.WithDNSNames("example.com"),
	)
	require.Error(t, verifyCertSANsMatch(string(rsaCert), string(diffSAN)))

	// same SANs in different order — should pass (order must not matter)
	ecdsaReversedSANs, _ := certificate.MustGenerateCertPEMFormat(
		certificate.WithCommonName("example.com"),
		certificate.WithDNSNames("www.example.com", "example.com"),
		certificate.WithKeyType(certificate.ECDSA),
	)
	require.NoError(t, verifyCertSANsMatch(string(rsaCert), string(ecdsaReversedSANs)))

	// both certs with no DNS SANs, only CN — should pass
	rsaNoDNS, _ := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("nodns.com"))
	ecdsaNoDNS, _ := certificate.MustGenerateCertPEMFormat(
		certificate.WithCommonName("nodns.com"),
		certificate.WithKeyType(certificate.ECDSA),
	)
	require.NoError(t, verifyCertSANsMatch(string(rsaNoDNS), string(ecdsaNoDNS)))
}

func TestMergeCerts(t *testing.T) {
	crt1, key1 := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("foo.com"))
	crt2, key2 := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("bar.com"))
	testCases := []struct {
		name         string
		certs        []certWrapper
		mergedCerts  []kongstate.Certificate
		idToMergedID certIDToMergedCertID
	}{
		{
			name: "single certificate",
			certs: []certWrapper{
				{
					identifier: string(crt1) + string(key1),
					cert: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
					},
					snis: []string{"foo.com"},
				},
			},
			mergedCerts: []kongstate.Certificate{
				{
					Certificate: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						SNIs: kong.StringSlice("foo.com"),
					},
				},
			},
			idToMergedID: certIDToMergedCertID{"certificate-1": "certificate-1"},
		},
		{
			name: "multiple different certificates",
			certs: []certWrapper{
				{
					identifier: string(crt1) + string(key1),
					cert: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
					},
					snis: []string{"foo.com"},
				},
				{
					identifier: string(crt2) + string(key2),
					cert: kong.Certificate{
						ID:   new("certificate-2"),
						Cert: new(string(crt2)),
						Key:  new(string(key2)),
					},
					snis: []string{"bar.com"},
				},
			},
			mergedCerts: []kongstate.Certificate{
				{
					Certificate: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						SNIs: kong.StringSlice("foo.com"),
					},
				},
				{
					Certificate: kong.Certificate{
						ID:   new("certificate-2"),
						Cert: new(string(crt2)),
						Key:  new(string(key2)),
						SNIs: kong.StringSlice("bar.com"),
					},
				},
			},
			idToMergedID: certIDToMergedCertID{
				"certificate-1": "certificate-1",
				"certificate-2": "certificate-2",
			},
		},
		{
			name: "multiple certs with same content should be merged",
			certs: []certWrapper{
				{
					identifier: string(crt1) + string(key1),
					cert: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
					},
					snis: []string{"foo.com"},
				},
				{
					identifier: string(crt1) + string(key1),
					cert: kong.Certificate{
						ID:   new("certificate-1-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
					},
					snis: []string{"baz.com"},
				},
			},
			mergedCerts: []kongstate.Certificate{
				{
					Certificate: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						// SNIs should be sorted
						SNIs: kong.StringSlice("baz.com", "foo.com"),
					},
				},
			},
			idToMergedID: certIDToMergedCertID{
				"certificate-1":   "certificate-1",
				"certificate-1-1": "certificate-1",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mergedCerts, idToMergedID := mergeCerts(logr.Discard(), tc.certs)
			// sort certs by their IDs to make a stable order of the result merged certs.
			sort.SliceStable(mergedCerts, func(i, j int) bool {
				return *mergedCerts[i].ID < *mergedCerts[j].ID
			})
			require.Equal(t, tc.mergedCerts, mergedCerts)
			require.Equal(t, tc.idToMergedID, idToMergedID)
		})
	}
}
