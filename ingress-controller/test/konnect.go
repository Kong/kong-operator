package test

import "github.com/kong/kong-operator/v2/test"

// KonnectServerRegion returns the region of the Konnect server based on the server URL.
func KonnectServerRegion() string {
	serverURL := test.KonnectServerURL()
	switch serverURL {
	case "https://eu.api.konghq.tech", "https://eu.kic.api.konghq.tech":
		return "eu"
	case "https://ap.api.konghq.tech", "https://ap.kic.api.konghq.tech":
		return "ap"
	case "https://us.api.konghq.tech", "https://us.kic.api.konghq.tech":
		return "us"
	default:
		return "us"
	}
}
