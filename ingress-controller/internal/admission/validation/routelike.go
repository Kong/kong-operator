package validation

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/annotations"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
)

func ValidateRouteSourceAnnotations(obj client.Object) error {
	protocols := annotations.ExtractProtocolNames(obj.GetAnnotations())
	for _, protocol := range protocols {
		if !util.ValidateProtocol(protocol) {
			return fmt.Errorf("invalid %s value: %s", annotations.AnnotationPrefix+annotations.ProtocolsKey, protocol)
		}
	}
	return nil
}
