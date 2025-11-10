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

	if override := os.Getenv("KONG_OPERATOR_POD_LABELS_FILE"); override != "" {
		locations = append(locations, override)
	}

	locations = append(locations, podLabelsFile)
	if root := os.Getenv("TELEPRESENCE_ROOT"); root != "" {
		relPath := strings.TrimPrefix(podLabelsFile, "/")
		locations = append([]string{filepath.Join(root, relPath)}, locations...)
	}

	for _, path := range locations {
		buf, err := os.ReadFile(path)
		if err != nil {
			lastErr = err
			continue
		}

		labels := strings.SplitSeq(string(buf), "\n")
		ret := make(map[string]string)
		for label := range labels {
			labelKV := strings.SplitN(label, "=", 2)
			if len(labelKV) != 2 {
				return nil, fmt.Errorf("invalid label format, should be key=value")
			}
			key := labelKV[0]
			// The value in labels are escaped, e.g: "ko" => "\"ko\"". So we need to unquote it.
			value, err := strconv.Unquote(labelKV[1])
			if err != nil {
				continue
			}
			ret[key] = value
		}
		return ret, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("cannot find pod labels from file %s: %w", locations[len(locations)-1], lastErr)
	}
	return nil, fmt.Errorf("cannot determine pod labels")
}

// RunningOnKubernetes returns true if it is running in the kubernetes environment.
// If the env KUBERNETES_SERVICE_HOST is configured to access the kubernetes API server,
// it is considered to be running on k8s.
func RunningOnKubernetes() bool {
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}
