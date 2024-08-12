package konnect

import (
	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectgocomp "github.com/Kong/sdk-konnect-go/models/components"
)

// SDKToken is a token used to authenticate with the Konnect SDK.
type SDKToken string

// SDKFactory is a factory for creating Konnect SDKs.
type SDKFactory interface {
	NewKonnectSDK(serverURL string, token SDKToken) *sdkkonnectgo.SDK
}

type sdkFactory struct{}

// NewSDKFactory creates a new SDKFactory.
func NewSDKFactory() SDKFactory {
	return sdkFactory{}
}

// NewKonnectSDK creates a new Konnect SDK.
func (f sdkFactory) NewKonnectSDK(serverURL string, token SDKToken) *sdkkonnectgo.SDK {
	return sdkkonnectgo.New(
		sdkkonnectgo.WithSecurity(
			sdkkonnectgocomp.Security{
				PersonalAccessToken: sdkkonnectgo.String(string(token)),
			},
		),
		sdkkonnectgo.WithServerURL(serverURL),
	)
}
