package test

import (
	"fmt"
	"os"
	"strings"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"

	"github.com/kong/kong-operator/v2/pkg/consts"
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

// ClusterIPFamily returns the IP family a newly created KIND test cluster
// should be configured with. It reads the KONG_TEST_CLUSTER_IP_FAMILY
// environment variable ("ipv4", "ipv6", or "dual"), defaulting to
// clusters.IPv4 when unset or unrecognized.
//
// This only takes effect when a new KIND cluster is created: KTF does not
// currently support configuring a non-default IP family on an existing
// cluster (see KONG_TEST_CLUSTER), so the setting is ignored in that case.
//
// clusters.Dual is not yet wired up to an actual dual-stack KIND cluster
// build (tracked in https://github.com/Kong/kong-operator/issues/5348) -
// callers must reject it explicitly rather than silently falling back to
// IPv4 or IPv6-only.
func ClusterIPFamily() clusters.IPFamily {
	raw := strings.ToLower(os.Getenv("KONG_TEST_CLUSTER_IP_FAMILY"))
	var family clusters.IPFamily
	switch raw {
	case "ipv6":
		family = clusters.IPv6
	case "dual":
		family = clusters.Dual
	case "", "ipv4":
		family = clusters.IPv4
	default:
		fmt.Printf("WARNING: unknown KONG_TEST_CLUSTER_IP_FAMILY %q, defaulting to %s\n", raw, clusters.IPv4)
		family = clusters.IPv4
	}
	fmt.Printf("INFO: test cluster IP family is %s\n", family)
	return family
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

// DataPlaneImage returns the data plane image to use in the test environment.
// It reads the KONG_TEST_DATA_PLANE_IMAGE environment variable, and defaults to consts.DefaultDataPlaneImage if not set.
func DataPlaneImage() string {
	image := os.Getenv("KONG_TEST_DATA_PLANE_IMAGE")
	if image == "" {
		image = consts.DefaultDataPlaneImage
	}
	return image
}

// KonnectAccessToken returns the Konnect access token for the test environment.
func KonnectAccessToken() string {
	return os.Getenv("KONG_TEST_KONNECT_ACCESS_TOKEN")
}

// KongLicenseData returns the raw Kong Enterprise license JSON for the test
// environment, as provided by the KONG_LICENSE_DATA environment variable
// (e.g. by the Kong/kong-license GitHub Action). Returns an empty string if
// no license is configured.
func KongLicenseData() string {
	return os.Getenv("KONG_LICENSE_DATA")
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
// to persist after the test for inspection. This has no effect when an existing cluster
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
	forceCleanup := strings.ToLower(os.Getenv("KONG_TEST_FORCE_CLEANUP"))
	if forceCleanup == "true" || forceCleanup == "1" {
		fmt.Println("INFO: test cleanup is forced by KONG_TEST_FORCE_CLEANUP")
		return false
	}
	return IsCI() || KeepTestCluster()
}

// ConformanceGatewayType returns the gateway type to run in conformance tests.
// It reads the KONG_TEST_CONFORMANCE_GATEWAY_TYPE environment variable.
// Valid values are "standard", "hybrid". If not set, it defaults to "standard".
func ConformanceGatewayType() string {
	gt := strings.ToLower(os.Getenv("KONG_TEST_CONFORMANCE_GATEWAY_TYPE"))
	if gt == "" {
		return "standard"
	}
	return gt
}

// ConformanceSkipTests returns an extra list of conformance test short names to
// skip, on top of the built-in skip lists. It reads the comma separated
// KONG_TEST_CONFORMANCE_SKIP_TESTS environment variable, e.g.
// "HTTPRouteReferenceGrant,HTTPRouteRedirectPort". This lets a local run drop
// the gotest -run filter entirely and instead exclude flaky or undesired tests.
// Whitespace around each name is trimmed and empty entries are ignored.
func ConformanceSkipTests() []string {
	raw := os.Getenv("KONG_TEST_CONFORMANCE_SKIP_TESTS")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var skip []string
	for name := range strings.SplitSeq(raw, ",") {
		if name = strings.TrimSpace(name); name != "" {
			skip = append(skip, name)
		}
	}
	return skip
}
