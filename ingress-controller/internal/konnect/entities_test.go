package konnect

import (
	"testing"

	"github.com/kong/go-database-reconciler/pkg/file"
	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/require"
)

func TestCountEntities(t *testing.T) {
	testCases := []struct {
		name     string
		content  *file.Content
		expected int
	}{
		{
			name:     "nil content",
			content:  nil,
			expected: 0,
		},
		{
			name:     "empty content",
			content:  &file.Content{},
			expected: 0,
		},
		{
			name: "content with nested entities",
			content: &file.Content{
				Services: []file.FService{
					{
						// 1 service + 1 service plugin + 2 routes + 1 route plugin.
						Plugins: []*file.FPlugin{{}},
						Routes: []*file.FRoute{
							{Plugins: []*file.FPlugin{{}}},
							{},
						},
					},
				},
				// 1 route + 1 route plugin.
				Routes: []file.FRoute{{Plugins: []*file.FPlugin{{}}}},
				// 1 global plugin.
				Plugins: []file.FPlugin{{}},
				Consumers: []file.FConsumer{
					{
						// 1 consumer + 1 plugin + 7 credentials. Group memberships are not counted.
						Plugins:     []*file.FPlugin{{}},
						KeyAuths:    []*kong.KeyAuth{{}},
						HMACAuths:   []*kong.HMACAuth{{}},
						JWTAuths:    []*kong.JWTAuth{{}},
						BasicAuths:  []*kong.BasicAuth{{}},
						Oauth2Creds: []*kong.Oauth2Credential{{}},
						ACLGroups:   []*kong.ACLGroup{{}},
						MTLSAuths:   []*kong.MTLSAuth{{}},
						Groups:      []*kong.ConsumerGroup{{}},
					},
				},
				// 1 consumer group + 1 plugin.
				ConsumerGroups: []file.FConsumerGroupObject{{Plugins: []*kong.ConsumerGroupPlugin{{}}}},
				// 1 upstream + 2 targets.
				Upstreams: []file.FUpstream{{Targets: []*file.FTarget{{}, {}}}},
				// 1 certificate + 2 SNIs.
				Certificates: []file.FCertificate{{SNIs: []kong.SNI{{}, {}}}},
				// 1 of each.
				CACertificates: []file.FCACertificate{{}},
				Vaults:         []file.FVault{{}},
				Licenses:       []file.FLicense{{}},
				Keys:           []file.FKey{{}},
				KeySets:        []file.FKeySet{{}},
				Partials:       []file.FPartial{{}},
			},
			expected: 5 + 2 + 1 + 9 + 2 + 3 + 3 + 6,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, countEntities(tc.content))
		})
	}
}
