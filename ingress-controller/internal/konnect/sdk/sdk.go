package sdk

import (
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/samber/lo"
)

// New returns a new SDK instance.
func New(token string, opts ...sdkkonnectgo.SDKOption) *sdkkonnectgo.SDK {
	security := sdkkonnectcomp.Security{}
	switch {
	case strings.HasPrefix(token, "kpat_"):
		security.PersonalAccessToken = lo.ToPtr(token)
	case strings.HasPrefix(token, "spat_"):
		security.SystemAccountAccessToken = lo.ToPtr(token)
	}
	sdkOpts := []sdkkonnectgo.SDKOption{
		sdkkonnectgo.WithSecurity(security),
	}
	sdkOpts = append(sdkOpts, opts...)

	return sdkkonnectgo.New(sdkOpts...)
}
