package translator

import (
	"sort"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

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
