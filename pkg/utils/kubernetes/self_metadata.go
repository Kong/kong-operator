package kubernetes

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// This file includes utility functions for operating the metadata of
// the running KO instance itself.

const (
	podNamespaceEnvName         = "POD_NAMESPACE"
	podLabelsFile               = "/etc/podinfo/labels"
	serviceAccountNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
)

// GetSelfNamespace gets the namespace in which KO runs.
func GetSelfNamespace() (string, error) {
	if ns := os.Getenv(podNamespaceEnvName); ns != "" {
		return ns, nil
	}
	// This actually gets the namespace of the service account to run the pod.
	buf, err := os.ReadFile(serviceAccountNamespaceFile)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

// GetSelfPodLabels gets all the labels of the KO pod.
func GetSelfPodLabels() (map[string]string, error) {
	var (
		lastErr   error
		locations []string
	)

	// Prefer explicit override first
	if override := os.Getenv("KONG_OPERATOR_POD_LABELS_FILE"); override != "" {
		locations = append(locations, override)
	}

	// Then try TELEPRESENCE_ROOT mounted path
	if root := os.Getenv("TELEPRESENCE_ROOT"); root != "" {
		relPath := strings.TrimPrefix(podLabelsFile, "/")
		locations = append(locations, filepath.Join(root, relPath))
	}

	// Finally fall back to standard pod labels file
	locations = append(locations, podLabelsFile)

	for _, path := range locations {
		buf, err := os.ReadFile(path)
		if err != nil {
			lastErr = err
			continue
		}

		ret := parsePodLabels(string(buf))
		if len(ret) > 0 {
			return ret, nil
		}
		lastErr = fmt.Errorf("no valid labels found in %s", path)
	}

	if lastErr != nil {
		return nil, fmt.Errorf("cannot find pod labels from %v: %w", locations, lastErr)
	}
	return nil, fmt.Errorf("cannot determine pod labels")
}

// parsePodLabels parses pod labels from DownwardAPI format.
// Supports both newline-separated and comma-separated formats.
func parsePodLabels(content string) map[string]string {
	ret := make(map[string]string)
	content = strings.TrimSpace(content)
	if content == "" {
		return ret
	}

	// Try newline-separated first
	lines := strings.Split(content, "\n")
	// If we only have one line and it contains commas, try comma-separated
	if len(lines) == 1 && strings.Contains(content, ",") {
		lines = strings.Split(content, ",")
	}

	for _, label := range lines {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}

		labelKV := strings.SplitN(label, "=", 2)
		if len(labelKV) != 2 {
			continue
		}

		key := strings.TrimSpace(labelKV[0])
		value := strings.TrimSpace(labelKV[1])
		if key == "" {
			continue
		}

		// Try to unquote the value (DownwardAPI escapes values)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}

		ret[key] = value
	}

	return ret
}

// RunningOnKubernetes returns true if it is running in the kubernetes environment.
// If the env KUBERNETES_SERVICE_HOST is configured to access the kubernetes API server,
// it is considered to be running on k8s.
func RunningOnKubernetes() bool {
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}
