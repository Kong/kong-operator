//go:build e2e_tests

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kustomize/kyaml/copyutil"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

// prepareKustomizeDir prepares a temporary kustomize directory with operator
// image patch in place.
// It takes the provided image and uses it to append an "images:" section to
// resulting kustomization.yaml.
// It returns the path of said directory.
func prepareKustomizeDir(t *testing.T, image string) string {
	t.Helper()

	const (
		configDir                                 = "../../config"
		gatewayOperatorDefaultImage               = "ghcr.io/kong/gateway-operator"
		gatewayOperatorDefaultTag                 = "main"
		gatewayOperatorImageKustomizationContents = "\n" +
			"images:\n" +
			"- name: ghcr.io/kong/gateway-operator\n" +
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

	fs := filesys.MakeFsOnDisk()
	tmp, err := filesys.NewTmpConfirmedDir()
	require.NoError(t, err)

	// Copy the whole contents of config/ dir to temp dir for tests.
	require.NoError(t, copyutil.CopyDir(fs, configDir, tmp.String()))

	// Create tests/ dir to contain the tests specific kustomization.
	testsDir := filepath.Join(tmp.String(), "tests")
	require.NoError(t, fs.MkdirAll(testsDir))
	t.Logf("using temporary directory for tests' kustomization.yaml: %s", testsDir)

	// Copy tests dir (containing kustomization.yaml) to tmp tests/ dir.
	require.NoError(t, copyutil.CopyDir(fs, testsKustomizationPath, testsDir))

	// Write the image patch to tests/kustomization.yaml in temp dir.
	// NOTE: This could probably be done via parsed structs somehow instead of
	// appending the patch verbatim to the file.
	imagesPatch := fmt.Sprintf(gatewayOperatorImageKustomizationContents, imageName, imageTag)
	f, err := os.OpenFile(fmt.Sprintf("%s%c%s", testsDir, filepath.Separator, "kustomization.yaml"),
		os.O_APPEND|os.O_WRONLY|os.O_CREATE,
		0o600,
	)
	require.NoError(t, err)
	_, err = f.WriteString(imagesPatch)
	require.NoError(t, err)

	return testsDir
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

func Test_extractImageNameAndTag(t *testing.T) {
	tests := []struct {
		name     string
		fullName string
		wantName string
		wantTag  string
		wantErr  bool
	}{
		{
			name:     "gcr.io/kong/gateway-operator:v1.0",
			fullName: "gcr.io/kong/gateway-operator:v1.0",
			wantName: "gcr.io/kong/gateway-operator",
			wantTag:  "v1.0",
		},
		{
			name:     "localhost:5000/kong/gateway-operator:v1.0",
			fullName: "localhost:5000/kong/gateway-operator:v1.0",
			wantName: "localhost:5000/kong/gateway-operator",
			wantTag:  "v1.0",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotTag, err := extractImageNameAndTag(tt.fullName)
			if tt.wantErr {
				require.NoError(t, err)
				return
			}

			require.Equal(t, tt.wantName, gotName)
			require.Equal(t, tt.wantTag, gotTag)
		})
	}
}
