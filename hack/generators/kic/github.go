package kic

import (
	"fmt"
	"io"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/kong/semver/v4"
)

// GetFileFromKICRepositoryForVersion fetches a file from the KIC repository for a given version.
func GetFileFromKICRepositoryForVersion(filePath string, version semver.Version) ([]byte, error) {
	const baseRepoURLTemplate = "https://raw.githubusercontent.com/Kong/kubernetes-ingress-controller/%s/%s"
	githubRevision := fmt.Sprintf("v%s", version.String())
	url := fmt.Sprintf(baseRepoURLTemplate, githubRevision, filePath)
	resp, err := retryablehttp.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s from KIC repository: %w", url, err)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
