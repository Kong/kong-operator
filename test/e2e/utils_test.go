//go:build e2e_tests
// +build e2e_tests

package e2e

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

// createNamespaceForTest creates a namespace for a test case and cleans it up after the test finishes.
func createNamespaceForTest(t *testing.T) (*corev1.Namespace, func()) {
	namespaceForTestCase, err := clusters.GenerateNamespace(ctx, env.Cluster(), t.Name())
	exitOnErr(err)

	cleanup := func() {
		assert.NoError(t, clusters.CleanupGeneratedResources(ctx, env.Cluster(), t.Name()))
	}

	return namespaceForTestCase, cleanup
}

const gatewayOperatorImageKustomizationContents = `
images:
- name: ghcr.io/kong/gateway-operator:main
  newName: %v
  newTag: '%v'
`

// setOperatorImage appends content for replacing image to kustomization file
// and puts original content of kustomization file into a temporary file for backup.
func setOperatorImage() error {
	var image string
	if imageLoad != "" {
		image = imageLoad
	} else {
		image = imageOverride
	}

	if image == "" {
		fmt.Println("INFO: use default image")
		return nil
	}

	// TODO: deal with image names in format <host>:<port>/<repo>/<name>:[tag]
	// e.g localhost:32000/kong/gateway-operator:xxx
	parts := strings.Split(image, ":")
	if len(parts) != 2 {
		fmt.Printf("could not parse override image '%s', use default image\n", image)
		return nil
	}
	imageName := parts[0]
	imageTag := parts[1]

	fmt.Println("INFO: use custom image", image)

	buf, err := os.ReadFile(kustomizationFile)
	if err != nil {
		return err
	}

	// write current content of kustomization file to backup file.
	if backupKustomizationFile == "" {
		filename, err := createBackupKustomizationFile()
		if err != nil {
			return err
		}
		backupKustomizationFile = filename
		fmt.Printf("INFO: writing current content of kustomization file to %s for backup\n", filename)
		err = os.WriteFile(filename, buf, os.ModeAppend)
		if err != nil {
			return err
		}
	}

	// append image contents to replace image
	fmt.Println("INFO: replacing image in kustomization file")
	appendImageKustomizationContents := []byte(fmt.Sprintf(gatewayOperatorImageKustomizationContents, imageName, imageTag))
	newBuf := append(buf, appendImageKustomizationContents...)
	return os.WriteFile(kustomizationFile, newBuf, os.ModeAppend)
}

func createBackupKustomizationFile() (string, error) {
	file, err := os.CreateTemp("", "kustomization-yaml-backup")
	if err != nil {
		return "", err
	}

	defer file.Close()
	return file.Name(), nil
}

func restoreKustomizationFile() error {
	if backupKustomizationFile == "" {
		return nil
	}

	fmt.Printf("INFO: restore kustomization file from backup file %s\n", backupKustomizationFile)
	backUpBuf, err := os.ReadFile(backupKustomizationFile)
	if err != nil {
		return err
	}

	return os.WriteFile(kustomizationFile, backUpBuf, os.ModeAppend)
}
