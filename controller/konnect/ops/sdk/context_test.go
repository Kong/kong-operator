package sdk

import (
	"context"
	"testing"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testSDKWrapper struct {
	SDKWrapper
	serverURL string
	dpNodes   sdkkonnectgo.DPNodesSDK
}

func (w testSDKWrapper) GetServerURL() string {
	return w.serverURL
}

func (w testSDKWrapper) GetDPNodesSDK() sdkkonnectgo.DPNodesSDK {
	return w.dpNodes
}

func TestSDKContext(t *testing.T) {
	_, ok := SDKFromContext(context.Background())
	assert.False(t, ok)

	ctx := WithSDK(context.Background(), testSDKWrapper{serverURL: "https://example.com"})
	got, ok := SDKFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, "https://example.com", got.GetServerURL())

	assert.NotPanics(t, func() {
		WithSDK(context.Background(), nil)
	})
}
