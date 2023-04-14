//go:build e2e_tests
// +build e2e_tests

package e2e

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const gatewayOperatorImageKustomizationContents = `
images:
- name: ghcr.io/kong/gateway-operator
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

	imageName, imageTag, err := extractImageNameAndTag(image)
	if err != nil {
		fmt.Printf("ERROR: failed to parse custom image '%s', using default image\n", image)
		return nil //nolint:nilerr
	}

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
