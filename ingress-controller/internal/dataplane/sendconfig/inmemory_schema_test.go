package sendconfig_test

import (
	"encoding/json"
	"testing"

	"github.com/kong/go-database-reconciler/pkg/file"
	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/sendconfig"
)

func TestDBLessConfigMarshalToJSON(t *testing.T) {
	dblessConfig := sendconfig.DBLessConfig{
		Services: []file.FService{
			{
				Name: new("service-id"),
			},
		},
		ConsumerGroupConsumerRelationships: []sendconfig.ConsumerGroupConsumerRelationship{
			{
				ConsumerGroup: "cg1",
				Consumer:      "c1",
			},
		},
	}

	expected := `{
  "services": [
    {
      "name": "service-id"
    }
  ],
  "consumer_group_consumers": [
    {
      "consumer_group": "cg1",
      "consumer": "c1"
    }
  ]
}`
	b, err := json.Marshal(dblessConfig)
	require.NoError(t, err)
	require.JSONEq(t, expected, string(b))
}

func TestDefaultContentToDBLessConfigConverter(t *testing.T) {
	converter := sendconfig.DefaultContentToDBLessConfigConverter{}

	testCases := []struct {
		name                 string
		content              *file.Content
		expectedDBLessConfig sendconfig.DBLessConfig
	}{
		{
			name:    "empty content",
			content: &file.Content{},
			expectedDBLessConfig: sendconfig.DBLessConfig{
				Content: file.Content{},
			},
		},
		{
			name: "content with info",
			content: &file.Content{
				Info: &file.Info{
					SelectorTags: []string{"tag1", "tag2"},
				},
			},
			expectedDBLessConfig: sendconfig.DBLessConfig{
				Content: file.Content{},
			},
		},
		{
			name: "content with consumer group consumers and plugins",
			content: &file.Content{
				ConsumerGroups: []file.FConsumerGroupObject{
					{
						Name:      new("cg1"),
						Consumers: []*kong.Consumer{{Username: new("c1")}},
						Plugins:   []*kong.ConsumerGroupPlugin{{Name: new("p1")}},
					},
				},
				Consumers: []file.FConsumer{
					{
						Username: new("c1"),
						Groups:   []*kong.ConsumerGroup{{ID: new("cg1"), Name: new("cg1")}},
					},
				},
				Plugins: []file.FPlugin{
					{
						Name:          new("p1"),
						ConsumerGroup: &kong.ConsumerGroup{ID: new("cg1"), Name: new("cg1")},
					},
				},
			},
			expectedDBLessConfig: sendconfig.DBLessConfig{
				Content: file.Content{
					ConsumerGroups: []file.FConsumerGroupObject{
						{
							Name: new("cg1"),
						},
					},
					Consumers: []file.FConsumer{
						{
							Username: new("c1"),
						},
					},
					Plugins: []file.FPlugin{
						{
							Name: new("p1"),
							ConsumerGroup: &kong.ConsumerGroup{
								Name: new("cg1"),
								ID:   new("cg1"),
							},
						},
					},
				},
				ConsumerGroupConsumerRelationships: []sendconfig.ConsumerGroupConsumerRelationship{
					{
						ConsumerGroup: "cg1",
						Consumer:      "c1",
					},
				},
			},
		},
		{
			name: "content with consumer group consumers and plugins (only IDs filled)",
			content: &file.Content{
				ConsumerGroups: []file.FConsumerGroupObject{
					{
						Name:      new("cg1"),
						Consumers: []*kong.Consumer{{ID: new("c1")}},
						Plugins:   []*kong.ConsumerGroupPlugin{{ID: new("p1")}},
					},
				},
				Consumers: []file.FConsumer{
					{
						ID:     new("c1"),
						Groups: []*kong.ConsumerGroup{{ID: new("cg1")}},
					},
				},
				Plugins: []file.FPlugin{
					{
						Name:          new("p1"),
						ConsumerGroup: &kong.ConsumerGroup{ID: new("cg1")},
					},
				},
			},
			expectedDBLessConfig: sendconfig.DBLessConfig{
				Content: file.Content{
					ConsumerGroups: []file.FConsumerGroupObject{
						{
							Name: new("cg1"),
						},
					},
					Consumers: []file.FConsumer{
						{
							ID: new("c1"),
						},
					},
					Plugins: []file.FPlugin{
						{
							Name: new("p1"),
							ConsumerGroup: &kong.ConsumerGroup{
								ID: new("cg1"),
							},
						},
					},
				},
				ConsumerGroupConsumerRelationships: []sendconfig.ConsumerGroupConsumerRelationship{
					{
						ConsumerGroup: "cg1",
						Consumer:      "c1",
					},
				},
			},
		},
		{
			name: "content with plugin config nulls",
			content: &file.Content{
				Plugins: []file.FPlugin{
					{
						Name: new("p1"),
						Config: kong.Configuration{
							"config1": nil,
							"config2": "value2",
						},
					},
				},
				Consumers: []file.FConsumer{
					{
						Username: new("c1"),
						Plugins: []*file.FPlugin{
							{
								Name: new("p1"),
								Config: kong.Configuration{
									"config1": nil,
									"config2": "value2",
								},
							},
						},
					},
				},
				Routes: []file.FRoute{
					{
						Name: new("r1"),
						Plugins: []*file.FPlugin{
							{
								Name: new("p1"),
								Config: kong.Configuration{
									"config1": nil,
									"config2": "value2",
								},
							},
						},
					},
				},
				Services: []file.FService{
					{
						Name: new("s1"),
						Plugins: []*file.FPlugin{
							{
								Name: new("p1"),
								Config: kong.Configuration{
									"config1": nil,
									"config2": "value2",
								},
							},
						},
					},
				},
			},
			expectedDBLessConfig: sendconfig.DBLessConfig{
				Content: file.Content{
					Plugins: []file.FPlugin{
						{
							Name: new("p1"),
							Config: kong.Configuration{
								"config1": nil,
								"config2": "value2",
							},
						},
					},
					Consumers: []file.FConsumer{
						{
							Username: new("c1"),
							Plugins: []*file.FPlugin{
								{
									Name: new("p1"),
									Config: kong.Configuration{
										"config1": nil,
										"config2": "value2",
									},
								},
							},
						},
					},
					Routes: []file.FRoute{
						{
							Name: new("r1"),
							Plugins: []*file.FPlugin{
								{
									Name: new("p1"),
									Config: kong.Configuration{
										"config1": nil,
										"config2": "value2",
									},
								},
							},
						},
					},
					Services: []file.FService{
						{
							Name: new("s1"),
							Plugins: []*file.FPlugin{
								{
									Name: new("p1"),
									Config: kong.Configuration{
										"config1": nil,
										"config2": "value2",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dblessConfig := converter.Convert(tc.content)
			require.Equal(t, tc.expectedDBLessConfig, dblessConfig)
		})
	}
}

func BenchmarkDefaultContentToDBLessConfigConverter_Convert(b *testing.B) {
	content := &file.Content{
		Info: &file.Info{
			SelectorTags: []string{"tag1", "tag2"},
		},
		ConsumerGroups: []file.FConsumerGroupObject{
			{
				Name:      new("cg1"),
				Consumers: []*kong.Consumer{{Username: new("c1")}},
				Plugins:   []*kong.ConsumerGroupPlugin{{Name: new("p1")}},
			},
		},
		Consumers: []file.FConsumer{
			{
				Username: new("c1"),
				Groups:   []*kong.ConsumerGroup{{Name: new("cg1")}},
			},
		},
		Plugins: []file.FPlugin{
			{
				Name:          new("p1"),
				ConsumerGroup: &kong.ConsumerGroup{Name: new("cg1")},
				Config:        kong.Configuration{"config1": nil},
			},
			{
				Name:          new("p2"),
				ConsumerGroup: &kong.ConsumerGroup{Name: new("cg1")},
				Config:        kong.Configuration{"config1": nil},
			},
			{
				Name:          new("p3"),
				ConsumerGroup: &kong.ConsumerGroup{Name: new("cg1")},
				Config:        kong.Configuration{"config1": nil},
			},
		},
	}

	converter := sendconfig.DefaultContentToDBLessConfigConverter{}
	for b.Loop() {
		dblessConfig := converter.Convert(content)
		_ = dblessConfig
	}
}
