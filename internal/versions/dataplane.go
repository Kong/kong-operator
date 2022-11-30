package versions

import (
	"fmt"

	"github.com/kong/gateway-operator/internal/consts"
)

// supportedControlPlaneImages is the list of the supported DataPlane images
var supportedDataPlaneImages = map[string]struct{}{
	fmt.Sprintf("%s:3.0", consts.DefaultDataPlaneBaseImage):   {},
	fmt.Sprintf("%s:3.0.1", consts.DefaultDataPlaneBaseImage): {},
}

// IsDataPlaneSupported is a helper intended to validate the DataPlane
// image support.
// The image is expected to follow the format <container-image-name>:<tag>
func IsDataPlaneSupported(version string) bool {
	_, ok := supportedDataPlaneImages[version]
	return ok
}
