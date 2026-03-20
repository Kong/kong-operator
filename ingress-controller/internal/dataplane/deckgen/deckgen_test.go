package deckgen_test

import (
	"testing"

	"github.com/kong/go-database-reconciler/pkg/file"
	"github.com/kong/go-kong/kong"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/deckgen"
)

func TestGetFCertificateFromKongCert(t *testing.T) {
	testCases := []struct {
		name     string
		inmemory bool
		cert     kong.Certificate
		want     file.FCertificate
	}{
		{
			name:     "empty certificate",
			inmemory: false,
			cert:     kong.Certificate{},
			want: file.FCertificate{
				SNIs: []kong.SNI{},
			},
		},
		{
			name:     "all fields set, inmemory=true, SNIs have no certificate ref",
			inmemory: true,
			cert: kong.Certificate{
				ID:   lo.ToPtr("cert-id"),
				Key:  lo.ToPtr("cert-key"),
				Cert: lo.ToPtr("cert-pem"),
				SNIs: []*string{lo.ToPtr("example.com"), lo.ToPtr("other.com")},
			},
			want: file.FCertificate{
				ID:   lo.ToPtr("cert-id"),
				Key:  lo.ToPtr("cert-key"),
				Cert: lo.ToPtr("cert-pem"),
				SNIs: []kong.SNI{
					{Name: lo.ToPtr("example.com")},
					{Name: lo.ToPtr("other.com")},
				},
			},
		},
		{
			name:     "all fields set, inmemory=false, SNIs have certificate ref",
			inmemory: false,
			cert: kong.Certificate{
				ID:   lo.ToPtr("cert-id"),
				Key:  lo.ToPtr("cert-key"),
				Cert: lo.ToPtr("cert-pem"),
				SNIs: []*string{lo.ToPtr("example.com")},
			},
			want: file.FCertificate{
				ID:   lo.ToPtr("cert-id"),
				Key:  lo.ToPtr("cert-key"),
				Cert: lo.ToPtr("cert-pem"),
				SNIs: []kong.SNI{
					{
						Name:        lo.ToPtr("example.com"),
						Certificate: &kong.Certificate{ID: lo.ToPtr("cert-id")},
					},
				},
			},
		},
		{
			name:     "nil ID, inmemory=false, SNIs have no certificate ref",
			inmemory: false,
			cert: kong.Certificate{
				Key:  lo.ToPtr("cert-key"),
				Cert: lo.ToPtr("cert-pem"),
				SNIs: []*string{lo.ToPtr("example.com")},
			},
			want: file.FCertificate{
				Key:  lo.ToPtr("cert-key"),
				Cert: lo.ToPtr("cert-pem"),
				SNIs: []kong.SNI{
					{Name: lo.ToPtr("example.com")},
				},
			},
		},
		{
			name:     "no SNIs",
			inmemory: false,
			cert: kong.Certificate{
				ID:   lo.ToPtr("cert-id"),
				Key:  lo.ToPtr("cert-key"),
				Cert: lo.ToPtr("cert-pem"),
			},
			want: file.FCertificate{
				ID:   lo.ToPtr("cert-id"),
				Key:  lo.ToPtr("cert-key"),
				Cert: lo.ToPtr("cert-pem"),
				SNIs: []kong.SNI{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := deckgen.GetFCertificateFromKongCert(tc.inmemory, tc.cert)
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
							Name: kong.String("test"),
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
