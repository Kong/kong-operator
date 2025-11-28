package kubernetes

import (
	"fmt"
	"os"
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
	buf, err := os.ReadFile(podLabelsFile)
	if err != nil {
		return nil, fmt.Errorf("cannot find pod labels from file %s: %w", podLabelsFile, err)
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

// RunningOnKubernetes returns true if it is running in the kubernetes environment.
// If the env KUBERNETES_SERVICE_HOST is configured to access the kubernetes API server,
// it is considered to be running on k8s.
func RunningOnKubernetes() bool {
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

// RunningInPod returns true if the process is running inside an actual Kubernetes pod.
// This checks for the presence of the service account namespace file which is mounted
// in every pod by default.
// This is useful to distinguish between running in a real pod vs running locally
// with telepresence (where KUBERNETES_SERVICE_HOST might be set but we're not in a pod).
func RunningInPod() bool {
	_, err := os.Stat(serviceAccountNamespaceFile)
	return err == nil
}
