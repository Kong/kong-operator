package helpers

import (
	"context"
	"fmt"
	"os/exec"
)

// SetupTelepresence installs the telepresence traffic manager in the cluster and connects to it.
// It returns a cleanup function that should be called when the test is done.
func SetupTelepresence(ctx context.Context) (func(), error) {
	fmt.Println("INFO: installing telepresence traffic manager in the cluster")
	out, err := exec.CommandContext(ctx, "telepresence", "helm", "install").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to install telepresence traffic manager: %w, %s", err, string(out))
	}

	fmt.Println("INFO: connecting to the cluster with telepresence")
	out, err = exec.CommandContext(ctx, "telepresence", "connect").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the cluster with telepresence: %w, %s", err, string(out))
	}

	return func() {
		fmt.Println("INFO: quitting telepresence daemons")
		out, err := exec.CommandContext(ctx, "telepresence", "quit").CombinedOutput()
		if err != nil {
			fmt.Printf("ERROR: failed to quit telepresence daemons: %s\n", string(out))
		}
	}, nil
}
