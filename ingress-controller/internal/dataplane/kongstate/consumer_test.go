package kongstate

import (
	"testing"

	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/assert"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
)

func TestConsumer_SanitizedCopy(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   Consumer
		want Consumer
	}{
		{
			name: "sanitizes all credentials and copies all other fields",
			in: Consumer{
				Consumer: kong.Consumer{
					ID:        new("1"),
					CustomID:  new("2"),
					Username:  new("3"),
					CreatedAt: new(int64(4)),
					Tags:      []*string{new("5.1"), new("5.2")},
				},
				ConsumerGroups: []kong.ConsumerGroup{
					{ID: new("group-1")},
					{ID: new("group-2")},
				},
				Plugins: []kong.Plugin{{ID: new("1")}},
				KeyAuths: []*KeyAuth{
					{
						KeyAuth: kong.KeyAuth{ID: new("1"), Key: new("secret")},
					},
				},
				HMACAuths: []*HMACAuth{
					{
						HMACAuth: kong.HMACAuth{ID: new("1"), Secret: new("secret")},
					},
				},
				JWTAuths: []*JWTAuth{
					{
						JWTAuth: kong.JWTAuth{ID: new("1"), Secret: new("secret")},
					},
				},
				BasicAuths: []*BasicAuth{
					{
						BasicAuth: kong.BasicAuth{ID: new("1"), Password: new("secret")},
					},
				},
				ACLGroups: []*ACLGroup{
					{
						ACLGroup: kong.ACLGroup{ID: new("1")},
					},
				},
				Oauth2Creds: []*Oauth2Credential{
					{
						Oauth2Credential: kong.Oauth2Credential{ID: new("1"), ClientSecret: new("secret")},
					},
				},
				MTLSAuths: []*MTLSAuth{
					{
						MTLSAuth: kong.MTLSAuth{ID: new("1"), SubjectName: new("foo@example.com")},
					},
				},
				K8sKongConsumer: configurationv1.KongConsumer{Username: "foo"},
			},
			want: Consumer{
				Consumer: kong.Consumer{
					ID:        new("1"),
					CustomID:  new("2"),
					Username:  new("3"),
					CreatedAt: new(int64(4)),
					Tags:      []*string{new("5.1"), new("5.2")},
				},
				ConsumerGroups: []kong.ConsumerGroup{
					{ID: new("group-1")},
					{ID: new("group-2")},
				},
				Plugins: []kong.Plugin{{ID: new("1")}},
				KeyAuths: []*KeyAuth{
					{
						KeyAuth: kong.KeyAuth{ID: new("1"), Key: new("{vault://52fdfc07-2182-454f-963f-5f0f9a621d72}")},
					},
				},
				HMACAuths: []*HMACAuth{
					{
						HMACAuth: kong.HMACAuth{ID: new("1"), Secret: redactedString},
					},
				},
				JWTAuths: []*JWTAuth{
					{
						JWTAuth: kong.JWTAuth{ID: new("1"), Secret: redactedString},
					},
				},
				BasicAuths: []*BasicAuth{
					{
						BasicAuth: kong.BasicAuth{ID: new("1"), Password: redactedString},
					},
				},
				ACLGroups: []*ACLGroup{
					{
						ACLGroup: kong.ACLGroup{ID: new("1")},
					},
				},
				Oauth2Creds: []*Oauth2Credential{
					{
						Oauth2Credential: kong.Oauth2Credential{ID: new("1"), ClientSecret: redactedString},
					},
				},
				MTLSAuths: []*MTLSAuth{
					{
						MTLSAuth: kong.MTLSAuth{ID: new("1"), SubjectName: new("foo@example.com")},
					},
				},
				K8sKongConsumer: configurationv1.KongConsumer{Username: "foo"},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.SanitizedCopy(StaticUUIDGenerator{UUID: "52fdfc07-2182-454f-963f-5f0f9a621d72"})
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConsumer_SetCredential(t *testing.T) {
	username := "example"
	type args struct {
		credType   string
		consumer   *Consumer
		credConfig any
	}
	type Case struct {
		name    string
		args    args
		result  *Consumer
		wantErr bool
	}

	tests := []Case{
		{
			name: "invalid cred type errors",
			args: args{
				credType:   "invalid-type",
				consumer:   &Consumer{},
				credConfig: nil,
			},
			result:  &Consumer{},
			wantErr: true,
		},
		{
			name: "key-auth",
			args: args{
				credType:   "key-auth",
				consumer:   &Consumer{},
				credConfig: map[string]string{"key": "foo"},
			},
			result: &Consumer{
				KeyAuths: []*KeyAuth{
					{
						KeyAuth: kong.KeyAuth{
							Key:  new("foo"),
							Tags: []*string{},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "key-auth without key",
			args: args{
				credType:   "key-auth",
				consumer:   &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]string{},
			},
			result:  &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
			wantErr: true,
		},
		{
			name: "key-auth with invalid key type",
			args: args{
				credType:   "key-auth",
				consumer:   &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]any{"key": true},
			},
			result:  &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
			wantErr: true,
		},
		{
			name: "keyauth_credential",
			args: args{
				credType:   "keyauth_credential",
				consumer:   &Consumer{},
				credConfig: map[string]string{"key": "foo"},
			},
			result: &Consumer{
				KeyAuths: []*KeyAuth{
					{
						KeyAuth: kong.KeyAuth{
							Key:  new("foo"),
							Tags: []*string{},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "basic-auth",
			args: args{
				credType: "basic-auth",
				consumer: &Consumer{},
				credConfig: map[string]string{
					"username": "foo",
					"password": "bar",
				},
			},
			result: &Consumer{
				BasicAuths: []*BasicAuth{
					{
						BasicAuth: kong.BasicAuth{
							Username: new("foo"),
							Password: new("bar"),
							Tags:     []*string{},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "basic-auth without username",
			args: args{
				credType:   "basic-auth",
				consumer:   &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]string{},
			},
			result:  &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
			wantErr: true,
		},
		{
			name: "basic-auth with invalid username type",
			args: args{
				credType:   "basic-auth",
				consumer:   &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]any{"username": true},
			},
			result:  &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
			wantErr: true,
		},
		{
			name: "basicauth_credential",
			args: args{
				credType: "basicauth_credential",
				consumer: &Consumer{},
				credConfig: map[string]string{
					"username": "foo",
					"password": "bar",
				},
			},
			result: &Consumer{
				BasicAuths: []*BasicAuth{
					{
						BasicAuth: kong.BasicAuth{
							Username: new("foo"),
							Password: new("bar"),
							Tags:     []*string{},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "hmac-auth",
			args: args{
				credType: "hmac-auth",
				consumer: &Consumer{},
				credConfig: map[string]string{
					"username": "foo",
					"secret":   "bar",
				},
			},
			result: &Consumer{
				HMACAuths: []*HMACAuth{
					{
						HMACAuth: kong.HMACAuth{
							Username: new("foo"),
							Secret:   new("bar"),
							Tags:     []*string{},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "hmac-auth without username",
			args: args{
				credType:   "hmac-auth",
				consumer:   &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]string{},
			},
			result:  &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
			wantErr: true,
		},
		{
			name: "hmac-auth with invalid username type",
			args: args{
				credType:   "hmac-auth",
				consumer:   &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]any{"username": true},
			},
			result:  &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
			wantErr: true,
		},
		{
			name: "hmacauth_credential",
			args: args{
				credType: "hmacauth_credential",
				consumer: &Consumer{},
				credConfig: map[string]string{
					"username": "foo",
					"secret":   "bar",
				},
			},
			result: &Consumer{
				HMACAuths: []*HMACAuth{
					{
						HMACAuth: kong.HMACAuth{
							Username: new("foo"),
							Secret:   new("bar"),
							Tags:     []*string{},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "oauth2",
			args: args{
				credType: "oauth2",
				consumer: &Consumer{},
				credConfig: map[string]any{
					"name":          "foo",
					"client_id":     "bar",
					"client_secret": "baz",
					"redirect_uris": []string{"example.com"},
				},
			},
			result: &Consumer{
				Oauth2Creds: []*Oauth2Credential{
					{
						Oauth2Credential: kong.Oauth2Credential{
							Name:         new("foo"),
							ClientID:     new("bar"),
							ClientSecret: new("baz"),
							RedirectURIs: kong.StringSlice("example.com"),
							Tags:         []*string{},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "oauth2 without name",
			args: args{
				credType: "oauth2",
				consumer: &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]any{
					"client_id": "bar",
				},
			},
			result:  &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
			wantErr: true,
		},
		{
			name: "oauth2 without client_id",
			args: args{
				credType: "oauth2",
				consumer: &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]any{
					"name": "bar",
				},
			},
			result:  &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
			wantErr: true,
		},
		{
			name: "oauth2 with invalid client_id type",
			args: args{
				credType:   "oauth2",
				consumer:   &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]any{"client_id": true},
			},
			result:  &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
			wantErr: true,
		},
		{
			name: "jwt",
			args: args{
				credType: "jwt",
				consumer: &Consumer{},
				credConfig: map[string]string{
					"key":            "foo",
					"rsa_public_key": "bar",
					"secret":         "baz",
				},
			},
			result: &Consumer{
				JWTAuths: []*JWTAuth{
					{
						JWTAuth: kong.JWTAuth{
							Key:          new("foo"),
							RSAPublicKey: new("bar"),
							Secret:       new("baz"),
							// set by default
							Algorithm: new("HS256"),
							Tags:      []*string{},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "jwt without key",
			args: args{
				credType:   "jwt",
				consumer:   &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]string{},
			},
			result:  &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
			wantErr: true,
		},
		{
			name: "jwt with invalid key type",
			args: args{
				credType:   "jwt",
				consumer:   &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]any{"key": true},
			},
			result:  &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
			wantErr: true,
		},
		{
			name: "jwt_secret",
			args: args{
				credType: "jwt_secret",
				consumer: &Consumer{},
				credConfig: map[string]string{
					"key":            "foo",
					"rsa_public_key": "bar",
					"secret":         "baz",
				},
			},
			result: &Consumer{
				JWTAuths: []*JWTAuth{
					{
						JWTAuth: kong.JWTAuth{
							Key:          new("foo"),
							RSAPublicKey: new("bar"),
							Secret:       new("baz"),
							// set by default
							Algorithm: new("HS256"),
							Tags:      []*string{},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "acl",
			args: args{
				credType:   "acl",
				consumer:   &Consumer{},
				credConfig: map[string]string{"group": "group-foo"},
			},
			result: &Consumer{
				ACLGroups: []*ACLGroup{
					{
						ACLGroup: kong.ACLGroup{
							Group: new("group-foo"),
							Tags:  []*string{},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "acl without group",
			args: args{
				credType:   "acl",
				consumer:   &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]string{},
			},
			result:  &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
			wantErr: true,
		},
		{
			name: "acl with invalid group type",
			args: args{
				credType:   "acl",
				consumer:   &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]any{"group": true},
			},
			result:  &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.args.consumer.SetCredential(tt.args.credType, tt.args.credConfig, []*string{})
			if (err != nil) != tt.wantErr {
				t.Errorf("processCredential() error = %v, wantErr %v",
					err, tt.wantErr)
			}
			assert.Equal(t, tt.args.consumer, tt.result)
		})
	}

	mtlsSupportedTests := []Case{
		{
			name: "mtls-auth",
			args: args{
				credType:   "mtls-auth",
				consumer:   &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]string{"subject_name": "foo@example.com"},
			},
			result: &Consumer{
				Consumer: kong.Consumer{Username: &username, Tags: []*string{}},
				MTLSAuths: []*MTLSAuth{
					{
						MTLSAuth: kong.MTLSAuth{
							SubjectName: new("foo@example.com"),
							Tags:        []*string{},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "mtls-auth without subject_name",
			args: args{
				credType:   "mtls-auth",
				consumer:   &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]string{},
			},
			result:  &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
			wantErr: true,
		},
		{
			name: "mtls-auth with invalid subject_name type",
			args: args{
				credType:   "mtls-auth",
				consumer:   &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
				credConfig: map[string]any{"subject_name": true},
			},
			result:  &Consumer{Consumer: kong.Consumer{Username: &username, Tags: []*string{}}},
			wantErr: true,
		},
	}

	for _, tt := range mtlsSupportedTests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.args.consumer.SetCredential(tt.args.credType, tt.args.credConfig, []*string{})
			if (err != nil) != tt.wantErr {
				t.Errorf("processCredential() error = %v, wantErr %v",
					err, tt.wantErr)
			}
			assert.Equal(t, tt.args.consumer, tt.result)
		})
	}
}
