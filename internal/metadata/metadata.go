package metadata

import (
	"os"
)

// -----------------------------------------------------------------------------
// Versioning Information
// -----------------------------------------------------------------------------

var (
	// Organization is the kong organization
	Organization = "Kong"

	// ProjectURL id the address of the project website - git repository like github.com/kong/kubernetes-ingress-controller.
	ProjectURL = "https://github.com/Kong/gateway-operator"

	// ProjectName is the name of the project.
	ProjectName = "gateway-operator"

	// Release is the released version.
	Release = ""
)

func init() {
	if projectURL := os.Getenv("KGO_PROJECT_URL"); projectURL != "" {
		ProjectURL = projectURL
	}
	if projectName := os.Getenv("KGO_PROJECT_NAME"); projectName != "" {
		ProjectName = projectName
	}
	if release := os.Getenv("KGO_RELEASE"); release != "" {
		Release = release
	}
}
