package configfetcher_test

import (
	"reflect"
	"testing"

	"github.com/kong/go-database-reconciler/pkg/utils"
	"github.com/kong/go-kong/kong"
	"github.com/kong/go-kong/kong/custom"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/configfetcher"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
)

func buildCustomEntityWithObject(entityType custom.Type, obj custom.Object) custom.Entity {
	e := custom.NewEntityObject(entityType)
	e.SetObject(obj)
	return e
}

func TestKongRawStateToKongState(t *testing.T) {
	// This is to gather all the fields in KongRawState that are tested in this suite.
	testedKongRawStateFields := sets.New[string]()

	for _, tt := range []struct {
		name              string
		kongRawState      *utils.KongRawState
		expectedKongState *kongstate.KongState
	}{
		{
			name: "sanitizes all services, routes, and upstreams and create a KongState out of a KongRawState",
			kongRawState: &utils.KongRawState{
				Services: []*kong.Service{
					{
						Name:      new("service"),
						ID:        new("service"),
						CreatedAt: new(100),
					},
				},
				Routes: []*kong.Route{
					{
						Name:      new("route"),
						ID:        new("route"),
						CreatedAt: new(101),
						Service: &kong.Service{
							ID: new("service"),
						},
					},
				},
				Upstreams: []*kong.Upstream{
					{
						Name: new("upstream"),
						ID:   new("upstream"),
					},
				},
				Targets: []*kong.Target{
					{
						ID:        new("target"),
						CreatedAt: kong.Float64(102),
						Weight:    new(999),
						Upstream: &kong.Upstream{
							ID: new("upstream"),
						},
					},
				},
				Vaults: []*kong.Vault{
					{
						Name: new("test-vault"), Prefix: new("test-vault"),
					},
				},
				Plugins: []*kong.Plugin{
					{
						Name: new("plugin1"),
						ID:   new("plugin1"),
						Service: &kong.Service{
							ID: new("service"),
						},
					},
					{
						Name: new("plugin2"),
						ID:   new("plugin2"),
						Route: &kong.Route{
							ID: new("route"),
						},
					},
				},
				Certificates: []*kong.Certificate{
					{
						ID:   new("certificate"),
						Cert: new("cert"),
					},
				},
				CACertificates: []*kong.CACertificate{
					{
						ID:   new("CACertificate"),
						Cert: new("cert"),
					},
				},
				Consumers: []*kong.Consumer{
					{
						ID:       new("consumer"),
						CustomID: new("customID"),
					},
				},
				ConsumerGroups: []*kong.ConsumerGroupObject{
					{
						ConsumerGroup: &kong.ConsumerGroup{
							ID:   new("consumerGroup"),
							Name: new("consumerGroup"),
						},
					},
				},
				KeyAuths: []*kong.KeyAuth{
					{
						ID:  new("keyAuth"),
						Key: new("key"),
						Consumer: &kong.Consumer{
							ID: new("consumer"),
						},
					},
				},
				HMACAuths: []*kong.HMACAuth{
					{
						ID: new("hmacAuth"),
						Consumer: &kong.Consumer{
							ID: new("consumer"),
						},
						Username: new("username"),
					},
				},
				JWTAuths: []*kong.JWTAuth{
					{
						ID: new("jwtAuth"),
						Consumer: &kong.Consumer{
							ID: new("consumer"),
						},
						Key: new("key"),
					},
				},
				BasicAuths: []*kong.BasicAuthOptions{
					{
						BasicAuth: kong.BasicAuth{
							ID: new("basicAuth"),
							Consumer: &kong.Consumer{
								ID: new("consumer"),
							},
							Username: new("username"),
						},
					},
				},
				ACLGroups: []*kong.ACLGroup{
					{
						ID: new("basicAuth"),
						Consumer: &kong.Consumer{
							ID: new("consumer"),
						},
						Group: new("group"),
					},
				},
				Oauth2Creds: []*kong.Oauth2Credential{
					{
						ID: new("basicAuth"),
						Consumer: &kong.Consumer{
							ID: new("consumer"),
						},
						Name: new("name"),
					},
				},
				MTLSAuths: []*kong.MTLSAuth{
					{
						ID: new("basicAuth"),
						Consumer: &kong.Consumer{
							ID: new("consumer"),
						},
						SubjectName: new("subjectName"),
					},
				},
				CustomEntities: []custom.Entity{
					buildCustomEntityWithObject("degraphql_routes", custom.Object{
						"id":    "degraphql-route-1",
						"uri":   "/graphql",
						"query": "query{name}",
						"service": map[string]any{
							"id": "service",
						},
					}),
				},
			},
			expectedKongState: &kongstate.KongState{
				Services: []kongstate.Service{
					{
						Service: kong.Service{
							Name: new("service"),
						},
						Plugins: []kong.Plugin{
							{
								Name: new("plugin1"),
							},
						},
						Routes: []kongstate.Route{
							{
								Route: kong.Route{
									Name: new("route"),
								},
								Plugins: []kong.Plugin{
									{
										Name: new("plugin2"),
									},
								},
							},
						},
					},
				},
				Upstreams: []kongstate.Upstream{
					{
						Upstream: kong.Upstream{
							Name: new("upstream"),
						},
						Targets: []kongstate.Target{
							{
								Target: kong.Target{
									Weight: new(999),
								},
							},
						},
					},
				},
				Vaults: []kongstate.Vault{
					{
						Vault: kong.Vault{
							Name: new("test-vault"), Prefix: new("test-vault"),
						},
					},
				},
				Certificates: []kongstate.Certificate{
					{
						Certificate: kong.Certificate{
							Cert: new("cert"),
						},
					},
				},
				CACertificates: []kong.CACertificate{
					{
						Cert: new("cert"),
					},
				},
				ConsumerGroups: []kongstate.ConsumerGroup{
					{
						ConsumerGroup: kong.ConsumerGroup{
							Name: new("consumerGroup"),
						},
					},
				},
				Consumers: []kongstate.Consumer{
					{
						Consumer: kong.Consumer{
							CustomID: new("customID"),
						},
						KeyAuths: []*kongstate.KeyAuth{
							{
								KeyAuth: kong.KeyAuth{
									Key: new("key"),
								},
							},
						},
						HMACAuths: []*kongstate.HMACAuth{
							{
								HMACAuth: kong.HMACAuth{
									Username: new("username"),
								},
							},
						},
						JWTAuths: []*kongstate.JWTAuth{
							{
								JWTAuth: kong.JWTAuth{
									Key: new("key"),
								},
							},
						},
						BasicAuths: []*kongstate.BasicAuth{
							{
								BasicAuth: kong.BasicAuth{
									Username: new("username"),
								},
							},
						},
						ACLGroups: []*kongstate.ACLGroup{
							{
								ACLGroup: kong.ACLGroup{
									Group: new("group"),
								},
							},
						},
						Oauth2Creds: []*kongstate.Oauth2Credential{
							{
								Oauth2Credential: kong.Oauth2Credential{
									Name: new("name"),
								},
							},
						},
						MTLSAuths: []*kongstate.MTLSAuth{
							{
								MTLSAuth: kong.MTLSAuth{
									SubjectName: new("subjectName"),
								},
							},
						},
					},
				},
				CustomEntities: map[string]*kongstate.KongCustomEntityCollection{
					"degraphql_routes": {
						Entities: []kongstate.CustomEntity{
							{
								Object: custom.Object{
									"id":    "degraphql-route-1",
									"uri":   "/graphql",
									"query": "query{name}",
									"service": map[string]any{
										"id": "service",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:         "doesn't panic when KongRawState is nil",
			kongRawState: nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt := tt

			// Collect all fields that are tested in this test case.
			if tt.kongRawState != nil {
				testedKongRawStateFields.Insert(extractNotEmptyFieldNames(*tt.expectedKongState)...)
			}

			var state *kongstate.KongState
			require.NotPanics(t, func() {
				state = configfetcher.KongRawStateToKongState(tt.kongRawState)
			})
			if tt.kongRawState != nil {
				require.Equal(t, tt.expectedKongState, state)
			}
		})
	}

	ensureAllKongStateFieldsAreTested(t, testedKongRawStateFields.UnsortedList())
}

// extractNotEmptyFieldNames returns the names of all non-empty fields in the given KongState.
// This is to programmatically find out what fields are used in a test case.
func extractNotEmptyFieldNames(s kongstate.KongState) []string {
	var fields []string
	typ := reflect.ValueOf(s).Type()
	for i := range typ.NumField() {
		f := typ.Field(i)
		v := reflect.ValueOf(s).Field(i)
		if !f.Anonymous && f.IsExported() && v.IsValid() && !v.IsZero() {
			fields = append(fields, f.Name)
		}
	}
	return fields
}

// ensureAllKongStateFieldsAreTested verifies that all fields in KongState are tested.
// It uses the testedFields slice to determine what fields were actually tested and compares
// it to the list of all fields in KongState, excluding fields that KIC doesn't support.
func ensureAllKongStateFieldsAreTested(t *testing.T, testedFields []string) {
	exempt := []string{
		// Plugins live under their attached objects and are not populated independently at the top level.
		"Plugins",
		// Licenses are injected from the license getter rather than extracted from the last state.
		"Licenses",
	}
	allKongStateFields := func() []string {
		var fields []string
		typ := reflect.ValueOf(kongstate.KongState{}).Type()
		for field := range typ.Fields() {
			name := field.Name
			if !lo.Contains(exempt, name) {
				fields = append(fields, name)
			}
		}
		return fields
	}()

	// Meta test - ensure we have testcases covering all fields in KongRawState.
	for _, field := range allKongStateFields {
		assert.True(t, lo.Contains(testedFields, field), "field %s not tested", field)
	}
}
