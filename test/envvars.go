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

// KeepTestCluster indicates whether the caller wants the cluster created by the test suite
// to persist after the test for inspection. This has a nil effect when an existing cluster
// is provided, as cleanup is not performed for existing clusters.
func KeepTestCluster() bool {
	envVar := strings.ToLower(os.Getenv("KONG_TEST_CLUSTER_PERSIST"))
	keepTestCluster := envVar == "true" || envVar == "1"
	fmt.Printf("INFO: keeping test cluster after tests: %t\n", keepTestCluster)
	return keepTestCluster
}

// IsCI indicates whether or not the tests are running in a CI environment.
func IsCI() bool {
	// It's a common convention that e.g. GitHub, GitLab, and other CI providers
	// set the CI environment variable.
	envVar := strings.ToLower(os.Getenv("CI"))
	isCI := envVar == "true" || envVar == "1"
	fmt.Printf("INFO: running in CI: %t\n", isCI)
	return isCI
}

// SkipCleanup indicates whether or not the test environment should skip cleanup,
// either because it's running in a CI environment or because the user has
// explicitly requested to keep the test cluster.
func SkipCleanup() bool {
	return IsCI() || !KeepTestCluster()
}
