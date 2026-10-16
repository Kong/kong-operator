package isolated

import "fmt"

func examplesManifestPath(manifestName string) string {
	return fmt.Sprintf("../../../../ingress-controller/examples/%s", manifestName)
}
