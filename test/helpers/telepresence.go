package helpers

import (
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

	out, err := exec.CommandContext(ctx, telepresenceExecutable, "helm", "install").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to install telepresence traffic manager: %w, %s", err, string(out))
	}

	fmt.Println("INFO: connecting to the cluster with telepresence")
	out, err = exec.CommandContext(ctx, telepresenceExecutable, "connect").CombinedOutput()
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
