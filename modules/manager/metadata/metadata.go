// Package metadata includes metadata variables for logging and reporting.
package metadata

import (
	"fmt"
	"runtime"

	sdkkonnectmetadata "github.com/Kong/sdk-konnect-go/pkg/metadata"
)

func init() {
	// NOTE: We do it this way because speakeasy does not provide a way to set the
	// user-agent for the SDK instance.
	sdkkonnectmetadata.SetUserAgent(Metadata().UserAgent())
}

// -----------------------------------------------------------------------------
// Controller Manager - Versioning Information
// -----------------------------------------------------------------------------

// WARNING: moving any of these variables requires changes to both the Makefile
//          and the Dockerfile which modify them during the link step with -X

// Info is a struct type that holds the metadata for the controller manager.
type Info struct {
	// Release returns the release version, generally a semver like v1.0.0.
	Release string

	// Repo returns the git repository URL.
	Repo string

	// RepoURL returns the repository URL.
	RepoURL string

	// Commit returns the SHA from the current branch HEAD.
	Commit string

	// ProjectName is the name of the project.
	ProjectName string

	// Organization is the Kong organization
	Organization string
}

// UserAgent returns the User-Agent string to use in all HTTP requests made by KO.
func (inf Info) UserAgent() string {
	return fmt.Sprintf(
		"%s/%s (%s/%s)",
		inf.ProjectName, inf.Release, runtime.GOOS, runtime.GOARCH,
	)
}

var (
	// Release returns the release version, generally a semver like v1.0.0.
	release = "NOT_SET"

	// Repo returns the git repository URL.
	repo = "NOT_SET"

	// RepoURL returns the repository URL.
	repoURL = "NOT_SET"

	// Commit returns the SHA from the current branch HEAD.
	commit = "NOT_SET"

	// ProjectName is the name of the project.
	projectName = "NOT_SET"

	// Organization is the Kong organization.
	organization = "Kong"
)

// Metadata returns the metadata for the controller manager.
func Metadata() Info {
	return Info{
		Release:      release,
		Repo:         repo,
		RepoURL:      repoURL,
		Commit:       commit,
		ProjectName:  projectName,
		Organization: organization,
	}
}
