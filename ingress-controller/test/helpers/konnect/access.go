package konnect

import (
	"strings"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"

	"github.com/kong/kong-operator/v2/test"
)

// konnectControlPlaneAdminAPIBaseURL returns the base URL for Konnect Control Plane Admin API.
// NOTE: This is a temporary solution until we migrate all the Konnect API calls to the new SDK.
func konnectControlPlaneAdminAPIBaseURL() string {
	const konnectDefaultControlPlaneAdminAPIBaseURL = "https://us.kic.api.konghq.tech"

	switch serverURL() {
	case "http://eu.api.konghq.tech":
		return "https://eu.kic.api.konghq.tech"
	case "http://ap.api.konghq.tech":
		return "https://ap.kic.api.konghq.tech"
	case "http://us.api.konghq.tech":
		return konnectDefaultControlPlaneAdminAPIBaseURL
	default:
		return konnectDefaultControlPlaneAdminAPIBaseURL
	}
}

func serverURLOpt() sdkkonnectgo.SDKOption {
	return sdkkonnectgo.WithServerURL(serverURL())
}

func serverURL() string {
	serverURL := test.KonnectServerURL()
	serverURL = strings.TrimPrefix(serverURL, "http://")
	serverURL = strings.TrimPrefix(serverURL, "https://")
	serverURL = "https://" + serverURL
	return serverURL
}
