// Package metadata includes metadata variables for logging and reporting.
package metadata

import (
	"fmt"
	"strings"
)

// -----------------------------------------------------------------------------
// Controller Manager - Versioning Information
// -----------------------------------------------------------------------------

// WARNING: moving any of these variables requires changes to both the Makefile
//          and the Dockerfile which modify them during the link step with -X

// BuildFlavor is the flavor of the build.
type BuildFlavor string

const (
	// OSSFlavor is the open-source flavor.
	OSSFlavor BuildFlavor = "oss"
	// EEFlavor is the enterprise flavor.
	EEFlavor BuildFlavor = "enterprise"
)

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

	// Flavor is the flavor of the build.
	Flavor BuildFlavor
}

// UserAgent returns the User-Agent string to use in all HTTP requests made by KGO.
func (inf Info) UserAgent() string {
	org := strings.ToLower(inf.Organization)
	if inf.Flavor == OSSFlavor {
		return fmt.Sprintf("%s-%s-%s/%s", org, inf.ProjectName, OSSFlavor, inf.Release)
	}
	return fmt.Sprintf("%s-%s/%s", org, inf.ProjectName, inf.Release)
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

	// Organization is the Kong organization
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
		Flavor:       OSSFlavor,
	}
}
