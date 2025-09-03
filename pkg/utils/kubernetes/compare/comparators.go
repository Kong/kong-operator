package compare

import (
	"reflect"

	operatorv1beta1 "github.com/kong/kong-operator/apis/gateway-operator/v1beta1"
)

// NetworkOptionsDeepEqual checks if NetworkOptions are equal.
func NetworkOptionsDeepEqual(opts1, opts2 *operatorv1beta1.DataPlaneNetworkOptions) bool {
	return reflect.DeepEqual(opts1.Services, opts2.Services)
}

// DataPlaneResourceOptionsDeepEqual checks if DataPlane resource options are equal.
func DataPlaneResourceOptionsDeepEqual(opts1, opts2 *operatorv1beta1.DataPlaneResources) bool {
	return reflect.DeepEqual(opts1, opts2)
}
