package install

import (
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kong/gateway-operator/apis/v1alpha1"
)

// Install is a callback from client-gen to add the scheme to the client
// and needs to be here because current client-gen versions (at the time
// of writing) required it as part of their templates.
func Install(scheme *runtime.Scheme) {
	v1alpha1.AddToScheme(scheme) //nolint:errcheck
}
