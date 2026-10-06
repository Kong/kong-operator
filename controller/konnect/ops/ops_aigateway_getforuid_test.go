package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
)

// lookupEntry is a Konnect entity returned by a list call: its ID and the
// value of its k8s-uid label ("" for none).
type lookupEntry struct {
	id  string
	uid string
}

func (e lookupEntry) labels() map[string]string {
	if e.uid == "" {
		return nil
	}
	return map[string]string{KubernetesUIDLabelKey: e.uid}
}

// TestGetAIGatewayEntityForUID covers the getForUID lookups of AI Gateway
// entities that used to be matched by spec fields (name, or certificate and
// title): only the k8s-uid label identifies the entity, so an entity with the
// same name that another object (or no object) created is never matched.
func TestGetAIGatewayEntityForUID(t *testing.T) {
	const (
		gatewayID  = "gateway-1"
		consumerID = "consumer-1"
	)
	ref := func(id string) *aiconfigurationv1alpha1.KonnectEntityRef {
		return &aiconfigurationv1alpha1.KonnectEntityRef{ID: id}
	}

	// Each lookup returns the given entries from its list call. When uid is
	// empty, no list call is expected.
	lookups := []struct {
		name   string
		lookup func(t *testing.T, uid types.UID, entries []lookupEntry) (string, error)
	}{
		{
			name: "AIGatewayConsumer",
			lookup: func(t *testing.T, uid types.UID, entries []lookupEntry) (string, error) {
				sdk := sdkmocks.NewMockAIGatewayConsumersSDK(t)
				data := make([]sdkkonnectcomp.AIGatewayConsumer, 0, len(entries))
				for _, e := range entries {
					data = append(data, sdkkonnectcomp.AIGatewayConsumer{ID: e.id, Labels: e.labels()})
				}
				if uid != "" {
					sdk.EXPECT().
						ListAiGatewayConsumers(mock.Anything, sdkkonnectops.ListAiGatewayConsumersRequest{GatewayID: gatewayID, PageSize: new(listPageSize)}).
						Return(&sdkkonnectops.ListAiGatewayConsumersResponse{
							ListAIGatewayConsumersResponse: &sdkkonnectcomp.ListAIGatewayConsumersResponse{Data: data},
						}, nil).
						Once()
				}
				return getAIGatewayConsumerForUID(t.Context(), sdk, &aiconfigurationv1alpha1.AIGatewayConsumer{
					UID:    uid,
					Status: aiconfigurationv1alpha1.AIGatewayConsumerStatus{GatewayID: ref(gatewayID)},
				})
			},
		},
		{
			name: "AIGatewayConsumerGroup",
			lookup: func(t *testing.T, uid types.UID, entries []lookupEntry) (string, error) {
				sdk := sdkmocks.NewMockAIGatewayConsumerGroupsSDK(t)
				data := make([]sdkkonnectcomp.AIGatewayConsumerGroup, 0, len(entries))
				for _, e := range entries {
					data = append(data, sdkkonnectcomp.AIGatewayConsumerGroup{ID: e.id, Labels: e.labels()})
				}
				if uid != "" {
					sdk.EXPECT().
						ListAiGatewayConsumerGroups(mock.Anything, sdkkonnectops.ListAiGatewayConsumerGroupsRequest{GatewayID: gatewayID, PageSize: new(listPageSize)}).
						Return(&sdkkonnectops.ListAiGatewayConsumerGroupsResponse{
							ListAIGatewayConsumerGroupsResponse: &sdkkonnectcomp.ListAIGatewayConsumerGroupsResponse{Data: data},
						}, nil).
						Once()
				}
				return getAIGatewayConsumerGroupForUID(t.Context(), sdk, &aiconfigurationv1alpha1.AIGatewayConsumerGroup{
					UID:    uid,
					Status: aiconfigurationv1alpha1.AIGatewayConsumerGroupStatus{GatewayID: ref(gatewayID)},
				})
			},
		},
		{
			name: "AIGatewayConsumerCredential",
			lookup: func(t *testing.T, uid types.UID, entries []lookupEntry) (string, error) {
				sdk := sdkmocks.NewMockAIGatewayConsumersSDK(t)
				data := make([]sdkkonnectcomp.AIGatewayConsumerCredential, 0, len(entries))
				for _, e := range entries {
					data = append(data, sdkkonnectcomp.AIGatewayConsumerCredential{ID: e.id, Labels: e.labels()})
				}
				if uid != "" {
					sdk.EXPECT().
						ListAiGatewayConsumerCredentials(mock.Anything, sdkkonnectops.ListAiGatewayConsumerCredentialsRequest{
							PageSize:   new(listPageSize),
							GatewayID:  gatewayID,
							ConsumerID: consumerID,
						}).
						Return(&sdkkonnectops.ListAiGatewayConsumerCredentialsResponse{
							ListAIGatewayConsumerCredentialsResponse: &sdkkonnectcomp.ListAIGatewayConsumerCredentialsResponse{Data: data},
						}, nil).
						Once()
				}
				return getAIGatewayConsumerCredentialForUID(t.Context(), sdk, &aiconfigurationv1alpha1.AIGatewayConsumerCredential{
					UID: uid,
					Status: aiconfigurationv1alpha1.AIGatewayConsumerCredentialStatus{
						GatewayID:  ref(gatewayID),
						ConsumerID: ref(consumerID),
					},
				})
			},
		},
		{
			name: "AIGatewayDataPlaneCertificate",
			lookup: func(t *testing.T, uid types.UID, entries []lookupEntry) (string, error) {
				sdk := sdkmocks.NewMockAIGatewayDataPlaneCertificatesSDK(t)
				data := make([]sdkkonnectcomp.AIGatewayDataPlaneClientCertificate, 0, len(entries))
				for _, e := range entries {
					data = append(data, sdkkonnectcomp.AIGatewayDataPlaneClientCertificate{ID: e.id, Labels: e.labels()})
				}
				if uid != "" {
					sdk.EXPECT().
						ListAiGatewayDataPlaneCertificates(mock.Anything, sdkkonnectops.ListAiGatewayDataPlaneCertificatesRequest{GatewayID: gatewayID, PageSize: new(listPageSize)}).
						Return(&sdkkonnectops.ListAiGatewayDataPlaneCertificatesResponse{
							ListAIGatewayDataPlaneCertificatesResponse: &sdkkonnectcomp.ListAIGatewayDataPlaneCertificatesResponse{Data: data},
						}, nil).
						Once()
				}
				return getAIGatewayDataPlaneCertificateForUID(t.Context(), sdk, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
					UID:    uid,
					Status: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateStatus{GatewayID: ref(gatewayID)},
				})
			},
		},
	}

	const uid types.UID = "object-uid"
	testCases := []struct {
		name       string
		uid        types.UID
		entries    []lookupEntry
		expectedID string
	}{
		{
			name: "matches by k8s-uid label",
			uid:  uid,
			entries: []lookupEntry{
				{id: "no-label"},
				{id: "owned-by-other-object", uid: "other-uid"},
				{id: "matched", uid: string(uid)},
			},
			expectedID: "matched",
		},
		{
			name: "does not match entities without the label or owned by another object",
			uid:  uid,
			entries: []lookupEntry{
				{id: "no-label"},
				{id: "owned-by-other-object", uid: "other-uid"},
			},
		},
		{
			name: "returns not found without listing when the object has no UID",
		},
	}

	for _, l := range lookups {
		for _, tc := range testCases {
			t.Run(l.name+"/"+tc.name, func(t *testing.T) {
				id, err := l.lookup(t, tc.uid, tc.entries)
				if tc.expectedID != "" {
					require.NoError(t, err)
					assert.Equal(t, tc.expectedID, id)
					return
				}
				require.Empty(t, id)
				var notFoundErr EntityWithMatchingUIDNotFoundError
				require.ErrorAs(t, err, &notFoundErr)
			})
		}
	}
}
