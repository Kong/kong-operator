package e2e

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

//go:embed config/tests/kustomization.yaml
var testKustomizationFile []byte

// KustomizeDir represents a path to a temporary kustomize directory that has everything from
// config dir plus the tests/ dir with kustomization.yaml that has the image patch in place.
type KustomizeDir string

// Tests returns the path to the tests/ dir in the temporary kustomize directory.
func (kcp KustomizeDir) Tests() string {
	return filepath.Join(string(kcp), "tests")
}

// TestsKustomization returns the path to the tests/kustomization dir in the temporary kustomize directory.
func (kcp KustomizeDir) TestsKustomization() string {
	return filepath.Join(string(kcp), "tests/kustomization.yaml")
}

// CRD returns the path to the crd/ dir in the temporary kustomize directory.
func (kcp KustomizeDir) CRD() string {
	return filepath.Join(string(kcp), "crd")
}

// ManagerKustomizationYAML returns the path to the file manager/kustomization.yaml
func (kcp KustomizeDir) ManagerKustomizationYAML() string {
	return filepath.Join(string(kcp), "manager/kustomization.yaml")
}

// PrepareKustomizeDir prepares a temporary kustomize directory with operator
// image patch in place.
// It takes the provided image and uses it to append an "images:" section to
// resulting kustomization.yaml.
// It returns the KustomizeConfigPath that has methods to access particular paths.
func PrepareKustomizeDir(t *testing.T, image string) KustomizeDir {
	t.Helper()

	const (
		gatewayOperatorDefaultImage               = "docker.io/kong/kong-operator"
		gatewayOperatorDefaultTag                 = "main"
		gatewayOperatorImageKustomizationContents = "\n" +
			"images:\n" +
			"- name: docker.io/kong/kong-operator\n" +
			"  newName: %v\n" +
			"  newTag: '%v'\n"
	)

	var (
		imageName = gatewayOperatorDefaultImage
		imageTag  = gatewayOperatorDefaultTag
	)
	if image != "" {
		var err error
		imageName, imageTag, err = extractImageNameAndTag(image)
		if err != nil {
			t.Logf("failed to parse custom image '%s': %v, using default image: %s:%s",
				image, err, gatewayOperatorDefaultImage, gatewayOperatorDefaultTag)
			imageName, imageTag = gatewayOperatorDefaultImage, gatewayOperatorDefaultTag
		}
	}

	tmp := t.TempDir()

	// Create tests/ dir to contain the tests specific kustomization.
	testsDir := filepath.Join(tmp, "tests")
	require.NoError(t, os.Mkdir(testsDir, 0o700))
	t.Logf("using temporary directory for tests' kustomization.yaml: %s", testsDir)

	// Put tests config/tests/kustomization.yaml to tmp tests/ dir.
	kustomizationTestFilePath := filepath.Join(testsDir, "kustomization.yaml")
	require.NoError(t, os.WriteFile(kustomizationTestFilePath, testKustomizationFile, 0o600))

	// Write the image patch to tests/kustomization.yaml in temp dir.
	// NOTE: This could probably be done via parsed structs somehow instead of
	// appending the patch verbatim to the file.
	imagesPatch := fmt.Sprintf(gatewayOperatorImageKustomizationContents, imageName, imageTag)
	f, err := os.OpenFile(
		kustomizationTestFilePath,
		os.O_APPEND|os.O_WRONLY|os.O_CREATE,
		0o600,
	)
	require.NoError(t, err)
	_, err = f.WriteString(imagesPatch)
	require.NoError(t, err)

	return KustomizeDir(tmp)
}

// getOperatorImage gets the operator image to use in tests based on the image
// load and override environment variables.
func getOperatorImage(t *testing.T) string {
	t.Helper()

	if imageLoad != "" {
		t.Logf("using custom image via image load: %s", imageLoad)
		return imageLoad
	} else if imageOverride != "" {
		t.Logf("using custom image via image override: %s", imageOverride)
		return imageOverride
	}

	t.Log("using default image")
	return ""
}

func extractImageNameAndTag(fullname string) (name, tag string, err error) {
	var (
		lastColon  = strings.LastIndex(fullname, ":")
		parts      = strings.Split(fullname, ":")
		countParts = len(parts)
	)

	if countParts == 0 {
		return "", "", fmt.Errorf("could not parse image '%s'", fullname)
	}

	name = fullname[0:lastColon]
	tag = fullname[lastColon+1:]

	return name, tag, nil
}
