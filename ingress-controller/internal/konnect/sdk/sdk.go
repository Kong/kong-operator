package sdk

import (
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
)

// New returns a new SDK instance.
func New(token string, opts ...sdkkonnectgo.SDKOption) *sdkkonnectgo.SDK {
	security := sdkkonnectcomp.Security{}
	switch {
	case strings.HasPrefix(token, "kpat_"):
		security.PersonalAccessToken = new(token)
	case strings.HasPrefix(token, "spat_"):
		security.SystemAccountAccessToken = new(token)
	}
	sdkOpts := []sdkkonnectgo.SDKOption{
		sdkkonnectgo.WithSecurity(security),
	}
	sdkOpts = append(sdkOpts, opts...)

	return sdkkonnectgo.New(sdkOpts...)
}
