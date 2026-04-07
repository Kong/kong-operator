package deckgen_test

import (
	"testing"

	"github.com/kong/go-database-reconciler/pkg/file"
	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/deckgen"
)

func TestGetFCertificateFromKongCert(t *testing.T) {
	testCases := []struct {
		name string
		cert kong.Certificate
		want file.FCertificate
	}{
		{
			name: "empty certificate",
			cert: kong.Certificate{},
			want: file.FCertificate{
				SNIs: []kong.SNI{},
			},
		},
		{
			name: "all fields set, SNIs have certificate ref",
			cert: kong.Certificate{
				ID:   new("cert-id"),
				Key:  new("cert-key"),
				Cert: new("cert-pem"),
				Tags: kong.StringSlice("k8s-name:secret1", "k8s-namespace:default"),
				SNIs: []*string{new("example.com")},
			},
			want: file.FCertificate{
				ID:   new("cert-id"),
				Key:  new("cert-key"),
				Cert: new("cert-pem"),
				Tags: kong.StringSlice("k8s-name:secret1", "k8s-namespace:default"),
				SNIs: []kong.SNI{
					{
						Name:        new("example.com"),
						Certificate: &kong.Certificate{ID: new("cert-id")},
					},
				},
			},
		},
		{
			name: "nil ID, SNIs have no certificate ref",
			cert: kong.Certificate{
				Key:  new("cert-key"),
				Cert: new("cert-pem"),
				SNIs: []*string{new("example.com")},
			},
			want: file.FCertificate{
				Key:  new("cert-key"),
				Cert: new("cert-pem"),
				SNIs: []kong.SNI{
					{Name: new("example.com")},
				},
			},
		},
		{
			name: "no SNIs",
			cert: kong.Certificate{
				ID:   new("cert-id"),
				Key:  new("cert-key"),
				Cert: new("cert-pem"),
			},
			want: file.FCertificate{
				ID:   new("cert-id"),
				Key:  new("cert-key"),
				Cert: new("cert-pem"),
				SNIs: []kong.SNI{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := deckgen.GetFCertificateFromKongCert(tc.cert)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestIsContentEmpty(t *testing.T) {
	testCases := []struct {
		name    string
		content *file.Content
		want    bool
	}{
		{
			name: "non-empty content",
			content: &file.Content{
				Upstreams: []file.FUpstream{
					{
						Upstream: kong.Upstream{
							Name: new("test"),
						},
					},
				},
			},
			want: false,
		},
		{
			name:    "empty content",
			content: &file.Content{},
			want:    true,
		},
		{
			name: "empty with version and info",
			content: &file.Content{
				FormatVersion: "1.1",
				Info: &file.Info{
					SelectorTags: []string{"tag1", "tag2"},
				},
			},
			want: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := deckgen.IsContentEmpty(tc.content)
			require.Equal(t, tc.want, got)
		})
	}
}
