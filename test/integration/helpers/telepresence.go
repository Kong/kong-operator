package helpers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/kong/kong-operator/test"
)

// SetupTelepresence installs the telepresence traffic manager in the cluster and connects to it.
// It returns a cleanup function that should be called when the test is done.
var (
	telepresencePathOnce sync.Once
	telepresencePath     string
	telepresencePathErr  error
)

// resolveTelepresenceExecutable returns the absolute path to the telepresence executable.
// It respects the TELEPRESENCE_BIN environment variable and falls back to the
// binary available in PATH.
func resolveTelepresenceExecutable() (string, error) {
	telepresencePathOnce.Do(func() {
		execPath := os.Getenv("TELEPRESENCE_BIN")
		if execPath == "" {
			execPath = "telepresence"
		}

		var err error
		telepresencePath, err = exec.LookPath(execPath)
		if err != nil {
			telepresencePathErr = fmt.Errorf("failed to locate telepresence binary %q: %w", execPath, err)
			return
		}

		if err := os.Setenv("TELEPRESENCE_BIN", telepresencePath); err != nil {
			telepresencePathErr = fmt.Errorf("failed setting TELEPRESENCE_BIN: %w", err)
			return
		}
	})

	return telepresencePath, telepresencePathErr
}

// SetupTelepresence installs the telepresence traffic manager in the cluster and connects to it.
// It returns a cleanup function that should be called when the test is done.
func SetupTelepresence(ctx context.Context) (func(), error) {
	if test.IsTelepresenceDisabled() {
		fmt.Println("INFO: telepresence is disabled, skipping setup")
		return func() {}, nil
	}

	fmt.Println("INFO: installing telepresence traffic manager in the cluster")
	telepresenceExec, err := resolveTelepresenceExecutable()
	if err != nil {
		return nil, err
	}
	fmt.Printf("INFO: using telepresence binary at %s\n", telepresenceExec)

	// Ensure any stale daemons are terminated so that binary and daemon versions match.
	if out, err := exec.CommandContext(ctx, telepresenceExec, "quit", "-s").CombinedOutput(); err != nil && len(out) > 0 {
		fmt.Printf("WARN: telepresence quit -s reported: %s\n", string(out))
	}

	out, err := exec.CommandContext(ctx, telepresenceExec, "helm", "install").CombinedOutput()
	if err != nil && bytes.Contains(out, []byte("use 'telepresence helm upgrade' instead to replace it")) {
		if out, err := exec.CommandContext(ctx, telepresenceExec, "helm", "upgrade").CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to upgrade telepresence traffic manager: %w, %s", err, string(out))
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to install telepresence traffic manager: %w, %s", err, string(out))
	}

	fmt.Println("INFO: connecting to the cluster with telepresence")
	connectArgs := []string{"connect"}
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		connectArgs = append(connectArgs, "--namespace", ns)
	}
	out, err = exec.CommandContext(ctx, telepresenceExec, connectArgs...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the cluster with telepresence: %w, %s", err, string(out))
	}

	return func() {
		fmt.Println("INFO: quitting telepresence daemons")
		out, err := exec.CommandContext(ctx, telepresenceExec, "quit").CombinedOutput()
		if err != nil {
			fmt.Printf("ERROR: failed to quit telepresence daemons: %s\n", string(out))
		}
	}, nil
}
