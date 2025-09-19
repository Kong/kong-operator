package install

import (
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
)

// Install is a callback from client-gen to add the scheme to the client
// and needs to be here because current client-gen versions (at the time
// of writing) required it as part of their templates.
func Install(scheme *runtime.Scheme) {
	utilruntime.Must(operatorv1beta1.AddToScheme(scheme))
}
