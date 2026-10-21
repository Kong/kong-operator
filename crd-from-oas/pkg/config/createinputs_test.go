package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestTrackCreateInputs(t *testing.T) {
	var tc TypeConfig
	require.NoError(t, yaml.Unmarshal([]byte(`
path: /v1/entities
dataSources:
  - path: spec.apiSpec.value
    type: Secret
ops:
  trackCreateInputs: true
  create:
    path: github.com/Kong/sdk-konnect-go/models/components.EventGatewayStaticKeyCreate
  delete: {}
`), &tc))
	require.NoError(t, tc.validate())
	assert.True(t, tc.OpsTrackCreateInputs)
	group := APIGroupVersionConfig{Types: []*TypeConfig{&tc}}
	ops := group.OpsConfig(map[string]string{tc.Path: "Entity"})
	require.Contains(t, ops, "Entity")
	assert.True(t, ops["Entity"].TrackCreateInputs)
	tc.Ops["update"] = &OpConfig{Path: "github.com/Kong/sdk-konnect-go/models/components.EventGatewayStaticKeyCreate"}
	require.ErrorContains(t, tc.validate(), "requires create without update")
	delete(tc.Ops, "update")
	tc.DataSources = nil
	require.ErrorContains(t, tc.validate(), "requires dataSources")
}
