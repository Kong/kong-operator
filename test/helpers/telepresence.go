package helpers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/kong/kong-operator/test"
)

// SetupTelepresence installs the telepresence traffic manager in the cluster and connects to it.
// It returns a cleanup function that should be called when the test is done.
func SetupTelepresence(ctx context.Context) (func(), error) {
	if test.IsTelepresenceDisabled() {
		fmt.Println("INFO: telepresence is disabled, skipping setup")
		return func() {}, nil
	}

	fmt.Println("INFO: installing telepresence traffic manager in the cluster")
	const telepresenceBin = "TELEPRESENCE_BIN"
	telepresenceExecutable := os.Getenv(telepresenceBin)
	if telepresenceExecutable == "" {
		telepresenceExecutable = "telepresence"
		fmt.Printf("WARN: environment variable %s is not set, try to fallback to a system wide 'telepresnce'", telepresenceBin)
	} else {
		fmt.Printf("INFO: path to binary from %s environment variable is %s\n", telepresenceBin, telepresenceExecutable)
	}

	// Set pod labels on traffic-manager to match the labels expected by NetworkPolicy.
	// This allows traffic from the local test process (via telepresence) to be allowed
	// by the DataPlane's NetworkPolicy which restricts admin API access.
	// NOTE: We use "app.kubernetes.io/name" instead of "app" because "app" conflicts
	// with telepresence's deployment selector.
	// NOTE: We install traffic-manager in kong-system namespace to match the NetworkPolicy
	// rules which only allow traffic from kong-system namespace.
	// See: https://github.com/Kong/kong-operator/issues/2074
	commonHelmFlags := []string{
		"--manager-namespace", "kong-system",
		"--set", "podLabels.app\\.kubernetes\\.io/name=kong-operator",
	}
	helmInstallArgs := append([]string{"helm", "install"}, commonHelmFlags...)
	out, err := exec.CommandContext(ctx, telepresenceExecutable, helmInstallArgs...).CombinedOutput()
	if err != nil && bytes.Contains(out, []byte("use 'telepresence helm upgrade' instead to replace it")) {
		helmUpgradeArgs := append([]string{"helm", "upgrade"}, commonHelmFlags...)
		if out, err := exec.CommandContext(ctx, telepresenceExecutable, helmUpgradeArgs...).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to upgrade telepresence traffic manager: %w, %s", err, string(out))
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to install telepresence traffic manager: %w, %s", err, string(out))
	}

	fmt.Println("INFO: connecting to the cluster with telepresence")
	// NOTE: We need to specify --manager-namespace to connect to the traffic-manager
	// installed in kong-system namespace above.
	connectArgs := []string{"connect", "--manager-namespace", "kong-system"}
	out, err = exec.CommandContext(ctx, telepresenceExecutable, connectArgs...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the cluster with telepresence: %w, %s", err, string(out))
	}

	return func() {
		fmt.Println("INFO: quitting telepresence daemons")
		out, err := exec.CommandContext(ctx, telepresenceExecutable, "quit").CombinedOutput()
		if err != nil {
			fmt.Printf("ERROR: failed to quit telepresence daemons: %s\n", string(out))
		}
	}, nil
}
