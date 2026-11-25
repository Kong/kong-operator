package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEntitySyncHashesPerType pins the canonical payload and representation
// SHA-256 golden hashes for every guarded Kong Gateway entity type (17 entity
// types, including both JSON and expression Route variants).
//
// Because hashing canonicalizes the serialized sdk-konnect-go request and
// response structs, an SDK upgrade or canonicalization change that alters JSON
// encoding will fail this test instead of silently causing a PUT storm or
// invalidating persisted status.sync records.
//
// If this test fails after an intentional change, consult
// docs/internal/konnect-sync-hashing.md, decide whether SyncHashVersion must be
// bumped, and update the golden digests accordingly.
func TestEntitySyncHashesPerType(t *testing.T) {
	const (
		controlPlaneID = "11111111-1111-1111-1111-111111111111"
		entityID       = "22222222-2222-2222-2222-222222222222"
		parentID       = "33333333-3333-3333-3333-333333333333"
		caCertID1      = "44444444-4444-4444-4444-444444444444"
		caCertID2      = "55555555-5555-5555-5555-555555555555"
	)

	tags := []string{"k8s-name:example", "k8s-namespace:default", "managed-by:kong-operator"}

	testCases := []struct {
		name                string
		payload             func(marker string) any
		repr                func(marker string) any
		expectedPayloadHash string
		expectedReprHash    string
	}{
		{
			name: "service",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertServiceRequest{
					ControlPlaneID: controlPlaneID,
					ServiceID:      entityID,
					ServiceRequest: sdkkonnectcomp.CreateServiceRequestServiceWithHost(
						sdkkonnectcomp.ServiceWithHost{
							Name:              new("svc-" + marker),
							Host:              "example.com",
							Port:              new(int64(443)),
							Protocol:          sdkkonnectcomp.ServiceRequestProtocolHTTPS.ToPointer(),
							Path:              new("/api"),
							ConnectTimeout:    new(int64(60000)),
							ReadTimeout:       new(int64(60000)),
							WriteTimeout:      new(int64(60000)),
							Retries:           new(int64(5)),
							Enabled:           new(true),
							TLSVerify:         new(true),
							TLSVerifyDepth:    new(int64(2)),
							CaCertificates:    []string{caCertID2, caCertID1},
							ClientCertificate: &sdkkonnectcomp.ServiceRequestClientCertificate{ID: new(parentID)},
							Tags:              tags,
						},
					),
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.Service{
					ID:                new(entityID),
					Name:              new("svc-" + marker),
					Host:              "example.com",
					Port:              new(int64(443)),
					Protocol:          sdkkonnectcomp.ServiceProtocolHTTPS.ToPointer(),
					Path:              new("/api"),
					ConnectTimeout:    new(int64(60000)),
					ReadTimeout:       new(int64(60000)),
					WriteTimeout:      new(int64(60000)),
					Retries:           new(int64(5)),
					Enabled:           new(true),
					TLSVerify:         new(true),
					TLSVerifyDepth:    new(int64(2)),
					CaCertificates:    []string{caCertID1, caCertID2},
					ClientCertificate: &sdkkonnectcomp.ClientCertificate{ID: new(parentID)},
					Tags:              tags,
					CreatedAt:         new(int64(1700000000)),
					UpdatedAt:         new(int64(1700000100)),
				}
			},
			expectedPayloadHash: "sha256:15fcf0c168d5fdd76f7707a1e40efcf738bf4970267b32cda931a0d1de748b14",
			expectedReprHash:    "sha256:01f23fec2ddbaeaf1f4de1bbfdb6c9b9aed115c1fb21fd2c70f38794350cd35a",
		},
		{
			name: "route-json",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertRouteRequest{
					ControlPlaneID: controlPlaneID,
					RouteID:        entityID,
					Route: sdkkonnectcomp.CreateRouteRouteJSON(
						sdkkonnectcomp.RouteJSON{
							Name:                    new("route-" + marker),
							Hosts:                   []string{"api.example.com", "www.example.com"},
							Paths:                   []string{"/v1", "/v2"},
							Methods:                 []string{"GET", "POST"},
							Protocols:               []sdkkonnectcomp.RouteJSONProtocols{sdkkonnectcomp.RouteJSONProtocolsHTTPS, sdkkonnectcomp.RouteJSONProtocolsHTTP},
							Headers:                 map[string][]string{"X-Env": {"prod", "canary"}, "X-Region": {"us"}},
							Snis:                    []string{"api.example.com"},
							StripPath:               new(true),
							PreserveHost:            new(false),
							RegexPriority:           new(int64(10)),
							HTTPSRedirectStatusCode: sdkkonnectcomp.HTTPSRedirectStatusCodeFourHundredAndTwentySix.ToPointer(),
							PathHandling:            sdkkonnectcomp.PathHandlingV0.ToPointer(),
							RequestBuffering:        new(true),
							ResponseBuffering:       new(true),
							Service:                 &sdkkonnectcomp.RouteJSONService{ID: new(parentID)},
							Tags:                    tags,
						},
					),
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.Route{
					Type: sdkkonnectcomp.RouteUnionTypeRouteJSON,
					RouteJSON: &sdkkonnectcomp.RouteJSON{
						ID:                      new(entityID),
						Name:                    new("route-" + marker),
						Hosts:                   []string{"api.example.com", "www.example.com"},
						Paths:                   []string{"/v1", "/v2"},
						Methods:                 []string{"GET", "POST"},
						Protocols:               []sdkkonnectcomp.RouteJSONProtocols{sdkkonnectcomp.RouteJSONProtocolsHTTP, sdkkonnectcomp.RouteJSONProtocolsHTTPS},
						Headers:                 map[string][]string{"X-Region": {"us"}, "X-Env": {"prod", "canary"}},
						Snis:                    []string{"api.example.com"},
						StripPath:               new(true),
						PreserveHost:            new(false),
						RegexPriority:           new(int64(10)),
						HTTPSRedirectStatusCode: sdkkonnectcomp.HTTPSRedirectStatusCodeFourHundredAndTwentySix.ToPointer(),
						PathHandling:            sdkkonnectcomp.PathHandlingV0.ToPointer(),
						RequestBuffering:        new(true),
						ResponseBuffering:       new(true),
						Service:                 &sdkkonnectcomp.RouteJSONService{ID: new(parentID)},
						Tags:                    tags,
						CreatedAt:               new(int64(1700000000)),
						UpdatedAt:               new(int64(1700000100)),
					},
				}
			},
			expectedPayloadHash: "sha256:38b2b5c8b17f194b362081eca9dde046af9af97ab7e48f4d1302c40dbfec2bb8",
			expectedReprHash:    "sha256:a9c5a07fd789bbe786575b0701488316711cac035ecdadcb8ec6b6af768b9bcb",
		},
		{
			name: "route-expression",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertRouteRequest{
					ControlPlaneID: controlPlaneID,
					RouteID:        entityID,
					Route: sdkkonnectcomp.CreateRouteRouteExpression(
						sdkkonnectcomp.RouteExpression{
							Name:       new("expr-route-" + marker),
							Expression: new(`http.path ^= "/api"`),
							Priority:   new(int64(100)),
							Protocols:  []sdkkonnectcomp.RouteExpressionProtocols{sdkkonnectcomp.RouteExpressionProtocolsHTTPS, sdkkonnectcomp.RouteExpressionProtocolsHTTP},
							Service:    &sdkkonnectcomp.RouteExpressionService{ID: new(parentID)},
							Tags:       tags,
						},
					),
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.Route{
					Type: sdkkonnectcomp.RouteUnionTypeRouteExpression,
					RouteExpression: &sdkkonnectcomp.RouteExpression{
						ID:         new(entityID),
						Name:       new("expr-route-" + marker),
						Expression: new(`http.path ^= "/api"`),
						Priority:   new(int64(100)),
						Protocols:  []sdkkonnectcomp.RouteExpressionProtocols{sdkkonnectcomp.RouteExpressionProtocolsHTTP, sdkkonnectcomp.RouteExpressionProtocolsHTTPS},
						Service:    &sdkkonnectcomp.RouteExpressionService{ID: new(parentID)},
						Tags:       tags,
						CreatedAt:  new(int64(1700000000)),
						UpdatedAt:  new(int64(1700000100)),
					},
				}
			},
			expectedPayloadHash: "sha256:9959b1d6ac55cf71eaff49e74b0943ce43c3cdb97ca86b92a69cc87268c3e888",
			expectedReprHash:    "sha256:3e8f084b75060c40b4d3a51eeb1e168515edb93794df91571c5292fe34d5d38f",
		},
		{
			name: "plugin",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertPluginRequest{
					ControlPlaneID: controlPlaneID,
					PluginID:       entityID,
					Plugin: sdkkonnectcomp.Plugin{
						Name:         "request-transformer",
						InstanceName: new("plugin-" + marker),
						Enabled:      new(true),
						Protocols:    []sdkkonnectcomp.Protocols{sdkkonnectcomp.ProtocolsHTTPS, sdkkonnectcomp.ProtocolsHTTP},
						Service:      &sdkkonnectcomp.PluginService{ID: new(parentID)},
						Ordering: &sdkkonnectcomp.Ordering{
							After: &sdkkonnectcomp.After{Access: []string{"rate-limiting"}},
						},
						Config: map[string]any{
							"add": map[string]any{
								"headers": []string{"x-z:1", "x-a:2"},
							},
							"replace": map[string]any{
								"uri": "/v2",
							},
						},
						Tags: tags,
					},
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.Plugin{
					ID:           new(entityID),
					Name:         "request-transformer",
					InstanceName: new("plugin-" + marker),
					Enabled:      new(true),
					Protocols:    []sdkkonnectcomp.Protocols{sdkkonnectcomp.ProtocolsHTTP, sdkkonnectcomp.ProtocolsHTTPS},
					Service:      &sdkkonnectcomp.PluginService{ID: new(parentID)},
					Ordering: &sdkkonnectcomp.Ordering{
						After: &sdkkonnectcomp.After{Access: []string{"rate-limiting"}},
					},
					Config: map[string]any{
						"replace": map[string]any{
							"uri": "/v2",
						},
						"add": map[string]any{
							"headers": []string{"x-z:1", "x-a:2"},
						},
					},
					Tags:      tags,
					CreatedAt: new(int64(1700000000)),
					UpdatedAt: new(int64(1700000100)),
				}
			},
			expectedPayloadHash: "sha256:7881e452ff791184ba9c45b17c075b5b11a8e7b0df11198407e1412bbdc41eae",
			expectedReprHash:    "sha256:2b58ec5838deb93d880672c6a0da25bd45f5623bf867eaa4874b678f67b49520",
		},
		{
			name: "consumer",
			payload: func(marker string) any {
				return newConsumerSyncPayload(
					sdkkonnectops.UpsertConsumerRequest{
						ControlPlaneID: controlPlaneID,
						ConsumerID:     entityID,
						Consumer: sdkkonnectcomp.Consumer{
							Username: new("consumer-" + marker),
							CustomID: new("custom-" + marker),
							Tags:     tags,
						},
					},
					[]string{caCertID2, caCertID1},
					[]invalidConsumerGroupRef{
						{Name: "cg-b", Reason: "NotFound"},
						{Name: "cg-a", Reason: "NotCreatedInKonnect"},
					},
				)
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.Consumer{
					ID:        new(entityID),
					Username:  new("consumer-" + marker),
					CustomID:  new("custom-" + marker),
					Tags:      tags,
					CreatedAt: new(int64(1700000000)),
					UpdatedAt: new(int64(1700000100)),
				}
			},
			expectedPayloadHash: "sha256:ad7e1c7a9475c51c4794825b10b67edf3a8be0ab4f8c23878c662cec8c2f6057",
			expectedReprHash:    "sha256:a4e941692127003cda2be6429749b1f136f40022798ad9611a3700e49cb6cb30",
		},
		{
			name: "consumer-group",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertConsumerGroupRequest{
					ControlPlaneID:  controlPlaneID,
					ConsumerGroupID: entityID,
					ConsumerGroup: sdkkonnectcomp.ConsumerGroup{
						Name: "group-" + marker,
						Tags: tags,
					},
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.ConsumerGroup{
					ID:        new(entityID),
					Name:      "group-" + marker,
					Tags:      tags,
					CreatedAt: new(int64(1700000000)),
					UpdatedAt: new(int64(1700000100)),
				}
			},
			expectedPayloadHash: "sha256:aecfd0467379471ec538bab769f2ad6709253e207ad75b9c48f5b509c8c3a07e",
			expectedReprHash:    "sha256:25e851758a0717b362f93261a97073ebf5636f374b7d972959166aadf1ebe4d7",
		},
		{
			name: "upstream",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertUpstreamRequest{
					ControlPlaneID: controlPlaneID,
					UpstreamID:     entityID,
					Upstream: sdkkonnectcomp.Upstream{
						Name:              "upstream-" + marker + ".example.com",
						Algorithm:         sdkkonnectcomp.UpstreamAlgorithmRoundRobin.ToPointer(),
						Slots:             new(int64(10000)),
						HashOn:            sdkkonnectcomp.HashOnHeader.ToPointer(),
						HashOnHeader:      new("X-Key"),
						HashFallback:      sdkkonnectcomp.HashFallbackNone.ToPointer(),
						ClientCertificate: &sdkkonnectcomp.UpstreamClientCertificate{ID: new(parentID)},
						Healthchecks: &sdkkonnectcomp.Healthchecks{
							Threshold: new(float64(50)),
							Active: &sdkkonnectcomp.Active{
								HTTPPath: new("/healthz"),
								Headers:  map[string][]string{"X-Probe": {"1"}, "Host": {"health.internal"}},
								Healthy: &sdkkonnectcomp.Healthy{
									HTTPStatuses: []int64{200, 302},
									Interval:     new(float64(5)),
									Successes:    new(int64(2)),
								},
							},
						},
						Tags: tags,
					},
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.Upstream{
					ID:                new(entityID),
					Name:              "upstream-" + marker + ".example.com",
					Algorithm:         sdkkonnectcomp.UpstreamAlgorithmRoundRobin.ToPointer(),
					Slots:             new(int64(10000)),
					HashOn:            sdkkonnectcomp.HashOnHeader.ToPointer(),
					HashOnHeader:      new("X-Key"),
					HashFallback:      sdkkonnectcomp.HashFallbackNone.ToPointer(),
					ClientCertificate: &sdkkonnectcomp.UpstreamClientCertificate{ID: new(parentID)},
					Healthchecks: &sdkkonnectcomp.Healthchecks{
						Threshold: new(float64(50)),
						Active: &sdkkonnectcomp.Active{
							HTTPPath: new("/healthz"),
							Headers:  map[string][]string{"Host": {"health.internal"}, "X-Probe": {"1"}},
							Healthy: &sdkkonnectcomp.Healthy{
								HTTPStatuses: []int64{200, 302},
								Interval:     new(float64(5)),
								Successes:    new(int64(2)),
							},
						},
					},
					Tags:      tags,
					CreatedAt: new(int64(1700000000)),
					UpdatedAt: new(int64(1700000100)),
				}
			},
			expectedPayloadHash: "sha256:8a1f730e10f72a01e2c5ea7f33c4a9e7ef864432eecefe5685d056edc984b56b",
			expectedReprHash:    "sha256:efb5d5d36bfb74980a6f50c51da475428bf97bdb04dcb8217387f477399b3fbb",
		},
		{
			name: "target",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertTargetWithUpstreamRequest{
					ControlPlaneID:      controlPlaneID,
					UpstreamIDForTarget: parentID,
					TargetID:            entityID,
					TargetWithoutParents: sdkkonnectcomp.TargetWithoutParents{
						Target: new("10.0.0.1:" + marker),
						Weight: new(int64(100)),
						Tags:   tags,
					},
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.Target{
					ID:        new(entityID),
					Target:    new("10.0.0.1:" + marker),
					Weight:    new(int64(100)),
					Upstream:  &sdkkonnectcomp.TargetUpstream{ID: new(parentID)},
					Tags:      tags,
					CreatedAt: new(float64(1700000000.5)),
					UpdatedAt: new(float64(1700000100.25)),
				}
			},
			expectedPayloadHash: "sha256:e006d6f5c8b16f9dfd27b58974ae03833b4c97d0b8f290dcd20951712ea64f11",
			expectedReprHash:    "sha256:7f14624f880414001f10dd7816d37ef4db75ab4a667e0cf7e07b9ed786263395",
		},
		{
			name: "ca-certificate",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertCaCertificateRequest{
					ControlPlaneID:  controlPlaneID,
					CACertificateID: entityID,
					CACertificate: sdkkonnectcomp.CACertificate{
						Cert: "-----BEGIN CERTIFICATE-----\n" + marker + "\n-----END CERTIFICATE-----\n",
						Tags: tags,
					},
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.CACertificate{
					ID:         new(entityID),
					Cert:       "-----BEGIN CERTIFICATE-----\n" + marker + "\n-----END CERTIFICATE-----\n",
					CertDigest: new("deadbeef"),
					Tags:       tags,
					CreatedAt:  new(int64(1700000000)),
					UpdatedAt:  new(int64(1700000100)),
				}
			},
			expectedPayloadHash: "sha256:2c49c464a91e38fcffc535455223a7f1b229b9b0fce54b71f577888e613ed90d",
			expectedReprHash:    "sha256:854a6cd39a94b0189957e481936a407275b699d725cb0a558fe335cf4434ba74",
		},
		{
			name: "certificate",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertCertificateRequest{
					ControlPlaneID: controlPlaneID,
					CertificateID:  entityID,
					CertificateRequest: sdkkonnectcomp.CreateCertificateRequestCertificateWithCertAndKey(
						sdkkonnectcomp.CertificateWithCertAndKey{
							Cert:    "cert-" + marker,
							Key:     "key-" + marker,
							CertAlt: new("cert-alt-" + marker),
							KeyAlt:  new("key-alt-" + marker),
							Tags:    tags,
						},
					),
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.Certificate{
					ID:        new(entityID),
					Cert:      "cert-" + marker,
					Key:       "key-" + marker,
					CertAlt:   new("cert-alt-" + marker),
					KeyAlt:    new("key-alt-" + marker),
					Tags:      tags,
					CreatedAt: new(int64(1700000000)),
					UpdatedAt: new(int64(1700000100)),
				}
			},
			expectedPayloadHash: "sha256:82d282173c7c8d3fabb286004f2c5ab1416591628225c906086123dae7f59f4d",
			expectedReprHash:    "sha256:cff9f81e3cbc3819b5db064d62cd5637889d91d26472e86be13c9c63d80aca74",
		},
		{
			name: "sni",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertSniWithCertificateRequest{
					ControlPlaneID: controlPlaneID,
					CertificateID:  parentID,
					SNIID:          entityID,
					SNIWithoutParents: sdkkonnectcomp.SNIWithoutParents{
						Name: marker + ".example.com",
						Tags: tags,
					},
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.Sni{
					ID:          new(entityID),
					Name:        marker + ".example.com",
					Certificate: sdkkonnectcomp.SNICertificate{ID: new(parentID)},
					Tags:        tags,
					CreatedAt:   new(int64(1700000000)),
					UpdatedAt:   new(int64(1700000100)),
				}
			},
			expectedPayloadHash: "sha256:6b54f7ad9fd0e0b7f8c5e52a107e05fa08800b68e9f32d056b578294aab9f8b2",
			expectedReprHash:    "sha256:097a3665bb77a4f254150f4353142ce7e8ada23e30e4a1b6a78d7335ba27f8c7",
		},
		{
			name: "key",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertKeyRequest{
					ControlPlaneID: controlPlaneID,
					KeyID:          entityID,
					Key: sdkkonnectcomp.Key{
						Kid:  "kid-" + marker,
						Name: new("key-" + marker),
						Set:  &sdkkonnectcomp.Set{ID: new(parentID)},
						Pem: &sdkkonnectcomp.Pem{
							PublicKey:  new("pub-" + marker),
							PrivateKey: new("priv-" + marker),
						},
						Tags: tags,
					},
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.Key{
					ID:   new(entityID),
					Kid:  "kid-" + marker,
					Name: new("key-" + marker),
					Set:  &sdkkonnectcomp.Set{ID: new(parentID)},
					Pem: &sdkkonnectcomp.Pem{
						PublicKey:  new("pub-" + marker),
						PrivateKey: new("priv-" + marker),
					},
					Tags:      tags,
					CreatedAt: new(int64(1700000000)),
					UpdatedAt: new(int64(1700000100)),
				}
			},
			expectedPayloadHash: "sha256:41f5bca7d1ffde84bea4972104f6390427febfa3fb71754b88f236e5d7400db3",
			expectedReprHash:    "sha256:96f30f27e17a1394e1c01155cb4371c66b6d6b389b2a601c3397f411ed50caad",
		},
		{
			name: "key-set",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertKeySetRequest{
					ControlPlaneID: controlPlaneID,
					KeySetID:       entityID,
					KeySet: &sdkkonnectcomp.KeySet{
						Name: new("keyset-" + marker),
						Tags: tags,
					},
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.KeySet{
					ID:        new(entityID),
					Name:      new("keyset-" + marker),
					Tags:      tags,
					CreatedAt: new(int64(1700000000)),
					UpdatedAt: new(int64(1700000100)),
				}
			},
			expectedPayloadHash: "sha256:e6a60a5e1c344b730c9141aa5db511d9a13820bfc7134fb8772beb560867ece7",
			expectedReprHash:    "sha256:da0ba049702d58e6dbfb5526aaba501e9d6907011f2fb978d2f5e08638988ed4",
		},
		{
			name: "basic-auth-credential",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertBasicAuthWithConsumerRequest{
					ControlPlaneID:              controlPlaneID,
					ConsumerIDForNestedEntities: parentID,
					BasicAuthID:                 entityID,
					BasicAuthWithoutParents: sdkkonnectcomp.BasicAuthWithoutParents{
						Username: "user-" + marker,
						Password: "password-" + marker,
						Tags:     tags,
					},
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.BasicAuth{
					ID:        new(entityID),
					Consumer:  &sdkkonnectcomp.BasicAuthConsumer{ID: new(parentID)},
					Username:  "user-" + marker,
					Password:  "hashed-password-" + marker,
					Tags:      tags,
					CreatedAt: new(int64(1700000000)),
				}
			},
			expectedPayloadHash: "sha256:247372dd8644cc0f22fc79737a7fa7d858b4340494ffa64154716cd14890012a",
			expectedReprHash:    "sha256:ab835848828027559846a1085c7614589b00361942d08fa60bd41d1b85ca2eb9",
		},
		{
			name: "api-key-credential",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertKeyAuthWithConsumerRequest{
					ControlPlaneID:              controlPlaneID,
					ConsumerIDForNestedEntities: parentID,
					KeyAuthID:                   entityID,
					KeyAuthWithoutParents: &sdkkonnectcomp.KeyAuthWithoutParents{
						Key:  new("apikey-" + marker),
						Tags: tags,
					},
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.KeyAuth{
					ID:        new(entityID),
					Consumer:  &sdkkonnectcomp.KeyAuthConsumer{ID: new(parentID)},
					Key:       new("apikey-" + marker),
					Tags:      tags,
					CreatedAt: new(int64(1700000000)),
				}
			},
			expectedPayloadHash: "sha256:1be9a3bdad0fb7bd3a2a01d75eea1a74808b156e70bd14adaa48069ec36660e2",
			expectedReprHash:    "sha256:4b6852e030c3f4d65babdda3237312ff018cbce0a8580b2194e50e7d3e18700b",
		},
		{
			name: "acl-credential",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertACLWithConsumerRequest{
					ControlPlaneID:              controlPlaneID,
					ConsumerIDForNestedEntities: parentID,
					ACLID:                       entityID,
					ACLWithoutParents: sdkkonnectcomp.ACLWithoutParents{
						Group: "acl-" + marker,
						Tags:  tags,
					},
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.ACL{
					ID:        new(entityID),
					Consumer:  &sdkkonnectcomp.ACLConsumer{ID: new(parentID)},
					Group:     "acl-" + marker,
					Tags:      tags,
					CreatedAt: new(int64(1700000000)),
				}
			},
			expectedPayloadHash: "sha256:55cf472a1c1ebf271d23540326395ce136e45daba75d273900f84b9a5d23472f",
			expectedReprHash:    "sha256:51b7b8beb786ed5fff7f33c9162bf1e40952e6af45f48c1b7302ef46ec57a80c",
		},
		{
			name: "jwt-credential",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertJwtWithConsumerRequest{
					ControlPlaneID:              controlPlaneID,
					ConsumerIDForNestedEntities: parentID,
					JWTID:                       entityID,
					JWTWithoutParents: &sdkkonnectcomp.JWTWithoutParents{
						Algorithm: sdkkonnectcomp.JWTWithoutParentsAlgorithmHs256.ToPointer(),
						Key:       new("jwt-key-" + marker),
						Secret:    new("jwt-secret-" + marker),
						Tags:      tags,
					},
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.Jwt{
					ID:        new(entityID),
					Consumer:  &sdkkonnectcomp.JWTConsumer{ID: new(parentID)},
					Algorithm: sdkkonnectcomp.JWTAlgorithmHs256.ToPointer(),
					Key:       new("jwt-key-" + marker),
					Secret:    new("jwt-secret-" + marker),
					Tags:      tags,
					CreatedAt: new(int64(1700000000)),
				}
			},
			expectedPayloadHash: "sha256:8daad733d73fbfe75757802fa0aef6c11a0a030f14765026763c8ef0b5429782",
			expectedReprHash:    "sha256:2d51dad8ae1e75443b33f0be71042ede911528b876e3fc896a584bf47fb6a721",
		},
		{
			name: "hmac-credential",
			payload: func(marker string) any {
				return sdkkonnectops.UpsertHmacAuthWithConsumerRequest{
					ControlPlaneID:              controlPlaneID,
					ConsumerIDForNestedEntities: parentID,
					HMACAuthID:                  entityID,
					HMACAuthWithoutParents: sdkkonnectcomp.HMACAuthWithoutParents{
						Username: "hmac-user-" + marker,
						Secret:   new("hmac-secret-" + marker),
						Tags:     tags,
					},
				}
			},
			repr: func(marker string) any {
				return &sdkkonnectcomp.HMACAuth{
					ID:        new(entityID),
					Consumer:  &sdkkonnectcomp.HMACAuthConsumer{ID: new(parentID)},
					Username:  "hmac-user-" + marker,
					Secret:    new("hmac-secret-" + marker),
					Tags:      tags,
					CreatedAt: new(int64(1700000000)),
				}
			},
			expectedPayloadHash: "sha256:68ea910b8e9d18abd3012f0fe1bfb9f0a8f47459c0c64d4e69ad320ec829609c",
			expectedReprHash:    "sha256:5684fd91850f0531a2575c430b508d66e342c91b246bfe13ebec8bfeedc57ed6",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			payloadHashA, err := entityPayloadHash(controlPlaneID, entityID, tc.payload("a"))
			require.NoError(t, err)
			payloadHashA2, err := entityPayloadHash(controlPlaneID, entityID, tc.payload("a"))
			require.NoError(t, err)
			payloadHashB, err := entityPayloadHash(controlPlaneID, entityID, tc.payload("b"))
			require.NoError(t, err)

			assert.Equal(t, payloadHashA, payloadHashA2, "payload hash must be deterministic")
			assert.NotEqual(t, payloadHashA, payloadHashB, "payload change must change the hash")
			assert.Equal(
				t,
				tc.expectedPayloadHash,
				payloadHashA.String(),
				"golden payload hash changed for %s; see docs/internal/konnect-sync-hashing.md before updating or bumping SyncHashVersion",
				tc.name,
			)

			reprHashA, err := representationHash(tc.repr("a"))
			require.NoError(t, err)
			reprHashA2, err := representationHash(tc.repr("a"))
			require.NoError(t, err)
			reprHashB, err := representationHash(tc.repr("b"))
			require.NoError(t, err)

			assert.Equal(t, reprHashA, reprHashA2, "representation hash must be deterministic")
			assert.NotEqual(t, reprHashA, reprHashB, "representation change must change the hash")
			assert.Equal(
				t,
				tc.expectedReprHash,
				reprHashA.String(),
				"golden representation hash changed for %s; see docs/internal/konnect-sync-hashing.md before updating or bumping SyncHashVersion",
				tc.name,
			)
		})
	}
}

func TestCanonicalPayloadNormalization(t *testing.T) {
	const (
		cpID = "cp-1"
		id   = "ent-1"
	)

	t.Run("struct field declaration order is ignored", func(t *testing.T) {
		type structAB struct {
			A string `json:"a"`
			B int64  `json:"b"`
		}
		type structBA struct {
			B int64  `json:"b"`
			A string `json:"a"`
		}
		ha, err := entityPayloadHash(cpID, id, structAB{A: "hello", B: 42})
		require.NoError(t, err)
		hb, err := entityPayloadHash(cpID, id, structBA{B: 42, A: "hello"})
		require.NoError(t, err)
		assert.Equal(t, ha, hb)
	})

	t.Run("nested map keys inside plugin config are sorted recursively", func(t *testing.T) {
		// The nested values are structs with opposite field declaration
		// order: encoding/json serializes structs in field order, so the
		// hashes only match if canonicalization decodes the payload into
		// maps and re-sorts every level. Plain nested maps would make the
		// assertion pass even without canonicalization, because
		// encoding/json already sorts map keys.
		type nestedAB struct {
			A int `json:"a"`
			B int `json:"b"`
		}
		type nestedBA struct {
			B int `json:"b"`
			A int `json:"a"`
		}
		p1 := sdkkonnectops.UpsertPluginRequest{
			ControlPlaneID: cpID,
			PluginID:       id,
			Plugin: sdkkonnectcomp.Plugin{
				Name: "request-transformer",
				Config: map[string]any{
					"z": nestedAB{A: 1, B: 2},
					"a": "first",
				},
			},
		}
		p2 := sdkkonnectops.UpsertPluginRequest{
			ControlPlaneID: cpID,
			PluginID:       id,
			Plugin: sdkkonnectcomp.Plugin{
				Name: "request-transformer",
				Config: map[string]any{
					"a": "first",
					"z": nestedBA{B: 2, A: 1},
				},
			},
		}
		h1, err := entityPayloadHash(cpID, id, p1)
		require.NoError(t, err)
		h2, err := entityPayloadHash(cpID, id, p2)
		require.NoError(t, err)
		assert.Equal(t, h1, h2)
	})

	t.Run("unordered string sets are normalized for tags, protocols, ca_certificates, and consumer groups", func(t *testing.T) {
		svc1 := sdkkonnectops.UpsertServiceRequest{
			ControlPlaneID: cpID,
			ServiceID:      id,
			ServiceRequest: sdkkonnectcomp.CreateServiceRequestServiceWithHost(
				sdkkonnectcomp.ServiceWithHost{
					Host:           "example.com",
					CaCertificates: []string{"ca-2", "ca-1"},
					Tags:           []string{"z", "a"},
				},
			),
		}
		svc2 := sdkkonnectops.UpsertServiceRequest{
			ControlPlaneID: cpID,
			ServiceID:      id,
			ServiceRequest: sdkkonnectcomp.CreateServiceRequestServiceWithHost(
				sdkkonnectcomp.ServiceWithHost{
					Host:           "example.com",
					CaCertificates: []string{"ca-1", "ca-2"},
					Tags:           []string{"a", "z"},
				},
			),
		}
		hs1, err := entityPayloadHash(cpID, id, svc1)
		require.NoError(t, err)
		hs2, err := entityPayloadHash(cpID, id, svc2)
		require.NoError(t, err)
		assert.Equal(t, hs1, hs2)

		r1 := sdkkonnectops.UpsertRouteRequest{
			ControlPlaneID: cpID,
			RouteID:        id,
			Route: sdkkonnectcomp.CreateRouteRouteJSON(sdkkonnectcomp.RouteJSON{
				Protocols: []sdkkonnectcomp.RouteJSONProtocols{sdkkonnectcomp.RouteJSONProtocolsHTTPS, sdkkonnectcomp.RouteJSONProtocolsHTTP},
			}),
		}
		r2 := sdkkonnectops.UpsertRouteRequest{
			ControlPlaneID: cpID,
			RouteID:        id,
			Route: sdkkonnectcomp.CreateRouteRouteJSON(sdkkonnectcomp.RouteJSON{
				Protocols: []sdkkonnectcomp.RouteJSONProtocols{sdkkonnectcomp.RouteJSONProtocolsHTTP, sdkkonnectcomp.RouteJSONProtocolsHTTPS},
			}),
		}
		hr1, err := entityPayloadHash(cpID, id, r1)
		require.NoError(t, err)
		hr2, err := entityPayloadHash(cpID, id, r2)
		require.NoError(t, err)
		assert.Equal(t, hr1, hr2)

		c1 := newConsumerSyncPayload(
			sdkkonnectops.UpsertConsumerRequest{ControlPlaneID: cpID, ConsumerID: id},
			[]string{"cg-b", "cg-a"},
			[]invalidConsumerGroupRef{{Name: "bad-b", Reason: "NotFound"}, {Name: "bad-a", Reason: "NotFound"}},
		)
		c2 := newConsumerSyncPayload(
			sdkkonnectops.UpsertConsumerRequest{ControlPlaneID: cpID, ConsumerID: id},
			[]string{"cg-a", "cg-b"},
			[]invalidConsumerGroupRef{{Name: "bad-a", Reason: "NotFound"}, {Name: "bad-b", Reason: "NotFound"}},
		)
		hc1, err := entityPayloadHash(cpID, id, c1)
		require.NoError(t, err)
		hc2, err := entityPayloadHash(cpID, id, c2)
		require.NoError(t, err)
		assert.Equal(t, hc1, hc2)

		// nil vs empty consumer group slices must hash identically.
		cNil := newConsumerSyncPayload(sdkkonnectops.UpsertConsumerRequest{ControlPlaneID: cpID, ConsumerID: id}, nil, nil)
		cEmpty := newConsumerSyncPayload(sdkkonnectops.UpsertConsumerRequest{ControlPlaneID: cpID, ConsumerID: id}, []string{}, []invalidConsumerGroupRef{})
		hNil, err := entityPayloadHash(cpID, id, cNil)
		require.NoError(t, err)
		hEmpty, err := entityPayloadHash(cpID, id, cEmpty)
		require.NoError(t, err)
		assert.Equal(t, hNil, hEmpty)
	})

	t.Run("order-sensitive arrays preserve order", func(t *testing.T) {
		r1 := sdkkonnectops.UpsertRouteRequest{
			ControlPlaneID: cpID,
			RouteID:        id,
			Route:          sdkkonnectcomp.CreateRouteRouteJSON(sdkkonnectcomp.RouteJSON{Paths: []string{"/a", "/b"}}),
		}
		r2 := sdkkonnectops.UpsertRouteRequest{
			ControlPlaneID: cpID,
			RouteID:        id,
			Route:          sdkkonnectcomp.CreateRouteRouteJSON(sdkkonnectcomp.RouteJSON{Paths: []string{"/b", "/a"}}),
		}
		h1, err := entityPayloadHash(cpID, id, r1)
		require.NoError(t, err)
		h2, err := entityPayloadHash(cpID, id, r2)
		require.NoError(t, err)
		assert.NotEqual(t, h1, h2, "route paths order must remain significant")

		p1 := sdkkonnectops.UpsertPluginRequest{
			ControlPlaneID: cpID,
			PluginID:       id,
			Plugin: sdkkonnectcomp.Plugin{
				Name:   "request-transformer",
				Config: map[string]any{"tags": []string{"z", "a"}, "protocols": []string{"https", "http"}},
			},
		}
		p2 := sdkkonnectops.UpsertPluginRequest{
			ControlPlaneID: cpID,
			PluginID:       id,
			Plugin: sdkkonnectcomp.Plugin{
				Name:   "request-transformer",
				Config: map[string]any{"tags": []string{"a", "z"}, "protocols": []string{"http", "https"}},
			},
		}
		hp1, err := entityPayloadHash(cpID, id, p1)
		require.NoError(t, err)
		hp2, err := entityPayloadHash(cpID, id, p2)
		require.NoError(t, err)
		assert.NotEqual(t, hp1, hp2, "arrays inside opaque plugin config must not be reordered")
	})

	t.Run("large int64 values preserve exact precision", func(t *testing.T) {
		// Both values exceed float64's 2^53 exact integer boundary and would
		// collapse if decoded as float64 instead of json.Number.
		h1, err := entityPayloadHash(cpID, id, map[string]int64{"n": 9007199254740993})
		require.NoError(t, err)
		h2, err := entityPayloadHash(cpID, id, map[string]int64{"n": 9007199254740992})
		require.NoError(t, err)
		assert.NotEqual(t, h1, h2)
	})
}

func TestCanonicalRepresentationNormalization(t *testing.T) {
	t.Run("reordered protocols and tags between PUT and GET match", func(t *testing.T) {
		putRoute := &sdkkonnectcomp.Route{
			Type: sdkkonnectcomp.RouteUnionTypeRouteJSON,
			RouteJSON: &sdkkonnectcomp.RouteJSON{
				ID:        new("r-1"),
				Protocols: []sdkkonnectcomp.RouteJSONProtocols{sdkkonnectcomp.RouteJSONProtocolsHTTPS, sdkkonnectcomp.RouteJSONProtocolsHTTP},
				Tags:      []string{"b", "a"},
				CreatedAt: new(int64(100)),
				UpdatedAt: new(int64(100)),
			},
		}
		getRoute := &sdkkonnectcomp.Route{
			Type: sdkkonnectcomp.RouteUnionTypeRouteJSON,
			RouteJSON: &sdkkonnectcomp.RouteJSON{
				ID:        new("r-1"),
				Protocols: []sdkkonnectcomp.RouteJSONProtocols{sdkkonnectcomp.RouteJSONProtocolsHTTP, sdkkonnectcomp.RouteJSONProtocolsHTTPS},
				Tags:      []string{"a", "b"},
				CreatedAt: new(int64(100)),
				UpdatedAt: new(int64(200)),
			},
		}
		hPut, err := representationHash(putRoute)
		require.NoError(t, err)
		hGet, err := representationHash(getRoute)
		require.NoError(t, err)
		assert.Equal(t, hPut, hGet)
	})

	t.Run("opaque plugin config fields named created_at or updated_at are not stripped", func(t *testing.T) {
		p1 := &sdkkonnectcomp.Plugin{
			ID:        new("p-1"),
			Name:      "custom",
			Config:    map[string]any{"created_at": "keep-a", "updated_at": 1},
			CreatedAt: new(int64(100)),
			UpdatedAt: new(int64(100)),
		}
		p2 := &sdkkonnectcomp.Plugin{
			ID:        new("p-1"),
			Name:      "custom",
			Config:    map[string]any{"created_at": "keep-b", "updated_at": 1},
			CreatedAt: new(int64(100)),
			UpdatedAt: new(int64(200)),
		}
		h1, err := representationHash(p1)
		require.NoError(t, err)
		h2, err := representationHash(p2)
		require.NoError(t, err)
		assert.NotEqual(t, h1, h2)
	})

	t.Run("external TTL added to KeyAuth in Konnect is detected as drift", func(t *testing.T) {
		withoutTTL := &sdkkonnectcomp.KeyAuth{
			ID:        new("ka-1"),
			Key:       new("secret"),
			CreatedAt: new(int64(100)),
		}
		withTTL := &sdkkonnectcomp.KeyAuth{
			ID:        new("ka-1"),
			Key:       new("secret"),
			TTL:       new(int64(3600)),
			CreatedAt: new(int64(100)),
		}
		h1, err := representationHash(withoutTTL)
		require.NoError(t, err)
		h2, err := representationHash(withTTL)
		require.NoError(t, err)
		assert.NotEqual(t, h1, h2)
	})
}
