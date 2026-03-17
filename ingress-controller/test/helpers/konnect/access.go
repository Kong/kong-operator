package konnect

import (
	sdkkonnectgo "github.com/Kong/sdk-konnect-go"

	"github.com/kong/kong-operator/v2/ingress-controller/test"
)

// konnectControlPlaneAdminAPIBaseURL returns the base URL for Konnect Control Plane Admin API.
// NOTE: This is a temporary solution until we migrate all the Konnect API calls to the new SDK.
func konnectControlPlaneAdminAPIBaseURL() string {
	const konnectDefaultControlPlaneAdminAPIBaseURL = "https://us.kic.api.konghq.tech"

	serverURL := test.KonnectServerURL()
	switch serverURL {
	case "https://eu.api.konghq.tech":
		return "https://eu.kic.api.konghq.tech"
	case "https://ap.api.konghq.tech":
		return "https://ap.kic.api.konghq.tech"
	case "https://us.api.konghq.tech":
		return konnectDefaultControlPlaneAdminAPIBaseURL
	default:
		return konnectDefaultControlPlaneAdminAPIBaseURL
	}
}

func serverURLOpt() sdkkonnectgo.SDKOption {
	return sdkkonnectgo.WithServerURL(test.KonnectServerURL())
}
