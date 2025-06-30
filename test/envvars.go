package test

import (
	"fmt"
	"os"
	"strings"
)

// IsCalicoCNIDisabled returns true if the Calico CNI plugin is disabled in the test environment.
func IsCalicoCNIDisabled() bool {
	ret := strings.ToLower(os.Getenv("KONG_TEST_DISABLE_CALICO")) == "true"
	if ret {
		fmt.Println("INFO: CalicoCNI plugin is disabled")
	} else {
		fmt.Println("INFO: CalicoCNI plugin is enabled")
	}
	return ret
}

// IsCertManagerDisabled returns true if the Cert-Manager is disabled in the test environment.
func IsCertManagerDisabled() bool {
	ret := strings.ToLower(os.Getenv("KONG_TEST_DISABLE_CERTMANAGER")) == "true"
	if ret {
		fmt.Println("INFO: CertManager plugin is disabled")
	} else {
		fmt.Println("INFO: CertManager plugin is enabled")
	}
	return ret
}

// IsMetalLBDisabled returns true if the MetalLB is disabled in the test environment.
func IsMetalLBDisabled() bool {
	ret := strings.ToLower(os.Getenv("KONG_TEST_DISABLE_METALLB")) == "true"
	if ret {
		fmt.Println("INFO: MetalLB plugin is disabled")
	} else {
		fmt.Println("INFO: MetalLB plugin is enabled")
	}
	return ret
}

// IsInstallingCRDsDisabled returns true if installing CRDs is disabled in the test environment.
func IsInstallingCRDsDisabled() bool {
	ret := strings.ToLower(os.Getenv("KONG_TEST_DISABLE_CRD_INSTALL")) == "true"
	if ret {
		fmt.Println("INFO: Installing CRDs is disabled")
	} else {
		fmt.Println("INFO: Installing CRDs is enabled")
	}
	return ret
}

// KonnectAccessToken returns the Konnect access token for the test environment.
func KonnectAccessToken() string {
	return os.Getenv("KONG_TEST_KONNECT_ACCESS_TOKEN")
}

// KonnectServerURL returns the Konnect server URL for the test environment.
func KonnectServerURL() string {
	return os.Getenv("KONG_TEST_KONNECT_SERVER_URL")
}

// IsWebhookEnabled returns true if the webhook is enabled in the test environment.
func IsWebhookEnabled() bool {
	return strings.ToLower(os.Getenv("WEBHOOK_ENABLED")) == "true"
}

// IsTelepresenceDisabled returns true if the telepresence is disabled in the test environment.
func IsTelepresenceDisabled() bool {
	ret := strings.ToLower(os.Getenv("KONG_TEST_TELEPRESENCE_DISABLED")) == "true"
	if ret {
		fmt.Println("INFO: Telepresence is disabled")
	} else {
		fmt.Println("INFO: Telepresence is enabled")
	}
	return ret
}
