package webhook

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// NOTE: Copy-pasted from https://github.com/Kong/kubernetes-ingress-controller/blob/4f8b13d069750014fc3c5b78e5b6d4cdb0f9bdb2/internal/util/test/webhooks.go

// This hack is tracked in https://github.com/Kong/kubernetes-ingress-controller/issues/1613:
//
// The test process (`go test github.com/Kong/kubernetes-ingress-controller/test/integration/...`) serves the webhook
// endpoints to be consumed by the apiserver (so that the tests can apply a ValidatingWebhookConfiguration and test
// those validations).
//
// In order to make that possible, we needed to allow the apiserver (that gets spun up by the test harness) to access
// the system under test (which runs as a part of the `go test` process).
// Below, we're making an audacious assumption that the host running the `go test` process is either:
//
// - a direct Docker host on the default bridge, and that the apiserver is running within a context
// (such as KIND running on that same docker bridge), from which 172.17.0.1 routes to the host OR
// - a Colima host, and that the apiserver is running within a docker container hosted by Colima
// from which 192.168.5.2 routes to the host (https://github.com/abiosoft/colima/issues/220)
//
// This works if the test runs against a KIND cluster, and does not work against cloud providers (like GKE).

// getLocalOperatorListenHost returns the host IP address depends on environment where the test is running.
func getLocalOperatorListenHost() string {
	return localOperatorListenHost
}

var localOperatorListenHost = getHostIPbyType(getHostType())

type hostType string

const (
	hostTypeColima hostType = "colima"
	hostTypeLima   hostType = "lima"
	defaultDocker  hostType = "defaultDocker"
)

func getHostIPbyType(ht hostType) string {
	// Read more about those IPs in the docs of particular solution, e.g. for Lima:
	// https://github.com/lima-vm/socket_vmnet?tab=readme-ov-file#how-to-use-static-ip-addresses
	switch ht {
	case hostTypeColima:
		return "192.168.5.2"
	case hostTypeLima:
		return "192.168.105.1"
	case defaultDocker:
		return getDockerBridgeGateway()
	default:
		panic("unsupported host type")
	}
}

// getDockerBridgeGateway dynamically retrieves the Docker bridge network gateway IP.
// This is necessary because the default Docker bridge network may not always use 172.17.0.1
// (e.g., some systems use 172.18.0.1 or other addresses).
func getDockerBridgeGateway() string {
	cmd := exec.Command("docker", "network", "inspect", "bridge", "--format", "{{json .IPAM.Config}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("WARNING: failed to get Docker bridge gateway, falling back to 172.17.0.1: %v\n", err)
		return "172.17.0.1"
	}

	var configs []struct {
		Gateway string `json:"Gateway"`
	}
	if err := json.Unmarshal(out, &configs); err != nil {
		fmt.Printf("WARNING: failed to parse Docker bridge config, falling back to 172.17.0.1: %v\n", err)
		return "172.17.0.1"
	}

	if len(configs) > 0 && configs[0].Gateway != "" {
		return configs[0].Gateway
	}

	fmt.Println("WARNING: no gateway found in Docker bridge config, falling back to 172.17.0.1")
	return "172.17.0.1"
}

func getHostType() hostType {
	cmd := exec.Command("docker", "info", "--format", "{{.Name}}")
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		fmt.Printf("ERROR: failed to run %q command %s\n%s\n", cmd.String(), err, output)
		return defaultDocker
	}
	switch {
	case strings.Contains(output, "colima"):
		return hostTypeColima
	case strings.Contains(output, "lima"):
		return hostTypeLima
	default:
		return defaultDocker
	}
}
