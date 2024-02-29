package kic

import (
	"fmt"
	"io"
	"log"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/kong/semver/v4"
)

// GetFileFromKICRepositoryForVersion fetches a file from the KIC repository for a given version.
func GetFileFromKICRepositoryForVersion(filePath string, version semver.Version) ([]byte, error) {
	// versionsManifestsOverrides maps KIC versions to commit hashes that we want to use.
	// It is used to override the default version string when fetching manifests from repository (i.e. when a given KIC
	// version hasn't been released yet).
	// TODO: Remove this once 3.1.1 is released: https://github.com/Kong/gateway-operator/issues/1509
	var versionsManifestsOverrides = map[string]string{

		"3.1.1": "4f8b13d069750014fc3c5b78e5b6d4cdb0f9bdb2",
	}

	githubRevision := fmt.Sprintf("v%s", version.String())
	// If the version is overridden, use the commit hash instead.
	if commitHash, ok := versionsManifestsOverrides[version.String()]; ok {
		log.Printf("Using commit hash %s for version %s", commitHash, version.String())
		githubRevision = commitHash
	}

	const baseRepoURLTemplate = "https://raw.githubusercontent.com/Kong/kubernetes-ingress-controller/%s/%s"
	url := fmt.Sprintf(baseRepoURLTemplate, githubRevision, filePath)
	resp, err := retryablehttp.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s from KIC repository: %w", url, err)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
