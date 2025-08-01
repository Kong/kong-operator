package manager

import (
	"errors"
	"strings"
)

// NewWatchNamespaces parses the watchNamespaces string and returns a slice
// of trimmed namespace names.
func NewWatchNamespaces(watchNamespaces string) ([]string, error) {
	if watchNamespaces == "" {
		return nil, nil
	}

	// Split the namespaces by comma and trim whitespace
	namespaces := make([]string, 0)
	for ns := range strings.SplitSeq(watchNamespaces, ",") {
		ns = strings.TrimSpace(ns)
		if ns == "" {
			return nil, errors.New("watchNamespaces cannot contain empty namespace names")
		}
		namespaces = append(namespaces, ns)
	}
	return namespaces, nil
}
