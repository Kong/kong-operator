package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEntitySyncHashesPerType exercises the payload and representation hash
// input types used by every guarded Kong Gateway entity type. It catches
// serialization changes that would silently invalidate persisted status hashes
// or make an unchanged representation look different.
func TestEntitySyncHashesPerType(t *testing.T) {
	const controlPlaneID = "control-plane-id"

	testCases := []struct {
		name    string
		payload func(id string) any
		repr    func(id string) any
	}{
		{
			name: "service",
			payload: func(id string) any {
				return sdkkonnectops.UpsertServiceRequest{
					ControlPlaneID: controlPlaneID,
					ServiceID:      id,
					ServiceRequest: sdkkonnectcomp.CreateServiceRequestServiceWithHost(
						sdkkonnectcomp.ServiceWithHost{Host: "example.com"},
					),
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.Service{ID: new(id)}
			},
		},
		{
			name: "route",
			payload: func(id string) any {
				return sdkkonnectops.UpsertRouteRequest{
					ControlPlaneID: controlPlaneID,
					RouteID:        id,
					Route: sdkkonnectcomp.CreateRouteRouteJSON(
						sdkkonnectcomp.RouteJSON{ID: new(id)},
					),
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.Route{
					RouteJSON: &sdkkonnectcomp.RouteJSON{ID: new(id)},
				}
			},
		},
		{
			name: "plugin",
			payload: func(id string) any {
				return sdkkonnectops.UpsertPluginRequest{
					ControlPlaneID: controlPlaneID,
					PluginID:       id,
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.Plugin{ID: new(id)}
			},
		},
		{
			name: "consumer",
			payload: func(id string) any {
				return sdkkonnectops.UpsertConsumerRequest{
					ControlPlaneID: controlPlaneID,
					ConsumerID:     id,
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.Consumer{ID: new(id)}
			},
		},
		{
			name: "consumer-group",
			payload: func(id string) any {
				return sdkkonnectops.UpsertConsumerGroupRequest{
					ControlPlaneID:  controlPlaneID,
					ConsumerGroupID: id,
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.ConsumerGroup{ID: new(id)}
			},
		},
		{
			name: "upstream",
			payload: func(id string) any {
				return sdkkonnectops.UpsertUpstreamRequest{
					ControlPlaneID: controlPlaneID,
					UpstreamID:     id,
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.Upstream{ID: new(id)}
			},
		},
		{
			name: "target",
			payload: func(id string) any {
				return sdkkonnectops.UpsertTargetWithUpstreamRequest{
					ControlPlaneID: controlPlaneID,
					TargetID:       id,
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.Target{ID: new(id)}
			},
		},
		{
			name: "ca-certificate",
			payload: func(id string) any {
				return sdkkonnectops.UpsertCaCertificateRequest{
					ControlPlaneID:  controlPlaneID,
					CACertificateID: id,
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.CACertificate{ID: new(id)}
			},
		},
		{
			name: "certificate",
			payload: func(id string) any {
				return sdkkonnectops.UpsertCertificateRequest{
					ControlPlaneID: controlPlaneID,
					CertificateID:  id,
					CertificateRequest: sdkkonnectcomp.CreateCertificateRequestCertificateWithCertAndKey(
						sdkkonnectcomp.CertificateWithCertAndKey{Cert: "cert", Key: "key"},
					),
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.Certificate{ID: new(id)}
			},
		},
		{
			name: "sni",
			payload: func(id string) any {
				return sdkkonnectops.UpsertSniWithCertificateRequest{
					ControlPlaneID: controlPlaneID,
					SNIID:          id,
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.Sni{ID: new(id)}
			},
		},
		{
			name: "key",
			payload: func(id string) any {
				return sdkkonnectops.UpsertKeyRequest{
					ControlPlaneID: controlPlaneID,
					KeyID:          id,
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.Key{ID: new(id)}
			},
		},
		{
			name: "key-set",
			payload: func(id string) any {
				return sdkkonnectops.UpsertKeySetRequest{
					ControlPlaneID: controlPlaneID,
					KeySetID:       id,
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.KeySet{ID: new(id)}
			},
		},
		{
			name: "basic-auth-credential",
			payload: func(id string) any {
				return sdkkonnectops.UpsertBasicAuthWithConsumerRequest{
					ControlPlaneID: controlPlaneID,
					BasicAuthID:    id,
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.BasicAuth{ID: new(id)}
			},
		},
		{
			name: "api-key-credential",
			payload: func(id string) any {
				return sdkkonnectops.UpsertKeyAuthWithConsumerRequest{
					ControlPlaneID: controlPlaneID,
					KeyAuthID:      id,
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.KeyAuth{ID: new(id)}
			},
		},
		{
			name: "acl-credential",
			payload: func(id string) any {
				return sdkkonnectops.UpsertACLWithConsumerRequest{
					ControlPlaneID: controlPlaneID,
					ACLID:          id,
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.ACL{ID: new(id)}
			},
		},
		{
			name: "jwt-credential",
			payload: func(id string) any {
				return sdkkonnectops.UpsertJwtWithConsumerRequest{
					ControlPlaneID: controlPlaneID,
					JWTID:          id,
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.Jwt{ID: new(id)}
			},
		},
		{
			name: "hmac-credential",
			payload: func(id string) any {
				return sdkkonnectops.UpsertHmacAuthWithConsumerRequest{
					ControlPlaneID: controlPlaneID,
					HMACAuthID:     id,
				}
			},
			repr: func(id string) any {
				return &sdkkonnectcomp.HMACAuth{ID: new(id)}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			const entityID = "entity-id"

			payloadHashA, err := entityPayloadHash(controlPlaneID, entityID, tc.payload("payload-a"))
			require.NoError(t, err)
			payloadHashA2, err := entityPayloadHash(controlPlaneID, entityID, tc.payload("payload-a"))
			require.NoError(t, err)
			payloadHashB, err := entityPayloadHash(controlPlaneID, entityID, tc.payload("payload-b"))
			require.NoError(t, err)

			assert.Equal(t, payloadHashA, payloadHashA2, "payload hash must be deterministic")
			assert.NotEqual(t, payloadHashA, payloadHashB, "payload change must change the hash")
			assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, payloadHashA.String())

			reprHashA, err := representationHash(tc.repr("representation-a"))
			require.NoError(t, err)
			reprHashA2, err := representationHash(tc.repr("representation-a"))
			require.NoError(t, err)
			reprHashB, err := representationHash(tc.repr("representation-b"))
			require.NoError(t, err)

			assert.Equal(t, reprHashA, reprHashA2, "representation hash must be deterministic")
			assert.NotEqual(t, reprHashA, reprHashB, "representation change must change the hash")
			assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, reprHashA.String())
		})
	}
}

// entityPayloadHash mirrors the payload wrapper used by upsertUnlessInSync.
func entityPayloadHash(controlPlaneID, entityID string, payload any) (hash, error) {
	return hashJSON(struct {
		ControlPlaneID string `json:"control_plane_id"`
		ID             string `json:"id"`
		Payload        any    `json:"payload"`
	}{controlPlaneID, entityID, payload})
}
