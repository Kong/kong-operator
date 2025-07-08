package kubernetes

import (
	"fmt"
	"os"
	"strings"
)

// This file includes utility functions for operating the metadata of
// the running KO instance itself.

const (
	podNamespaceEnvName         = "POD_NAMESPACE"
	podLabelsEnvName            = "POD_LABELS"
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
	labels := os.Getenv(podLabelsEnvName)
	if labels == "" {
		return nil, fmt.Errorf("Cannot find pod labels from env %s", podLabelsEnvName)
	}

	labelList := strings.Split(labels, "\n")
	ret := make(map[string]string, len(labelList))
	for _, label := range labelList {
		labelKV := strings.SplitN(label, "=", 2)
		if len(labelKV) != 2 {
			// TODO: return error here?
			continue
		}
		key, value := labelKV[0], labelKV[1]
		ret[key] = value
	}
	return ret, nil
}
