package kongstate

import (
	"testing"

	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/assert"
)

func TestKeyAuth_SanitizedCopy(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   KeyAuth
		want KeyAuth
	}{
		{
			name: "fills all fields but Consumer and sanitizes key",
			in: KeyAuth{
				KeyAuth: kong.KeyAuth{
					Consumer:  &kong.Consumer{Username: new("foo")},
					CreatedAt: new(1),
					ID:        new("2"),
					Key:       new("3"),
					Tags:      []*string{new("4.1"), new("4.2")},
				},
			},
			want: KeyAuth{
				KeyAuth: kong.KeyAuth{
					CreatedAt: new(1),
					ID:        new("2"),
					Key:       new("{vault://52fdfc07-2182-454f-963f-5f0f9a621d72}"),
					Tags:      []*string{new("4.1"), new("4.2")},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := *tt.in.SanitizedCopy(StaticUUIDGenerator{UUID: "52fdfc07-2182-454f-963f-5f0f9a621d72"})
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHMACAuth_SanitizedCopy(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   HMACAuth
		want HMACAuth
	}{
		{
			name: "fills all fields but Consumer and sanitizes secret",
			in: HMACAuth{
				HMACAuth: kong.HMACAuth{
					Consumer:  &kong.Consumer{Username: new("foo")},
					CreatedAt: new(1),
					ID:        new("2"),
					Username:  new("3"),
					Secret:    new("4"),
					Tags:      []*string{new("5.1"), new("5.2")},
				},
			},
			want: HMACAuth{
				HMACAuth: kong.HMACAuth{
					CreatedAt: new(1),
					ID:        new("2"),
					Username:  new("3"),
					Secret:    redactedString,
					Tags:      []*string{new("5.1"), new("5.2")},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := *tt.in.SanitizedCopy()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestJWTAuth_SanitizedCopy(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   JWTAuth
		want JWTAuth
	}{
		{
			name: "fills all fields but Consumer and sanitizes secret",
			in: JWTAuth{
				JWTAuth: kong.JWTAuth{
					Consumer:     &kong.Consumer{Username: new("foo")},
					CreatedAt:    new(1),
					ID:           new("2"),
					Algorithm:    new("3"),
					Key:          new("4"),
					RSAPublicKey: new("5"),
					Secret:       new("6"),
					Tags:         []*string{new("7.1"), new("7.2")},
				},
			},
			want: JWTAuth{
				JWTAuth: kong.JWTAuth{
					CreatedAt:    new(1),
					ID:           new("2"),
					Algorithm:    new("3"),
					Key:          new("4"),
					RSAPublicKey: new("5"),
					Secret:       redactedString,
					Tags:         []*string{new("7.1"), new("7.2")},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := *tt.in.SanitizedCopy()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBasicAuth_SanitizedCopy(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   BasicAuth
		want BasicAuth
	}{
		{
			name: "fills all fields but Consumer and sanitizes password",
			in: BasicAuth{
				BasicAuth: kong.BasicAuth{
					Consumer:  &kong.Consumer{Username: new("foo")},
					CreatedAt: new(1),
					ID:        new("2"),
					Username:  new("3"),
					Password:  new("4"),
					Tags:      []*string{new("5.1"), new("5.2")},
				},
			},
			want: BasicAuth{
				BasicAuth: kong.BasicAuth{
					CreatedAt: new(1),
					ID:        new("2"),
					Username:  new("3"),
					Password:  redactedString,
					Tags:      []*string{new("5.1"), new("5.2")},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := *tt.in.SanitizedCopy()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOauth2Credential_SanitizedCopy(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   Oauth2Credential
		want Oauth2Credential
	}{
		{
			name: "fills all fields but Consumer and sanitizes client secret",
			in: Oauth2Credential{
				Oauth2Credential: kong.Oauth2Credential{
					Consumer:     &kong.Consumer{Username: new("foo")},
					CreatedAt:    new(1),
					ID:           new("2"),
					Name:         new("3"),
					ClientID:     new("4"),
					ClientSecret: new("5"),
					RedirectURIs: []*string{new("6.1"), new("6.2")},
					Tags:         []*string{new("7.1"), new("7.2")},
				},
			},
			want: Oauth2Credential{
				Oauth2Credential: kong.Oauth2Credential{
					CreatedAt:    new(1),
					ID:           new("2"),
					Name:         new("3"),
					ClientID:     new("4"),
					ClientSecret: redactedString,
					RedirectURIs: []*string{new("6.1"), new("6.2")},
					Tags:         []*string{new("7.1"), new("7.2")},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := *tt.in.SanitizedCopy()
			assert.Equal(t, tt.want, got)
		})
	}
}
