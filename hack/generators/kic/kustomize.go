package kic

import (
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
)

// BuildKustomizeForURLAndRef runs kustomize build for the provided URL and ref.
// It returns the output of the kustomize build command.
func BuildKustomizeForURLAndRef(ctx context.Context, url, ref string) ([]byte, error) {
	kustomizeResourceURL := fmt.Sprintf("%s?ref=%s", url, ref)

	log.Printf("Running 'kustomize build %s'\n", kustomizeResourceURL)
	cmd := exec.CommandContext(ctx, "kustomize", "build", kustomizeResourceURL)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start kustomize command %v: %w", cmd, err)
	}
	b, err := io.ReadAll(stdout)
	if err != nil {
		return nil, fmt.Errorf("failed to read kustomize stdout: %w", err)
	}
	berr, err := io.ReadAll(stderr)
	if err != nil {
		return nil, fmt.Errorf("failed to read kustomize stderr: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("failed to wait for kustomize to finish, output %s: %w", string(berr), err)
	}

	return b, nil
}
