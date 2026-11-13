package translator

import (
	"sort"
	"testing"
	"time"

	"github.com/go-logr/zapr"
	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

func TestMergeCerts(t *testing.T) {
	crt1, key1 := certificate.MustGenerateCertPEMFormat(certificate.WithKeyType(certificate.ECDSA), certificate.WithCommonName("foo.com"))
	crt2, key2 := certificate.MustGenerateCertPEMFormat(certificate.WithKeyType(certificate.ECDSA), certificate.WithCommonName("bar.com"))
	testCases := []struct {
		name         string
		certs        []certWrapper
		mergedCerts  []kongstate.Certificate
		idToMergedID certIDToMergedCertID
		// sniConflictLogs is the number of "Same SNI requested for multiple certs" errors expected.
		sniConflictLogs int
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
					ID:   new("certificate-1"),
					Cert: new(string(crt1)),
					Key:  new(string(key1)),
					SNIs: kong.StringSlice("foo.com"),
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
					ID:   new("certificate-1"),
					Cert: new(string(crt1)),
					Key:  new(string(key1)),
					SNIs: kong.StringSlice("foo.com"),
				},
				{
					ID:   new("certificate-2"),
					Cert: new(string(crt2)),
					Key:  new(string(key2)),
					SNIs: kong.StringSlice("bar.com"),
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
					ID:   new("certificate-1"),
					Cert: new(string(crt1)),
					Key:  new(string(key1)),
					// SNIs should be sorted
					SNIs: kong.StringSlice("baz.com", "foo.com"),
				},
			},
			idToMergedID: certIDToMergedCertID{
				"certificate-1":   "certificate-1",
				"certificate-1-1": "certificate-1",
			},
		},
		{
			// Regression test: the merged certificate's Tags must come from the same
			// winning Secret as its ID (earliest CreationTimestamp), not from whichever
			// certWrapper happened to be visited first while merging.
			name: "tags follow the earliest-created winner when the loser is listed first",
			certs: []certWrapper{
				{
					identifier:        string(crt1) + string(key1),
					CreationTimestamp: metav1.NewTime(time.Unix(200, 0)),
					cert: kong.Certificate{
						ID:   new("certificate-2"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("tag-2"),
					},
					snis: []string{"foo.com"},
				},
				{
					identifier:        string(crt1) + string(key1),
					CreationTimestamp: metav1.NewTime(time.Unix(100, 0)),
					cert: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("tag-1"),
					},
					snis: []string{"baz.com"},
				},
			},
			mergedCerts: []kongstate.Certificate{
				{
					ID:   new("certificate-1"),
					Cert: new(string(crt1)),
					Key:  new(string(key1)),
					Tags: kong.StringSlice("tag-1"),
					SNIs: kong.StringSlice("baz.com", "foo.com"),
				},
			},
			idToMergedID: certIDToMergedCertID{
				"certificate-1": "certificate-1",
				"certificate-2": "certificate-1",
			},
		},
		{
			name: "tags follow the earliest-created winner when the winner is listed first",
			certs: []certWrapper{
				{
					identifier:        string(crt1) + string(key1),
					CreationTimestamp: metav1.NewTime(time.Unix(100, 0)),
					cert: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("tag-1"),
					},
					snis: []string{"baz.com"},
				},
				{
					identifier:        string(crt1) + string(key1),
					CreationTimestamp: metav1.NewTime(time.Unix(200, 0)),
					cert: kong.Certificate{
						ID:   new("certificate-2"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("tag-2"),
					},
					snis: []string{"foo.com"},
				},
			},
			mergedCerts: []kongstate.Certificate{
				{
					ID:   new("certificate-1"),
					Cert: new(string(crt1)),
					Key:  new(string(key1)),
					Tags: kong.StringSlice("tag-1"),
					SNIs: kong.StringSlice("baz.com", "foo.com"),
				},
			},
			idToMergedID: certIDToMergedCertID{
				"certificate-1": "certificate-1",
				"certificate-2": "certificate-1",
			},
		},
		{
			name: "tags follow the lowest ID when creation timestamps tie",
			certs: []certWrapper{
				{
					identifier:        string(crt1) + string(key1),
					CreationTimestamp: metav1.NewTime(time.Unix(100, 0)),
					cert: kong.Certificate{
						ID:   new("certificate-2"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("tag-2"),
					},
					snis: []string{"foo.com"},
				},
				{
					identifier:        string(crt1) + string(key1),
					CreationTimestamp: metav1.NewTime(time.Unix(100, 0)),
					cert: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("tag-1"),
					},
					snis: []string{"baz.com"},
				},
			},
			mergedCerts: []kongstate.Certificate{
				{
					ID:   new("certificate-1"),
					Cert: new(string(crt1)),
					Key:  new(string(key1)),
					Tags: kong.StringSlice("tag-1"),
					SNIs: kong.StringSlice("baz.com", "foo.com"),
				},
			},
			idToMergedID: certIDToMergedCertID{
				"certificate-1": "certificate-1",
				"certificate-2": "certificate-1",
			},
		},
		{
			name: "tags follow the earliest-created winner across multiple duplicates with winner in the middle",
			certs: []certWrapper{
				{
					identifier:        string(crt1) + string(key1),
					CreationTimestamp: metav1.NewTime(time.Unix(200, 0)),
					cert: kong.Certificate{
						ID:   new("certificate-2"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("tag-2"),
					},
					snis: []string{"foo.com"},
				},
				{
					identifier:        string(crt1) + string(key1),
					CreationTimestamp: metav1.NewTime(time.Unix(100, 0)),
					cert: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("tag-1"),
					},
					snis: []string{"baz.com"},
				},
				{
					identifier:        string(crt1) + string(key1),
					CreationTimestamp: metav1.NewTime(time.Unix(300, 0)),
					cert: kong.Certificate{
						ID:   new("certificate-3"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("tag-3"),
					},
					snis: []string{"qux.com"},
				},
			},
			mergedCerts: []kongstate.Certificate{
				{
					ID:   new("certificate-1"),
					Cert: new(string(crt1)),
					Key:  new(string(key1)),
					Tags: kong.StringSlice("tag-1"),
					SNIs: kong.StringSlice("baz.com", "foo.com", "qux.com"),
				},
			},
			idToMergedID: certIDToMergedCertID{
				"certificate-1": "certificate-1",
				"certificate-2": "certificate-1",
				"certificate-3": "certificate-1",
			},
		},
		{
			name: "same certificate requested by an Ingress and Gateway listeners inherits the Gateways' tags",
			certs: []certWrapper{
				{
					identifier: string(crt1) + string(key1),
					cert: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("secret-1-tag"),
					},
					snis: []string{"ingress.com"},
				},
				{
					identifier: string(crt1) + string(key1),
					cert: kong.Certificate{
						ID:   new("certificate-2"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("secret-2-tag"),
					},
					snis:          []string{"a.com"},
					inheritedTags: util.InheritedTags{Gateway: []string{"gw-a"}, GatewayClass: []string{"gwc"}},
				},
				{
					identifier: string(crt1) + string(key1),
					cert: kong.Certificate{
						ID:   new("certificate-3"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("secret-3-tag"),
					},
					// a.com is also requested by gw-a's listener on the same certificate, so it inherits both.
					snis:          []string{"a.com", "b.com"},
					inheritedTags: util.InheritedTags{Gateway: []string{"gw-b"}, GatewayClass: []string{"gwc"}},
				},
			},
			mergedCerts: []kongstate.Certificate{
				{
					ID:   new("certificate-1"),
					Cert: new(string(crt1)),
					Key:  new(string(key1)),
					Tags: kong.StringSlice("secret-1-tag", "gw-a", "gw-b", "gwc"),
					SNIs: kong.StringSlice("a.com", "b.com", "ingress.com"),
					SNITags: map[string][]*string{
						"a.com": kong.StringSlice("gw-a", "gw-b", "gwc"),
						"b.com": kong.StringSlice("gw-b", "gwc"),
					},
				},
			},
			idToMergedID: certIDToMergedCertID{
				"certificate-1": "certificate-1",
				"certificate-2": "certificate-1",
				"certificate-3": "certificate-1",
			},
		},
		{
			name: "SNI requested by an Ingress and a Gateway listener on the same certificate inherits the Gateway's tags",
			certs: []certWrapper{
				{
					identifier: string(crt1) + string(key1),
					cert: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("ingress-secret-tag"),
					},
					snis: []string{"example.com"},
				},
				{
					identifier: string(crt1) + string(key1),
					cert: kong.Certificate{
						ID:   new("certificate-2"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("gateway-secret-tag"),
					},
					snis:          []string{"example.com"},
					inheritedTags: util.InheritedTags{Gateway: []string{"gw"}, GatewayClass: []string{"gwc"}},
				},
			},
			mergedCerts: []kongstate.Certificate{
				{
					ID:      new("certificate-1"),
					Cert:    new(string(crt1)),
					Key:     new(string(key1)),
					Tags:    kong.StringSlice("ingress-secret-tag", "gw", "gwc"),
					SNIs:    kong.StringSlice("example.com"),
					SNITags: map[string][]*string{"example.com": kong.StringSlice("gw", "gwc")},
				},
			},
			idToMergedID: certIDToMergedCertID{
				"certificate-1": "certificate-1",
				"certificate-2": "certificate-1",
			},
		},
		{
			// The merged certificate switches to the Gateway Secret's ID, which must neither drop the SNI's
			// inherited tags nor be reported as an SNI conflict.
			name: "SNI shared by an Ingress and a Gateway listener keeps the Gateway's tags when the Gateway Secret wins the ID",
			certs: []certWrapper{
				{
					identifier:        string(crt1) + string(key1),
					CreationTimestamp: metav1.NewTime(time.Unix(200, 0)),
					cert: kong.Certificate{
						ID:   new("certificate-2"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("ingress-secret-tag"),
					},
					snis: []string{"example.com"},
				},
				{
					identifier:        string(crt1) + string(key1),
					CreationTimestamp: metav1.NewTime(time.Unix(100, 0)),
					cert: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("gateway-secret-tag"),
					},
					snis:          []string{"example.com"},
					inheritedTags: util.InheritedTags{Gateway: []string{"gw"}, GatewayClass: []string{"gwc"}},
				},
			},
			mergedCerts: []kongstate.Certificate{
				{
					ID:      new("certificate-1"),
					Cert:    new(string(crt1)),
					Key:     new(string(key1)),
					Tags:    kong.StringSlice("gateway-secret-tag", "gw", "gwc"),
					SNIs:    kong.StringSlice("example.com"),
					SNITags: map[string][]*string{"example.com": kong.StringSlice("gw", "gwc")},
				},
			},
			idToMergedID: certIDToMergedCertID{
				"certificate-1": "certificate-1",
				"certificate-2": "certificate-1",
			},
		},
		{
			name: "SNI requested by two Gateway listeners on the same certificate inherits the tags of both",
			certs: []certWrapper{
				{
					identifier: string(crt1) + string(key1),
					cert: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
					},
					snis:          []string{"example.com"},
					inheritedTags: util.InheritedTags{Gateway: []string{"gw-a"}, GatewayClass: []string{"gwc"}},
				},
				{
					identifier: string(crt1) + string(key1),
					cert: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
					},
					snis:          []string{"example.com"},
					inheritedTags: util.InheritedTags{Gateway: []string{"gw-b"}, GatewayClass: []string{"gwc"}},
				},
			},
			mergedCerts: []kongstate.Certificate{
				{
					ID:      new("certificate-1"),
					Cert:    new(string(crt1)),
					Key:     new(string(key1)),
					Tags:    kong.StringSlice("gw-a", "gw-b", "gwc"),
					SNIs:    kong.StringSlice("example.com"),
					SNITags: map[string][]*string{"example.com": kong.StringSlice("gw-a", "gw-b", "gwc")},
				},
			},
			idToMergedID: certIDToMergedCertID{
				"certificate-1": "certificate-1",
			},
		},
		{
			// The Ingress' certificate serves the hostname, so the SNI does not inherit the tags of a Gateway
			// whose certificate is not the one served for it.
			name: "SNI requested by an Ingress and a Gateway listener with different certificates stays with the Ingress' certificate",
			certs: []certWrapper{
				{
					identifier: string(crt1) + string(key1),
					cert: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
						Tags: kong.StringSlice("ingress-secret-tag"),
					},
					snis: []string{"example.com"},
				},
				{
					identifier: string(crt2) + string(key2),
					cert: kong.Certificate{
						ID:   new("certificate-2"),
						Cert: new(string(crt2)),
						Key:  new(string(key2)),
						Tags: kong.StringSlice("gateway-secret-tag"),
					},
					snis:          []string{"example.com"},
					inheritedTags: util.InheritedTags{Gateway: []string{"gw"}, GatewayClass: []string{"gwc"}},
				},
			},
			mergedCerts: []kongstate.Certificate{
				{
					ID:   new("certificate-1"),
					Cert: new(string(crt1)),
					Key:  new(string(key1)),
					Tags: kong.StringSlice("ingress-secret-tag"),
					SNIs: kong.StringSlice("example.com"),
				},
				{
					ID:   new("certificate-2"),
					Cert: new(string(crt2)),
					Key:  new(string(key2)),
					Tags: kong.StringSlice("gateway-secret-tag", "gw", "gwc"),
				},
			},
			idToMergedID: certIDToMergedCertID{
				"certificate-1": "certificate-1",
				"certificate-2": "certificate-2",
			},
			sniConflictLogs: 1,
		},
		{
			// The tags of several Gateways come before the tags of their GatewayClasses, regardless of
			// which GatewayClass each Gateway belongs to.
			name: "certificate requested by Gateways of different GatewayClasses inherits all Gateways' tags first",
			certs: []certWrapper{
				{
					identifier: string(crt1) + string(key1),
					cert: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
					},
					snis:          []string{"example.com"},
					inheritedTags: util.InheritedTags{Gateway: []string{"gw-1"}, GatewayClass: []string{"gwc-a"}},
				},
				{
					identifier: string(crt1) + string(key1),
					cert: kong.Certificate{
						ID:   new("certificate-1"),
						Cert: new(string(crt1)),
						Key:  new(string(key1)),
					},
					snis:          []string{"example.com"},
					inheritedTags: util.InheritedTags{Gateway: []string{"gw-2"}, GatewayClass: []string{"gwc-b"}},
				},
			},
			mergedCerts: []kongstate.Certificate{
				{
					ID:      new("certificate-1"),
					Cert:    new(string(crt1)),
					Key:     new(string(key1)),
					Tags:    kong.StringSlice("gw-1", "gw-2", "gwc-a", "gwc-b"),
					SNIs:    kong.StringSlice("example.com"),
					SNITags: map[string][]*string{"example.com": kong.StringSlice("gw-1", "gw-2", "gwc-a", "gwc-b")},
				},
			},
			idToMergedID: certIDToMergedCertID{
				"certificate-1": "certificate-1",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zapcore.ErrorLevel)
			mergedCerts, idToMergedID := mergeCerts(zapr.NewLogger(zap.New(core)), tc.certs)
			// sort certs by their IDs to make a stable order of the result merged certs.
			sort.SliceStable(mergedCerts, func(i, j int) bool {
				return *mergedCerts[i].ID < *mergedCerts[j].ID
			})
			require.Equal(t, tc.mergedCerts, mergedCerts)
			require.Equal(t, tc.idToMergedID, idToMergedID)
			require.Equal(t, tc.sniConflictLogs, logs.FilterMessageSnippet("Same SNI requested for multiple certs").Len())
		})
	}
}
