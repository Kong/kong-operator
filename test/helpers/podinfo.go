package helpers

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"

	"k8s.io/client-go/rest"
)

const (
	podLabelsDir  = "/etc/podinfo"
	podLabelsFile = "labels"
)

// SetupKubernetesServiceHost sets KUBERNETES_SERVICE_HOST environment variable
// from the cluster config if not already set. This enables NetworkPolicy creation
// in tests by making RunningOnKubernetes() return true.
// See: https://github.com/Kong/kong-operator/issues/2074
func SetupKubernetesServiceHost(restConfig *rest.Config) {
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return
	}

	if restConfig.Host == "" {
		return
	}

	apiURL, err := url.Parse(restConfig.Host)
	if err != nil || apiURL.Host == "" {
		return
	}

	fmt.Printf("INFO: setting KUBERNETES_SERVICE_HOST=%s for NetworkPolicy support\n", apiURL.Hostname())
	os.Setenv("KUBERNETES_SERVICE_HOST", apiURL.Hostname())
}

// SetupFakePodLabels creates a fake pod labels file for testing.
// The controller expects this file when RunningOnKubernetes() is true.
// This function should be called after SetupKubernetesServiceHost().
//
// Returns a cleanup function that should be called to remove the created file,
// and an error if the setup failed.
func SetupFakePodLabels() (cleanup func(), err error) {
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" {
		return func() {}, nil
	}

	labelsFilePath := path.Join(podLabelsDir, podLabelsFile)

	// Check if file already exists
	if _, err := os.Stat(labelsFilePath); err == nil {
		return func() {}, nil
	}

	fmt.Printf("INFO: creating pod labels file at %s\n", labelsFilePath)

	// Try to create the directory
	if err := os.MkdirAll(podLabelsDir, 0755); err != nil {
		// Permission denied, try with sudo
		fmt.Println("INFO: permission denied, retrying with sudo")
		cmd := exec.Command("sudo", "mkdir", "-p", podLabelsDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to create directory %s with sudo: %w, output: %s", podLabelsDir, err, string(output))
		}
	}

	// Write the file content to a temporary location first.
	// The controller reads these labels and uses them to build NetworkPolicy rules.
	// NOTE: We use "app.kubernetes.io/name" instead of "app" to match the telepresence
	// traffic-manager pod labels (we can't use "app" as it conflicts with telepresence's
	// deployment selector).
	content := []byte("app.kubernetes.io/name=\"kong-operator\"")
	tmpFile, err := os.CreateTemp("", "podinfo-labels-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to write to temporary file: %w", err)
	}
	tmpFile.Close()

	// Try to copy the file to the target location
	if err := os.Rename(tmpPath, labelsFilePath); err != nil {
		// Permission denied, use sudo to copy
		fmt.Printf("INFO: using sudo to copy file to %s\n", labelsFilePath)
		cmd := exec.Command("sudo", "cp", tmpPath, labelsFilePath)
		if output, err := cmd.CombinedOutput(); err != nil {
			os.Remove(tmpPath)
			return nil, fmt.Errorf("failed to copy file with sudo: %w, output: %s", err, string(output))
		}
		// Set proper permissions
		cmd = exec.Command("sudo", "chmod", "644", labelsFilePath)
		if output, err := cmd.CombinedOutput(); err != nil {
			os.Remove(tmpPath)
			return nil, fmt.Errorf("failed to set permissions with sudo: %w, output: %s", err, string(output))
		}
		// Clean up temp file
		os.Remove(tmpPath)
	}

	fmt.Println("INFO: successfully created pod labels file")

	// Verify the file was actually created and is readable
	if content, err := os.ReadFile(labelsFilePath); err != nil {
		return nil, fmt.Errorf("failed to verify pod labels file %s: %w", labelsFilePath, err)
	} else {
		fmt.Printf("INFO: verified pod labels file content: %s\n", string(content))
	}

	// Return cleanup function (though it may not work perfectly with os.Exit)
	return func() {
		// Note: This cleanup may not be called if os.Exit is used
		os.Remove(labelsFilePath)
	}, nil
}
